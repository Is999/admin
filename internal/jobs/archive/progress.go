package archive

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// ProgressPhaseInactive 表示当前运行态没有启用同名归档任务。
	ProgressPhaseInactive = "inactive"
	// ProgressPhaseNotStarted 表示任务尚未生成水位或归档区间。
	ProgressPhaseNotStarted = "not_started"
	// ProgressPhasePending 表示已有待领取归档区间。
	ProgressPhasePending = "pending"
	// ProgressPhaseRunning 表示正在复制热表数据到历史表。
	ProgressPhaseRunning = "running"
	// ProgressPhaseLeaseExpired 表示执行区间仍为运行态，但 worker 租约已经失效。
	ProgressPhaseLeaseExpired = "lease_expired"
	// ProgressPhaseFailed 表示存在等待重试的失败归档区间。
	ProgressPhaseFailed = "failed"
	// ProgressPhaseWaitingDelete 表示已完成归档，正在等待热表删除窗口。
	ProgressPhaseWaitingDelete = "waiting_delete"
	// ProgressPhaseDeleting 表示正在删除已经归档的热表数据。
	ProgressPhaseDeleting = "deleting"
	// ProgressPhaseCaughtUp 表示归档水位已追到当前可归档上界。
	ProgressPhaseCaughtUp = "caught_up"
	// ProgressPhaseIdle 表示已有归档记录，但当前没有可见执行动作。
	ProgressPhaseIdle = "idle"

	// progressRecentSegmentLimit 限制详情接口返回的最近区间数量，避免控制表历史增长后返回大结果集。
	progressRecentSegmentLimit = 20
)

// ProgressCounts 汇总单个归档任务各区间状态数量。
type ProgressCounts struct {
	Total    int64 // 区间总数
	Pending  int64 // 待领取区间数
	Running  int64 // 正在归档区间数
	Done     int64 // 已归档待删除区间数
	Deleting int64 // 正在删除区间数
	Deleted  int64 // 已完成热表删除区间数
	Failed   int64 // 归档失败待重试区间数
}

// ProgressSegment 描述详情接口需要展示的归档区间执行状态。
type ProgressSegment struct {
	ID                       uint64       `gorm:"column:id"`                 // 区间 ID
	HistoryTableName         string       `gorm:"column:history_table_name"` // 历史表名
	RangeStart               time.Time    `gorm:"column:range_start"`        // 区间起点（含）
	RangeEnd                 time.Time    `gorm:"column:range_end"`          // 区间终点（不含）
	Status                   string       `gorm:"column:status"`             // 区间状态
	WorkerID                 string       `gorm:"column:worker_id"`          // 当前持有 worker
	LeaseExpiresAt           sql.NullTime `gorm:"column:lease_expires_at"`   // 当前租约过期时间
	LastArchivedID           int64        `gorm:"column:last_archived_id"`   // 最近归档主键游标
	LastArchivedTime         sql.NullTime `gorm:"column:last_archived_time"` // 最近归档时间游标
	RowsArchived             int64        `gorm:"column:rows_archived"`      // 累计归档行数
	AttemptCount             int          `gorm:"column:attempt_count"`      // 领取次数
	UpdatedAt                time.Time    `gorm:"column:updated_at"`         // 最近更新时间
	CompletedAt              sql.NullTime `gorm:"column:completed_at"`       // 归档完成时间
	EstimatedProgressPercent *float64     `gorm:"-"`                         // 复制阶段按时间游标估算的区间进度百分比
}

