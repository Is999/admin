package collector

import (
	corelogic "admin/internal/logic"
	"net/http"
	"sync"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// Collector 概览默认参数限制后台聚合批次、缓存时间和展示数量。
const (
	defaultCollectorOverviewKafkaBatchSize    = 500             // 概览展示 Kafka 默认批量拉取数量
	defaultCollectorOverviewRedisCount        = 500             // 概览展示 Redis Stream 默认查询数量
	defaultCollectorOverviewDBRunnerBatchSize = 500             // 概览展示 DB runner 默认批次大小
	defaultCollectorOverviewDBRunnerInterval  = 1               // 概览展示 DB runner 默认轮询间隔秒数
	defaultCollectorOverviewRunningLease      = 600             // 概览展示 DB outbox 默认运行租约秒数
	defaultCollectorOverviewMaxRetryTimes     = 8               // 概览展示 DB outbox 默认最大重试次数
	defaultCollectorOverviewBizTypeTopLimit   = 5               // 概览展示 biz_type 排行数量上限
	collectorOverviewCacheTTL                 = 5 * time.Second // Collector 概览短缓存有效期
)

// collectorOverviewCache 概览缓存
var collectorOverviewCache = struct {
	mu          sync.RWMutex                 // 保护概览缓存读写，避免并发请求重复打满 MySQL
	expiresAt   time.Time                    // 缓存过期时间
	generatedAt time.Time                    // 当前缓存对应的概览生成时间
	data        *types.CollectorOverviewResp // 最近一次成功聚合的概览快照
}{}

// CollectorLogic 承载通用收集器的管理后台能力：
// 1) 查询任务列表与任务详情；
// 2) 手动触发执行；
// 3) 手动对死信/重试任务发起重试或延迟重试。
type CollectorLogic struct {
	*corelogic.BaseLogic // 复用上下文、日志、数据库和审计能力
}

// NewCollectorLogic 创建通用收集器管理逻辑对象。
func NewCollectorLogic(r *http.Request, svcCtx *svc.ServiceContext) *CollectorLogic {
	return &CollectorLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// Overview 返回 Collector 当前积压、配置与近期处理表现。
func (l *CollectorLogic) Overview() *types.BizResult {
	db := l.Svc.ReadDB(svc.DatabaseMain)
	resp, err := l.cachedOverview(db)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.Overview").ToBizResult()
	}
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

// ListTasks 查询 collector outbox 任务列表。
func (l *CollectorLogic) ListTasks(req *types.ListCollectorTasksReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}

	db := l.Svc.ReadDB(svc.DatabaseMain)

	query := db.Model(&model.CollectorOutbox{})
	if req.BizType != "" {
		query = query.Where("biz_type = ?", req.BizType)
	}
	if req.Transport != "" {
		query = query.Where("transport = ?", req.Transport)
	}
	if req.State != nil {
		query = query.Where("state = ?", *req.State)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.ListTasks Count").ToBizResult()
	}

	offset := (req.Page - 1) * req.PageSize
	rows := make([]model.CollectorOutbox, 0, req.PageSize)
	if err := query.Order("id DESC").Offset(offset).Limit(req.PageSize).Find(&rows).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.ListTasks Find").ToBizResult()
	}

	list := make([]types.CollectorTaskResp, 0, len(rows))
	for i := range rows {
		list = append(list, *toCollectorTaskResp(&rows[i]))
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(types.ListResp[types.CollectorTaskResp]{
			List:  list,
			Total: total,
		})
}

// buildOverview 聚合 Collector 当前积压、配置与近窗口吞吐数据。
func (l *CollectorLogic) buildOverview(db *gorm.DB) (*types.CollectorOverviewResp, error) {
	now := time.Now()
	cfg := l.Svc.CurrentConfig().Collector
	resp := &types.CollectorOverviewResp{
		KafkaBatchSize:       resolveCollectorKafkaBatchSize(cfg.Kafka),
		RedisReadCount:       resolveCollectorRedisCount(cfg.Redis),
		DBRunnerBatchSize:    resolveCollectorDBRunnerBatchSize(cfg.DB),
		DBRunnerIntervalSecs: resolveCollectorDBRunnerInterval(cfg.DB),
		RunningLeaseSeconds:  resolveCollectorRunningLease(cfg.DB),
		MaxRetryTimes:        resolveCollectorMaxRetry(cfg.DB),
		Recent1m:             types.CollectorWindowStatResp{WindowMinutes: 1},
		Recent5m:             types.CollectorWindowStatResp{WindowMinutes: 5},
		Recent15m:            types.CollectorWindowStatResp{WindowMinutes: 15},
		BizTypeTop15m:        make([]types.CollectorBizTypeStatResp, 0),
		TransportStats:       make([]types.CollectorTransportStatResp, 0),
	}

	stateCounts := make([]struct {
		State int   `gorm:"column:state"`
		Total int64 `gorm:"column:total"`
	}, 0)
	if err := db.Model(&model.CollectorOutbox{}).
		Select("state, COUNT(*) AS total").
		Group("state").
		Scan(&stateCounts).Error; err != nil {
		return nil, errors.Tag(err)
	}
	for _, item := range stateCounts {
		switch item.State {
		case int(model.CollectorOutboxStatePending):
			resp.PendingCount = item.Total
		case int(model.CollectorOutboxStateRunning):
			resp.RunningCount = item.Total
		case int(model.CollectorOutboxStateDone):
			resp.DoneCount = item.Total
		case int(model.CollectorOutboxStateRetry):
			resp.RetryCount = item.Total
		case int(model.CollectorOutboxStateDead):
			resp.DeadCount = item.Total
		}
	}

	if err := db.Model(&model.CollectorOutbox{}).
		Where("(state = ? OR state = ?) AND next_run_at <= ?", model.CollectorOutboxStatePending, model.CollectorOutboxStateRetry, now).
		Count(&resp.ReadyCount).Error; err != nil {
		return nil, errors.Tag(err)
	}

	leaseCutoff := now.Add(-time.Duration(resp.RunningLeaseSeconds) * time.Second)
	if err := db.Model(&model.CollectorOutbox{}).
		Where("state = ? AND started_at IS NOT NULL AND started_at <= ?", model.CollectorOutboxStateRunning, leaseCutoff).
		Count(&resp.RunningTimeoutCount).Error; err != nil {
		return nil, errors.Tag(err)
	}

	oldestReady := struct {
		NextRunAt *time.Time `gorm:"column:next_run_at"`
	}{}
	if err := db.Model(&model.CollectorOutbox{}).
		Select("MIN(next_run_at) AS next_run_at").
		Where("(state = ? OR state = ?) AND next_run_at <= ?", model.CollectorOutboxStatePending, model.CollectorOutboxStateRetry, now).
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
	if err := l.fillOverviewTransportStats(db, now, resp); err != nil {
		return nil, errors.Tag(err)
	}
	return resp, nil
}

// fillOverviewWindow 聚合指定窗口内的成功/失败/耗时信息。
func (l *CollectorLogic) fillOverviewWindow(db *gorm.DB, now time.Time, window *types.CollectorWindowStatResp) error {
	if window == nil || window.WindowMinutes <= 0 {
		return nil
	}
	since := now.Add(-time.Duration(window.WindowMinutes) * time.Minute)
	successRow := struct {
		SuccessCount int64   `gorm:"column:success_count"`
		AvgCostMs    float64 `gorm:"column:avg_cost_ms"`
		MaxCostMs    float64 `gorm:"column:max_cost_ms"`
	}{}
	if err := db.Model(&model.CollectorOutbox{}).
		Select(
			"COUNT(*) AS success_count, "+
				"COALESCE(AVG(TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0), 0) AS avg_cost_ms, "+
				"COALESCE(MAX(TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0), 0) AS max_cost_ms",
		).
		Where("state = ? AND started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= ?", model.CollectorOutboxStateDone, since).
		Scan(&successRow).Error; err != nil {
		return errors.Tag(err)
	}
	window.SuccessCount = successRow.SuccessCount
	window.AvgCostMs = successRow.AvgCostMs
	window.MaxCostMs = successRow.MaxCostMs

	if err := db.Model(&model.CollectorOutbox{}).
		Where("(state = ? OR state = ?) AND updated_at >= ?", model.CollectorOutboxStateRetry, model.CollectorOutboxStateDead, since).
		Count(&window.FailCount).Error; err != nil {
		return errors.Tag(err)
	}
	return nil
}

// fillOverviewBizTypeTop 聚合最近 15 分钟重点业务排行，便于快速定位热点与慢业务。
func (l *CollectorLogic) fillOverviewBizTypeTop(db *gorm.DB, now time.Time, resp *types.CollectorOverviewResp) error {
	if resp == nil {
		return nil
	}
	since := now.Add(-15 * time.Minute)
	rows := make([]types.CollectorBizTypeStatResp, 0, defaultCollectorOverviewBizTypeTopLimit)
	if err := db.Model(&model.CollectorOutbox{}).
		Select(
			"biz_type AS biz_type, "+
				"SUM(CASE WHEN (state = 0 OR state = 3) AND next_run_at <= NOW() THEN 1 ELSE 0 END) AS ready_count, "+
				"SUM(CASE WHEN state = 1 THEN 1 ELSE 0 END) AS running_count, "+
				"SUM(CASE WHEN state = 3 THEN 1 ELSE 0 END) AS retry_count, "+
				"SUM(CASE WHEN state = 4 THEN 1 ELSE 0 END) AS dead_count, "+
				"SUM(CASE WHEN state = 2 AND finished_at >= ? THEN 1 ELSE 0 END) AS recent_success_count, "+
				"SUM(CASE WHEN (state = 3 OR state = 4) AND updated_at >= ? THEN 1 ELSE 0 END) AS recent_fail_count, "+
				"COALESCE(AVG(CASE WHEN state = 2 AND started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= ? THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS recent_avg_cost_ms, "+
				"COALESCE(MAX(CASE WHEN state = 2 AND started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= ? THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END), 0) AS recent_max_cost_ms",
			since,
			since,
			since,
			since,
		).
		Where("biz_type <> ''").
		Group("biz_type").
		Order(
			"SUM(CASE WHEN (state = 0 OR state = 3) AND next_run_at <= NOW() THEN 1 ELSE 0 END) DESC, " +
				"SUM(CASE WHEN state = 3 THEN 1 ELSE 0 END) DESC, " +
				"SUM(CASE WHEN state = 4 THEN 1 ELSE 0 END) DESC, " +
				"SUM(CASE WHEN state = 1 THEN 1 ELSE 0 END) DESC, " +
				"SUM(CASE WHEN state = 2 AND finished_at >= " + db.Dialector.Explain("?", since) + " THEN 1 ELSE 0 END) DESC",
		).
		Limit(defaultCollectorOverviewBizTypeTopLimit).
		Scan(&rows).Error; err != nil {
		return errors.Tag(err)
	}
	resp.BizTypeTop15m = rows
	return nil
}

// fillOverviewTransportStats 聚合不同入队来源的任务分布，便于判断来源侧压力结构。
func (l *CollectorLogic) fillOverviewTransportStats(db *gorm.DB, now time.Time, resp *types.CollectorOverviewResp) error {
	if resp == nil {
		return nil
	}
	rows := make([]types.CollectorTransportStatResp, 0, 4)
	if err := db.Model(&model.CollectorOutbox{}).
		Select(
			"transport AS transport, "+
				"COUNT(*) AS total_count, "+
				"SUM(CASE WHEN (state = 0 OR state = 3) AND next_run_at <= ? THEN 1 ELSE 0 END) AS ready_count, "+
				"SUM(CASE WHEN state = 1 THEN 1 ELSE 0 END) AS running_count, "+
				"SUM(CASE WHEN state = 3 THEN 1 ELSE 0 END) AS retry_count, "+
				"SUM(CASE WHEN state = 4 THEN 1 ELSE 0 END) AS dead_count, "+
				"SUM(CASE WHEN state = 2 THEN 1 ELSE 0 END) AS done_count",
			now,
		).
		Where("transport <> ''").
		Group("transport").
		Order("COUNT(*) DESC").
		Scan(&rows).Error; err != nil {
		return errors.Tag(err)
	}
	resp.TransportStats = rows
	return nil
}

// RunNow 手动触发执行一轮 collector outbox 任务。
func (l *CollectorLogic) RunNow(req *types.RunCollectorReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if l.Svc == nil || l.Svc.Collector == nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalErrorFormat, "Collector 未初始化").
			WithError(types.Nil)
	}

	if req.BizType != "" {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, "collector 手动执行暂不支持按 bizType 过滤").
			WithError(types.Nil)
	}
	processed, err := l.Svc.Collector.RunNow(l.Ctx, req.Limit)
	invalidateCollectorOverviewCache()
	resp := &types.RunCollectorResp{
		Processed: processed,
	}
	if err != nil {
		resp.Error = err.Error()
		resp.Message = "本次执行出现失败任务，已按重试策略回写任务状态"
		return types.NewBizResult(codes.ServerError).WithData(resp).WithError(errors.Tag(err))
	}
	resp.Message = "执行完成"
	return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeySuccess).WithData(resp)
}

