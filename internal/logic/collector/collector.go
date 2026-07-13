package collector

import (
	corelogic "admin/internal/logic"
	"net/http"
	"sync"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// Collector 概览默认参数限制后台聚合批次、缓存时间和展示数量。
const (
	defaultCollectorOverviewKafkaBatchSize   = 500             // 概览展示 Kafka producer 默认写入批次
	defaultCollectorOverviewFailureBatchSize = 500             // 概览展示失败账本重试默认批次大小
	defaultCollectorOverviewFailureInterval  = 1               // 概览展示失败账本重试默认轮询间隔秒数
	defaultCollectorOverviewRunningLease     = 600             // 概览展示失败重试默认运行租约秒数
	defaultCollectorOverviewMaxRetryTimes    = 8               // 概览展示失败事件默认最大重试次数
	defaultCollectorOverviewBizTypeTopLimit  = 5               // 概览展示失败 biz_type 排行数量上限
	collectorOverviewCacheTTL                = 5 * time.Second // Collector 概览短缓存有效期
)

// collectorOverviewBizTypeExpr 将空业务类型归入 unknown，避免概览误判无数据。
const collectorOverviewBizTypeExpr = "COALESCE(NULLIF(biz_type, ''), 'unknown')"

// collectorOverviewDoneAtFilter 兼容 finished_at 为空的重试成功记录。
const collectorOverviewDoneAtFilter = "(finished_at >= ? OR (finished_at IS NULL AND updated_at >= ?))"

// collectorOverviewCache 概览缓存
var collectorOverviewCache = struct {
	mu          sync.RWMutex                 // 保护概览缓存读写，避免并发请求重复打满 MySQL
	expiresAt   time.Time                    // 缓存过期时间
	generatedAt time.Time                    // 当前缓存对应的概览生成时间
	data        *types.CollectorOverviewResp // 最近一次成功聚合的概览快照
}{}

// CollectorLogic 承载通用收集器运行观测和失败账本管理能力。
type CollectorLogic struct {
	*corelogic.BaseLogic // 复用上下文、日志、数据库和审计能力
}

// NewCollectorLogic 创建通用收集器管理逻辑对象。
func NewCollectorLogic(r *http.Request, svcCtx *svc.ServiceContext) *CollectorLogic {
	return &CollectorLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// Overview 返回 Collector 运行态指标、失败账本积压和近期处理表现。
func (l *CollectorLogic) Overview() *types.BizResult {
	db := l.Svc.ReadDB(svc.DatabaseMain)
	resp, err := l.cachedOverview(db)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.Overview").ToBizResult()
	}
	resp.RuntimeMetrics = l.collectorRuntimeMetrics()
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(resp)
}

// cachedOverview 使用短缓存复用概览结果，避免多人巡检时连续重复打 MySQL 聚合。
func (l *CollectorLogic) cachedOverview(db *gorm.DB) (*types.CollectorOverviewResp, error) {
	if cached := loadCollectorOverviewCache(); cached != nil {
		return cached, nil
	}
	collectorOverviewCache.mu.Lock()
	defer collectorOverviewCache.mu.Unlock()
	if collectorOverviewCache.data != nil && time.Now().Before(collectorOverviewCache.expiresAt) {
		return cloneCollectorOverviewResp(collectorOverviewCache.data, collectorOverviewCache.generatedAt, true), nil
	}
	resp, err := l.buildOverview(db)
	if err != nil {
		return nil, errors.Tag(err)
	}
	generatedAt := time.Now()
	resp.OverviewGeneratedAt = generatedAt.Format(time.DateTime)
	resp.OverviewCacheTTLSeconds = int(collectorOverviewCacheTTL / time.Second)
	resp.OverviewCacheAgeSeconds = 0
	resp.OverviewCacheHit = false
	collectorOverviewCache.data = resp
	collectorOverviewCache.generatedAt = generatedAt
	collectorOverviewCache.expiresAt = generatedAt.Add(collectorOverviewCacheTTL)
	return cloneCollectorOverviewResp(resp, generatedAt, false), nil
}

// ListFailures 查询 Collector 失败事件列表。
func (l *CollectorLogic) ListFailures(req *types.ListCollectorFailuresReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}

	db := l.Svc.ReadDB(svc.DatabaseMain)

	query := db.Model(&model.CollectorFailedEvent{})
	if req.BizType != "" {
		query = query.Where("biz_type = ?", req.BizType)
	}
	if req.State != nil {
		query = query.Where("state = ?", *req.State)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.ListFailures Count").ToBizResult()
	}

	offset := (req.Page - 1) * req.PageSize
	rows := make([]model.CollectorFailedEvent, 0, req.PageSize)
	if err := query.Order("id DESC").Offset(offset).Limit(req.PageSize).Find(&rows).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.ListFailures Find").ToBizResult()
	}

	list := make([]types.CollectorFailureResp, 0, len(rows))
	for i := range rows {
		list = append(list, *toCollectorFailureResp(&rows[i]))
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(types.ListResp[types.CollectorFailureResp]{
			List:  list,
			Total: total,
		})
}