// Progress 描述归档任务当前运行态、水位和最近区间状态。
type Progress struct {
	JobName            string            // 归档任务名
	RuntimeMatched     bool              // 当前运行态是否存在同名任务
	RuntimeEnabled     bool              // 当前运行态归档模块和同名任务是否均已启用
	deleteDisabled     bool              // 当前运行态是否禁止删除热表数据
	SchemaReady        bool              // 归档水位表和区间表是否均已创建
	Phase              string            // 当前执行阶段
	WatermarkTime      sql.NullTime      // 已完整复制到历史表的排他上界
	WatermarkUpdatedAt sql.NullTime      // 水位最近更新时间
	EligibleUntil      sql.NullTime      // 当前允许归档到的排他上界
	PlannedUntil       sql.NullTime      // 已规划区间的最远排他上界
	LagSeconds         sql.NullInt64     // 水位或最早区间距离可归档上界的秒数
	Counts             ProgressCounts    // 各区间状态数量
	CurrentSegment     *ProgressSegment  // 当前活动或最近租约过期的执行区间；无执行记录时为空
	RecentSegments     []ProgressSegment // 最近区间按起点倒序排列，最多 20 条
	FetchedAt          time.Time         // 本次运行态快照生成时间
}

// progressCountRow 承接按区间状态分组的计数。
type progressCountRow struct {
	Status string `gorm:"column:status"` // 区间状态
	Total  int64  `gorm:"column:total"`  // 该状态区间数
}

// progressBounds 承接单个任务现存区间的起止边界。
type progressBounds struct {
	FirstRangeStart sql.NullTime `gorm:"column:first_range_start"` // 最早现存区间起点
	PlannedUntil    sql.NullTime `gorm:"column:planned_until"`     // 最远已规划区间终点
}

// Progress 查询单个归档任务的控制表水位和最近区间，不读取或统计业务热表。
func (s *Service) Progress(ctx context.Context, jobName string) (Progress, error) {
	jobName = strings.TrimSpace(jobName)
	if jobName == "" {
		return Progress{}, errors.New("归档任务名为空")
	}
	if s == nil || s.svcCtx == nil {
		return Progress{}, errors.New("归档服务上下文未初始化")
	}

	fetchedAt := time.Now()
	progress := Progress{
		JobName:        jobName,
		RecentSegments: make([]ProgressSegment, 0),
		FetchedAt:      fetchedAt,
	}
	job, matched, enabled := s.progressRuntimeJob(jobName)
	progress.RuntimeMatched = matched
	progress.RuntimeEnabled = enabled
	if enabled {
		progress.deleteDisabled = job.DeleteDisabled
		progress.EligibleUntil = sql.NullTime{Time: s.archiveEligibleUntil(job, fetchedAt), Valid: true}
	}

	controlDB := s.jobControlWriteDB()
	if controlDB == nil {
		return Progress{}, errors.Errorf("归档控制库连接为空: job=%s control_database=%s", jobName, s.controlDatabaseName())
	}
	readyTables, err := existingTables(ctx, controlDB, []string{tableNameWatermark, tableNameSegment})
	if err != nil {
		return Progress{}, errors.Wrapf(err, "查询归档控制表状态失败 job=%s", jobName)
	}
	_, watermarkReady := readyTables[tableNameWatermark]
	_, segmentReady := readyTables[tableNameSegment]
	progress.SchemaReady = watermarkReady && segmentReady

	if watermarkReady {
		watermark, err := s.loadWatermark(ctx, controlDB, jobName)
		if err != nil {
			return Progress{}, errors.Wrapf(err, "查询归档水位失败 job=%s", jobName)
		}
		if watermark != nil {
			progress.WatermarkTime = watermark.WatermarkTime
			progress.WatermarkUpdatedAt = sql.NullTime{Time: watermark.UpdatedAt, Valid: !watermark.UpdatedAt.IsZero()}
		}
	}
	if segmentReady {
		bounds, err := loadProgressSegments(ctx, controlDB, jobName, &progress)
		if err != nil {
			return Progress{}, errors.Wrapf(err, "查询归档区间进度失败 job=%s", jobName)
		}
		progress.PlannedUntil = bounds.PlannedUntil
		progress.LagSeconds = archiveProgressLag(progress.EligibleUntil, progress.WatermarkTime, bounds.FirstRangeStart)
	}
	progress.Phase = archiveProgressPhase(progress)
	return progress, nil
}