// RetryTasks 对指定 collector outbox 任务发起人工重试/延迟重试。
func (l *CollectorLogic) RetryTasks(req *types.RetryCollectorTasksReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(nil)
	}
	if len(req.IDs) == 0 {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, "ids 不能为空").
			WithError(types.Nil)
	}

	db := l.Svc.WriteDB(svc.DatabaseMain)

	delay := time.Duration(req.DelaySeconds) * time.Second
	if delay < 0 {
		delay = 0
	}
	resetAttempt := true
	if req.ResetAttempt || req.DelaySeconds != 0 {
		// ResetAttempt 默认 true；前端未传时保持默认。
		// DelaySeconds 非 0 时即表示人工干预，仍按默认重置重试次数处理。
		resetAttempt = true
	}

	now := time.Now()
	updates := map[string]any{
		"state":       model.CollectorOutboxStateRetry,
		"next_run_at": now.Add(delay),
		"updated_at":  now,
		"started_at":  nil,
		"finished_at": nil,
	}
	if resetAttempt {
		updates["attempt"] = 0
	}

	tx := db.Model(&model.CollectorOutbox{}).Where("id IN ?", req.IDs)
	if err := tx.Updates(updates).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "CollectorLogic.RetryTasks Updates").ToBizResult()
	}
	invalidateCollectorOverviewCache()

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.RetryCollectorTasksResp{
			Updated: len(req.IDs),
		})
}