// buildOverview 聚合 Collector 失败账本当前积压、配置与近窗口重试表现。
func (l *CollectorLogic) buildOverview(db *gorm.DB) (*types.CollectorOverviewResp, error) {
	now := time.Now()
	cfg := l.Svc.CurrentConfig().Collector
	resp := &types.CollectorOverviewResp{
		KafkaBatchSize:            resolveCollectorKafkaBatchSize(cfg.Kafka),
		FailureRunnerBatchSize:    resolveCollectorFailureBatchSize(cfg.FailureRetry),
		FailureRunnerIntervalSecs: resolveCollectorFailureInterval(cfg.FailureRetry),
		RunningLeaseSeconds:       resolveCollectorRunningLease(cfg.FailureRetry),
		MaxRetryTimes:             resolveCollectorMaxRetry(cfg.FailureRetry),
		Recent1m:                  types.CollectorWindowStatResp{WindowMinutes: 1},
		Recent5m:                  types.CollectorWindowStatResp{WindowMinutes: 5},
		Recent15m:                 types.CollectorWindowStatResp{WindowMinutes: 15},
		BizTypeTop15m:             make([]types.CollectorBizTypeStatResp, 0),
	}

	stateCounts := make([]struct {
		State int   `gorm:"column:state"` // 失败事件状态
		Total int64 `gorm:"column:total"` // 状态对应事件数
	}, 0)
	if err := freshDB(db).Model(&model.CollectorFailedEvent{}).
		Select("state, COUNT(*) AS total").
		Group("state").
		Scan(&stateCounts).Error; err != nil {
		return nil, errors.Tag(err)
	}
	for _, item := range stateCounts {
		switch item.State {
		case int(model.CollectorFailedEventStatePending):
			resp.PendingCount = item.Total
		case int(model.CollectorFailedEventStateRunning):
			resp.RunningCount = item.Total
		case int(model.CollectorFailedEventStateDone):
			resp.DoneCount = item.Total
		case int(model.CollectorFailedEventStateRetry):
			resp.RetryCount = item.Total
		case int(model.CollectorFailedEventStateDead):
			resp.DeadCount = item.Total
		}
	}

	if err := freshDB(db).Model(&model.CollectorFailedEvent{}).
		Where("(state = ? OR state = ?) AND next_run_at <= ?", model.CollectorFailedEventStatePending, model.CollectorFailedEventStateRetry, now).
		Count(&resp.ReadyCount).Error; err != nil {
		return nil, errors.Tag(err)
	}

	if err := freshDB(db).Model(&model.CollectorFailedEvent{}).
		Where("state = ? AND lease_until IS NOT NULL AND lease_until <= ?", model.CollectorFailedEventStateRunning, now).
		Count(&resp.RunningTimeoutCount).Error; err != nil {
		return nil, errors.Tag(err)
	}

	oldestReady := struct {
		NextRunAt *time.Time `gorm:"column:next_run_at"` // 最早可执行时间
	}{}
	if err := freshDB(db).Model(&model.CollectorFailedEvent{}).
		Select("MIN(next_run_at) AS next_run_at").
		Where("(state = ? OR state = ?) AND next_run_at <= ?", model.CollectorFailedEventStatePending, model.CollectorFailedEventStateRetry, now).
		Scan(&oldestReady).Error; err != nil {
		return nil, errors.Tag(err)
	}
	if oldestReady.NextRunAt != nil {
		resp.OldestReadyAgeSeconds = max(0, int64(now.Sub(*oldestReady.NextRunAt).Seconds()))
	}

	for _, window := range []*types.CollectorWindowStatResp{&resp.Recent1m, &resp.Recent5m, &resp.Recent15m} {
		if err := l.fillOverviewWindow(db, now, window); err != nil {
			return nil, errors.Tag(err)
		}
	}
	if err := l.fillOverviewBizTypeTop(db, now, resp); err != nil {
		return nil, errors.Tag(err)
	}
	return resp, nil
}