// progressRuntimeJob 判断同名任务是否已进入当前运行态并返回归一化配置。
func (s *Service) progressRuntimeJob(jobName string) (jobConfig, bool, bool) {
	cfg := s.svcCtx.CurrentConfig().Archive
	for _, item := range cfg.Jobs {
		if strings.TrimSpace(item.Name) != jobName {
			continue
		}
		if !cfg.Enabled || !item.Enabled {
			return jobConfig{}, true, false
		}
		job, ok := s.jobByName(jobName)
		return job, true, ok
	}
	return jobConfig{}, false, false
}

// loadProgressSegments 查询区间汇总、当前执行项和最近区间。
func loadProgressSegments(ctx context.Context, db *gorm.DB, jobName string, progress *Progress) (progressBounds, error) {
	counts, err := loadProgressCounts(ctx, db, jobName)
	if err != nil {
		return progressBounds{}, errors.Tag(err)
	}
	progress.Counts = counts

	current, err := loadCurrentProgressSegment(ctx, db, jobName)
	if err != nil {
		return progressBounds{}, errors.Tag(err)
	}
	progress.CurrentSegment = current

	if err = db.WithContext(ctx).Clauses(dbresolver.Write).
		Model(&Segment{}).
		Select(progressSegmentColumns()).
		Where("job_name = ?", jobName).
		Order("range_start DESC").
		Limit(progressRecentSegmentLimit).
		Scan(&progress.RecentSegments).Error; err != nil {
		return progressBounds{}, errors.Tag(err)
	}
	var bounds progressBounds
	if len(progress.RecentSegments) > 0 {
		// 区间按 range_start 连续规划，最新区间终点就是当前最远规划边界。
		bounds.PlannedUntil = sql.NullTime{Time: progress.RecentSegments[0].RangeEnd, Valid: true}
	}
	if progress.EligibleUntil.Valid && !progress.WatermarkTime.Valid && counts.Total > 0 {
		var first ProgressSegment
		err = db.WithContext(ctx).Clauses(dbresolver.Write).
			Model(&Segment{}).
			Select("range_start").
			Where("job_name = ?", jobName).
			Order("range_start ASC").
			Take(&first).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return progressBounds{}, errors.Tag(err)
		}
		if !first.RangeStart.IsZero() {
			bounds.FirstRangeStart = sql.NullTime{Time: first.RangeStart, Valid: true}
		}
	}
	return bounds, nil
}

// buildProgressCountsQuery 构造按 job_name/status 覆盖索引分组的状态计数查询。
func buildProgressCountsQuery(ctx context.Context, db *gorm.DB, jobName string) *gorm.DB {
	return db.WithContext(ctx).Clauses(dbresolver.Write).
		Model(&Segment{}).
		Select([]string{
			"status",
			"COUNT(*) AS total",
		}).
		Where("job_name = ?", jobName).
		Group("status")
}

// loadProgressCounts 使用 job_name/status 覆盖索引汇总状态，避免轮询时读取区间大字段。
func loadProgressCounts(ctx context.Context, db *gorm.DB, jobName string) (ProgressCounts, error) {
	var rows []progressCountRow
	if err := buildProgressCountsQuery(ctx, db, jobName).Scan(&rows).Error; err != nil {
		return ProgressCounts{}, errors.Tag(err)
	}
	var counts ProgressCounts
	for _, row := range rows {
		counts.Total += row.Total
		switch row.Status {
		case statusPending:
			counts.Pending = row.Total
		case statusRunning:
			counts.Running = row.Total
		case statusDone:
			counts.Done = row.Total
		case statusDeleting:
			counts.Deleting = row.Total
		case statusDeleted:
			counts.Deleted = row.Total
		case statusFailed:
			counts.Failed = row.Total
		}
	}
	return counts, nil
}