// toCollectorTaskResp 将 collector outbox 记录转换为 collector task 响应。
func toCollectorTaskResp(row *model.CollectorOutbox) *types.CollectorTaskResp {
	if row == nil {
		return &types.CollectorTaskResp{}
	}

	resp := &types.CollectorTaskResp{
		ID:           row.ID,
		EventID:      row.EventID,
		BizType:      row.BizType,
		PartitionKey: row.PartitionKey,
		Payload:      row.Payload,
		Transport:    row.Transport,
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

// resolveCollectorKafkaBatchSize 返回概览使用的 Kafka 批次配置。
func resolveCollectorKafkaBatchSize(cfg config.CollectorKafkaConfig) int {
	if cfg.BatchSize > 0 {
		return cfg.BatchSize
	}
	return defaultCollectorOverviewKafkaBatchSize
}

// resolveCollectorRedisCount 返回概览使用的 Redis 读取批次配置。
func resolveCollectorRedisCount(cfg config.CollectorRedisConfig) int64 {
	if cfg.Count > 0 {
		return cfg.Count
	}
	return defaultCollectorOverviewRedisCount
}

// resolveCollectorDBRunnerBatchSize 返回概览使用的 DB worker 批次配置。
func resolveCollectorDBRunnerBatchSize(cfg config.CollectorDBConfig) int {
	if cfg.RunnerBatchSize > 0 {
		return cfg.RunnerBatchSize
	}
	return defaultCollectorOverviewDBRunnerBatchSize
}

// resolveCollectorDBRunnerInterval 返回概览使用的 DB worker 轮询间隔。
func resolveCollectorDBRunnerInterval(cfg config.CollectorDBConfig) int {
	if cfg.RunnerIntervalSeconds > 0 {
		return cfg.RunnerIntervalSeconds
	}
	return defaultCollectorOverviewDBRunnerInterval
}

// resolveCollectorRunningLease 返回概览使用的 running 租约秒数。
func resolveCollectorRunningLease(cfg config.CollectorDBConfig) int {
	if cfg.RunningLeaseSeconds > 0 {
		return cfg.RunningLeaseSeconds
	}
	return defaultCollectorOverviewRunningLease
}

// resolveCollectorMaxRetry 返回概览使用的最大重试次数。
func resolveCollectorMaxRetry(cfg config.CollectorDBConfig) int {
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
	cloned.TransportStats = append([]types.CollectorTransportStatResp(nil), resp.TransportStats...)
	if !generatedAt.IsZero() {
		cloned.OverviewGeneratedAt = generatedAt.Format(time.DateTime)
		cloned.OverviewCacheTTLSeconds = int(collectorOverviewCacheTTL / time.Second)
		cloned.OverviewCacheAgeSeconds = max(0, int64(time.Since(generatedAt).Seconds()))
	}
	cloned.OverviewCacheHit = cacheHit
	return &cloned
}