// fillOverviewWindow 聚合指定窗口内的重试成功、失败和耗时信息。
func (l *CollectorLogic) fillOverviewWindow(db *gorm.DB, now time.Time, window *types.CollectorWindowStatResp) error {
	if window == nil || window.WindowMinutes <= 0 {
		return nil
	}
	since := now.Add(-time.Duration(window.WindowMinutes) * time.Minute)
	successRow := struct {
		SuccessCount int64   `gorm:"column:success_count"` // 成功任务数
		AvgCostMs    float64 `gorm:"column:avg_cost_ms"`   // 平均耗时毫秒
		MaxCostMs    float64 `gorm:"column:max_cost_ms"`   // 最大耗时毫秒
	}{}
	if err := buildCollectorWindowSuccessQuery(db, since).Scan(&successRow).Error; err != nil {
		return errors.Tag(err)
	}
	window.SuccessCount = successRow.SuccessCount
	window.AvgCostMs = successRow.AvgCostMs
	window.MaxCostMs = successRow.MaxCostMs

	if err := freshDB(db).Model(&model.CollectorFailedEvent{}).
		Where("(state = ? OR state = ?) AND updated_at >= ?", model.CollectorFailedEventStateRetry, model.CollectorFailedEventStateDead, since).
		Count(&window.FailCount).Error; err != nil {
		return errors.Tag(err)
	}
	return nil
}

// fillOverviewBizTypeTop 聚合最近 15 分钟失败热点业务，便于快速定位风险。
func (l *CollectorLogic) fillOverviewBizTypeTop(db *gorm.DB, now time.Time, resp *types.CollectorOverviewResp) error {
	if resp == nil {
		return nil
	}
	since := now.Add(-15 * time.Minute)
	rows := make([]types.CollectorBizTypeStatResp, 0, defaultCollectorOverviewBizTypeTopLimit)
	if err := buildCollectorBizTypeOverviewQuery(db, now, since).Scan(&rows).Error; err != nil {
		return errors.Tag(err)
	}
	resp.BizTypeTop15m = rows
	return nil
}

// freshDB 为每条概览聚合创建干净查询，避免 GORM Statement 条件串场。
func freshDB(db *gorm.DB) *gorm.DB {
	if db == nil {
		return db
	}
	session := &gorm.Session{NewDB: true}
	if db.Statement != nil && db.Statement.Context != nil {
		session.Context = db.Statement.Context
	}
	return db.Session(session).Clauses(dbresolver.Read)
}

// buildCollectorWindowSuccessQuery 生成窗口重试成功统计查询，兼容 finished_at 为空的完成记录。
func buildCollectorWindowSuccessQuery(db *gorm.DB, since time.Time) *gorm.DB {
	return freshDB(db).Model(&model.CollectorFailedEvent{}).
		Select(
			"COUNT(*) AS success_count, "+
				"COALESCE(AVG(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS avg_cost_ms, "+
				"COALESCE(MAX(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS max_cost_ms",
		).
		Where("state = ? AND "+collectorOverviewDoneAtFilter, model.CollectorFailedEventStateDone, since, since)
}