// loadCurrentProgressSegment 优先返回租约最新的复制或删除区间，租约全部过期时保留最近 checkpoint 供排障。
func loadCurrentProgressSegment(ctx context.Context, db *gorm.DB, jobName string) (*ProgressSegment, error) {
	var item ProgressSegment
	err := db.WithContext(ctx).Clauses(dbresolver.Write).
		Model(&Segment{}).
		Select(progressSegmentColumns()).
		Where("job_name = ? AND status IN ?", jobName, []string{statusRunning, statusDeleting}).
		Order("lease_expires_at DESC").
		Order("range_start ASC").
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	item.EstimatedProgressPercent = estimateArchiveSegmentProgress(item)
	return &item, nil
}

// progressSegmentColumns 返回进度详情允许读取的固定控制表列，不读取失败原文等内部诊断字段。
func progressSegmentColumns() []string {
	return []string{
		"id", "history_table_name", "range_start", "range_end", "status", "worker_id",
		"lease_expires_at", "last_archived_id", "last_archived_time", "rows_archived",
		"attempt_count", "updated_at", "completed_at",
	}
}

// estimateArchiveSegmentProgress 按复制阶段的时间 checkpoint 估算当前区间进度，终态前最多显示 99.9%。
func estimateArchiveSegmentProgress(item ProgressSegment) *float64 {
	if item.Status != statusRunning || !item.RangeEnd.After(item.RangeStart) {
		return nil
	}
	progressRate := 0.0
	if item.LastArchivedTime.Valid {
		progressRate = item.LastArchivedTime.Time.Sub(item.RangeStart).Seconds() /
			item.RangeEnd.Sub(item.RangeStart).Seconds() * 100
	}
	progressRate = math.Max(0, math.Min(99.9, progressRate))
	progressRate = math.Round(progressRate*10) / 10
	return &progressRate
}

// archiveProgressLag 计算当前归档基线距离可归档上界的滞后秒数；没有可靠基线时返回空值。
func archiveProgressLag(eligibleUntil, watermarkTime, firstRangeStart sql.NullTime) sql.NullInt64 {
	if !eligibleUntil.Valid {
		return sql.NullInt64{}
	}
	baseline := watermarkTime
	if !baseline.Valid {
		baseline = firstRangeStart
	}
	if !baseline.Valid {
		return sql.NullInt64{}
	}
	seconds := int64(eligibleUntil.Time.Sub(baseline.Time).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return sql.NullInt64{Int64: seconds, Valid: true}
}

// archiveProgressPhase 根据运行态、当前执行项和区间积压推导展示阶段。
func archiveProgressPhase(progress Progress) string {
	if !progress.RuntimeEnabled {
		return ProgressPhaseInactive
	}
	if progress.CurrentSegment != nil {
		if archiveSegmentLeaseExpired(progress.CurrentSegment, progress.FetchedAt) {
			return ProgressPhaseLeaseExpired
		}
		switch progress.CurrentSegment.Status {
		case statusRunning:
			return ProgressPhaseRunning
		case statusDeleting:
			return ProgressPhaseDeleting
		}
	}
	if progress.Counts.Failed > 0 {
		return ProgressPhaseFailed
	}
	if progress.Counts.Pending > 0 {
		return ProgressPhasePending
	}
	caughtUp := progress.EligibleUntil.Valid && progress.WatermarkTime.Valid &&
		!progress.WatermarkTime.Time.Before(progress.EligibleUntil.Time)
	if progress.Counts.Done > 0 && caughtUp && !progress.deleteDisabled {
		return ProgressPhaseWaitingDelete
	}
	if caughtUp {
		return ProgressPhaseCaughtUp
	}
	if progress.WatermarkTime.Valid || progress.Counts.Total > 0 {
		return ProgressPhaseIdle
	}
	return ProgressPhaseNotStarted
}

// archiveSegmentLeaseExpired 判断执行态区间是否已经失去有效 worker 租约。
func archiveSegmentLeaseExpired(segment *ProgressSegment, fetchedAt time.Time) bool {
	if segment == nil || (segment.Status != statusRunning && segment.Status != statusDeleting) {
		return false
	}
	return !segment.LeaseExpiresAt.Valid || !segment.LeaseExpiresAt.Time.After(fetchedAt)
}