// buildCollectorBizTypeOverviewQuery 生成 biz_type 热点查询，空业务类型统一归入 unknown。
func buildCollectorBizTypeOverviewQuery(db *gorm.DB, now, since time.Time) *gorm.DB {
	return freshDB(db).Model(&model.CollectorFailedEvent{}).
		Select(
			collectorOverviewBizTypeExpr+" AS biz_type, "+
				"SUM(CASE WHEN (state = ? OR state = ?) AND next_run_at <= ? THEN 1 ELSE 0 END) AS ready_count, "+
				"SUM(CASE WHEN state = ? THEN 1 ELSE 0 END) AS running_count, "+
				"SUM(CASE WHEN state = ? THEN 1 ELSE 0 END) AS retry_count, "+
				"SUM(CASE WHEN state = ? THEN 1 ELSE 0 END) AS dead_count, "+
				"SUM(CASE WHEN state = ? AND "+collectorOverviewDoneAtFilter+" THEN 1 ELSE 0 END) AS recent_success_count, "+
				"SUM(CASE WHEN (state = ? OR state = ?) AND updated_at >= ? THEN 1 ELSE 0 END) AS recent_fail_count, "+
				"COALESCE(AVG(CASE WHEN state = ? AND started_at IS NOT NULL AND finished_at IS NOT NULL AND "+collectorOverviewDoneAtFilter+" THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS recent_avg_cost_ms, "+
				"COALESCE(MAX(CASE WHEN state = ? AND started_at IS NOT NULL AND finished_at IS NOT NULL AND "+collectorOverviewDoneAtFilter+" THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS recent_max_cost_ms",
			model.CollectorFailedEventStatePending,
			model.CollectorFailedEventStateRetry,
			now,
			model.CollectorFailedEventStateRunning,
			model.CollectorFailedEventStateRetry,
			model.CollectorFailedEventStateDead,
			model.CollectorFailedEventStateDone,
			since,
			since,
			model.CollectorFailedEventStateRetry,
			model.CollectorFailedEventStateDead,
			since,
			model.CollectorFailedEventStateDone,
			since,
			since,
			model.CollectorFailedEventStateDone,
			since,
			since,
		).
		Group(collectorOverviewBizTypeExpr).
		Order("ready_count DESC, retry_count DESC, dead_count DESC, running_count DESC, recent_success_count DESC, recent_fail_count DESC, biz_type ASC").
		Limit(defaultCollectorOverviewBizTypeTopLimit)
}

// RunNow 手动触发执行一轮 Collector 失败账本重试。
func (l *CollectorLogic) RunNow(req *types.RunCollectorReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	if l.Svc == nil || l.Svc.Collector == nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyCollectorUnavailable).
			WithError(types.Nil)
	}

	processed, err := l.Svc.Collector.RunNow(l.Ctx, req.Limit)
	invalidateCollectorOverviewCache()
	resp := &types.RunCollectorResp{
		Processed: processed,
	}
	if err != nil {
		resp.Error = err.Error()
		resp.Message = l.Message(i18n.MsgKeyCollectorRunPartialFailed)
		return types.NewBizResult(codes.ServerError).WithData(resp).WithError(errors.Tag(err))
	}
	resp.Message = l.Message(i18n.MsgKeyCollectorRunSuccess)
	return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeySuccess).WithData(resp)
}

// RetryFailures 对指定 Collector 失败事件发起人工重试/延迟重试。
func (l *CollectorLogic) RetryFailures(req *types.RetryCollectorFailuresReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}

	db := l.Svc.WriteDB(svc.DatabaseMain)

	delay := time.Duration(req.DelaySeconds) * time.Second
	resetAttempt := true
	if req.ResetAttempt != nil {
		resetAttempt = *req.ResetAttempt
	}

	now := time.Now()
	updates := map[string]any{
		"state":       model.CollectorFailedEventStateRetry,
		"next_run_at": now.Add(delay),
		"updated_at":  now,
		"started_at":  nil,
		"finished_at": nil,
		"claim_token": "",
		"lease_until": nil,
	}
	if resetAttempt {
		updates["attempt"] = 0
	}

	result := db.Model(&model.CollectorFailedEvent{}).
		Where("id IN ? AND state IN ?", req.IDs, []model.CollectorFailedEventState{
			model.CollectorFailedEventStateRetry,
			model.CollectorFailedEventStateDead,
		}).
		Updates(updates)
	if err := result.Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.RetryFailures Updates").ToBizResult()
	}
	invalidateCollectorOverviewCache()

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.RetryCollectorFailuresResp{
			Updated: int(result.RowsAffected),
		})
}

// toCollectorFailureResp 将失败账本记录转换为 Collector 失败事件响应。
func toCollectorFailureResp(row *model.CollectorFailedEvent) *types.CollectorFailureResp {
	if row == nil {
		return &types.CollectorFailureResp{}
	}

	resp := &types.CollectorFailureResp{
		ID:           row.ID,
		EventID:      row.EventID,
		BizType:      row.BizType,
		PartitionKey: row.PartitionKey,
		Payload:      row.Payload,
		State:        int(row.State),
		Attempt:      row.Attempt,
		NextRunAt:    row.NextRunAt.Format(time.DateTime),
		LastError:    row.LastError,
		CreatedAt:    row.CreatedAt.Format(time.DateTime),
		UpdatedAt:    row.UpdatedAt.Format(time.DateTime),
	}
	if row.StartedAt != nil {
		resp.StartedAt = row.StartedAt.Format(time.DateTime)
	}
	if row.FinishedAt != nil {
		resp.FinishedAt = row.FinishedAt.Format(time.DateTime)
	}
	return resp
}

// resolveCollectorKafkaBatchSize 返回概览使用的 Kafka producer 批次配置。
func resolveCollectorKafkaBatchSize(cfg config.CollectorKafkaConfig) int {
	if cfg.WriteBatchSize > 0 {
		return cfg.WriteBatchSize
	}
	return defaultCollectorOverviewKafkaBatchSize
}

// resolveCollectorFailureBatchSize 返回概览使用的失败账本重试批次配置。
func resolveCollectorFailureBatchSize(cfg config.CollectorFailureRetryConfig) int {
	if cfg.RunnerBatchSize > 0 {
		return cfg.RunnerBatchSize
	}
	return defaultCollectorOverviewFailureBatchSize
}

// resolveCollectorFailureInterval 返回概览使用的失败账本重试轮询间隔。
func resolveCollectorFailureInterval(cfg config.CollectorFailureRetryConfig) int {
	if cfg.RunnerIntervalSeconds > 0 {
		return cfg.RunnerIntervalSeconds
	}
	return defaultCollectorOverviewFailureInterval
}

// resolveCollectorRunningLease 返回概览使用的 running 租约秒数。
func resolveCollectorRunningLease(cfg config.CollectorFailureRetryConfig) int {
	if cfg.RunningLeaseSeconds > 0 {
		return cfg.RunningLeaseSeconds
	}
	return defaultCollectorOverviewRunningLease
}

// resolveCollectorMaxRetry 返回概览使用的最大重试次数。
func resolveCollectorMaxRetry(cfg config.CollectorFailureRetryConfig) int {
	if cfg.MaxRetryTimes > 0 {
		return cfg.MaxRetryTimes
	}
	return defaultCollectorOverviewMaxRetryTimes
}

// loadCollectorOverviewCache 尝试读取仍在有效期内的概览缓存。
func loadCollectorOverviewCache() *types.CollectorOverviewResp {
	collectorOverviewCache.mu.RLock()
	defer collectorOverviewCache.mu.RUnlock()
	if collectorOverviewCache.data == nil || time.Now().After(collectorOverviewCache.expiresAt) {
		return nil
	}
	return cloneCollectorOverviewResp(collectorOverviewCache.data, collectorOverviewCache.generatedAt, true)
}

// invalidateCollectorOverviewCache 在手动执行、人工重试等关键操作后主动清空概览缓存。
func invalidateCollectorOverviewCache() {
	collectorOverviewCache.mu.Lock()
	defer collectorOverviewCache.mu.Unlock()
	collectorOverviewCache.data = nil
	collectorOverviewCache.generatedAt = time.Time{}
	collectorOverviewCache.expiresAt = time.Time{}
}

// cloneCollectorOverviewResp 复制概览响应，并按当前请求补齐缓存元信息，避免直接暴露共享指针。
func cloneCollectorOverviewResp(resp *types.CollectorOverviewResp, generatedAt time.Time, cacheHit bool) *types.CollectorOverviewResp {
	if resp == nil {
		return nil
	}
	cloned := *resp
	cloned.BizTypeTop15m = append([]types.CollectorBizTypeStatResp(nil), resp.BizTypeTop15m...)
	cloned.RuntimeMetrics.BizTypeTop15m = append([]types.CollectorRuntimeBizTypeResp(nil), resp.RuntimeMetrics.BizTypeTop15m...)
	if !generatedAt.IsZero() {
		cloned.OverviewGeneratedAt = generatedAt.Format(time.DateTime)
		cloned.OverviewCacheTTLSeconds = int(collectorOverviewCacheTTL / time.Second)
		cloned.OverviewCacheAgeSeconds = max(0, int64(time.Since(generatedAt).Seconds()))
	}
	cloned.OverviewCacheHit = cacheHit
	return &cloned
}

// collectorRuntimeMetrics 获取当前进程 Collector 运行态指标，避免正常成功事件落库。
func (l *CollectorLogic) collectorRuntimeMetrics() types.CollectorRuntimeMetricsResp {
	if l == nil || l.Svc == nil || l.Svc.Collector == nil {
		return toCollectorRuntimeMetricsResp(collectorx.RuntimeMetricsSnapshot{
			Scope:       "current_process",
			GeneratedAt: time.Now(),
			Recent1m:    collectorx.RuntimeMetricWindow{WindowMinutes: 1},
			Recent5m:    collectorx.RuntimeMetricWindow{WindowMinutes: 5},
			Recent15m:   collectorx.RuntimeMetricWindow{WindowMinutes: 15},
		})
	}
	return toCollectorRuntimeMetricsResp(l.Svc.Collector.RuntimeMetricsSnapshot())
}

// toCollectorRuntimeMetricsResp 转换 Collector 运行态指标响应。
func toCollectorRuntimeMetricsResp(snapshot collectorx.RuntimeMetricsSnapshot) types.CollectorRuntimeMetricsResp {
	resp := types.CollectorRuntimeMetricsResp{
		Enabled:       snapshot.Enabled,
		Scope:         snapshot.Scope,
		StartedAt:     formatCollectorRuntimeTime(snapshot.StartedAt),
		GeneratedAt:   formatCollectorRuntimeTime(snapshot.GeneratedAt),
		Totals:        toCollectorRuntimeTotalsResp(snapshot.Totals),
		Recent1m:      toCollectorRuntimeWindowResp(snapshot.Recent1m),
		Recent5m:      toCollectorRuntimeWindowResp(snapshot.Recent5m),
		Recent15m:     toCollectorRuntimeWindowResp(snapshot.Recent15m),
		BizTypeTop15m: make([]types.CollectorRuntimeBizTypeResp, 0, len(snapshot.BizTypeTop15m)),
	}
	for _, item := range snapshot.BizTypeTop15m {
		resp.BizTypeTop15m = append(resp.BizTypeTop15m, toCollectorRuntimeBizTypeResp(item))
	}
	return resp
}

// toCollectorRuntimeTotalsResp 转换运行态累计计数。
func toCollectorRuntimeTotalsResp(totals collectorx.RuntimeMetricTotals) types.CollectorRuntimeTotalsResp {
	return types.CollectorRuntimeTotalsResp{
		Published:     totals.Published,
		PublishFailed: totals.PublishFailed,
		Consumed:      totals.Consumed,
		Invalid:       totals.Invalid,
		Duplicate:     totals.Duplicate,
		Processed:     totals.Processed,
		Succeeded:     totals.Succeeded,
		Failed:        totals.Failed,
		Batches:       totals.Batches,
		FailedBatches: totals.FailedBatches,
		Dead:          totals.Dead,
	}
}

// toCollectorRuntimeWindowResp 转换运行态窗口计数。
func toCollectorRuntimeWindowResp(window collectorx.RuntimeMetricWindow) types.CollectorRuntimeWindowResp {
	return types.CollectorRuntimeWindowResp{
		WindowMinutes:              window.WindowMinutes,
		CollectorRuntimeTotalsResp: toCollectorRuntimeTotalsResp(window.RuntimeMetricTotals),
		AvgBatchSize:               window.AvgBatchSize,
		AvgCostMs:                  window.AvgCostMs,
		MaxCostMs:                  window.MaxCostMs,
		LastEventAt:                formatCollectorRuntimeTime(window.LastEventAt),
	}
}

// toCollectorRuntimeBizTypeResp 转换运行态 bizType 热点计数。
func toCollectorRuntimeBizTypeResp(item collectorx.RuntimeBizTypeMetric) types.CollectorRuntimeBizTypeResp {
	return types.CollectorRuntimeBizTypeResp{
		BizType:                    item.BizType,
		CollectorRuntimeTotalsResp: toCollectorRuntimeTotalsResp(item.RuntimeMetricTotals),
		AvgBatchSize:               item.AvgBatchSize,
		AvgCostMs:                  item.AvgCostMs,
		MaxCostMs:                  item.MaxCostMs,
		LastEventAt:                formatCollectorRuntimeTime(item.LastEventAt),
	}
}

// formatCollectorRuntimeTime 统一格式化运行态指标时间。
func formatCollectorRuntimeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.DateTime)
}
