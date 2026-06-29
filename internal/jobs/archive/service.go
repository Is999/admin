package archive

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	redislock "admin/internal/infra/redsync"
	adminlog "admin/internal/jobs/archive/adminlog"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/task/stats"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

const (
	// TaskTypeExecute 是归档工作流真正执行归档批次的任务类型。
	TaskTypeExecute = "archive:execute"

	// WorkflowNameRun 是通用归档工作流名称。
	WorkflowNameRun = "archive.run"

	// JobNameAdminLog 是 admin_log 的归档任务名。
	JobNameAdminLog = "admin_log"
)

const (
	// SplitUnitNone 表示历史数据写入固定历史表，不按时间拆物理表。
	SplitUnitNone = "none"
	// SplitUnitYear 表示按年拆表。
	SplitUnitYear = "year"
	// SplitUnitQuarter 表示按季度拆表。
	SplitUnitQuarter = "quarter"
	// SplitUnitMonth 表示按月拆表。
	SplitUnitMonth = "month"
	// SplitUnitWeek 表示按周拆表。
	SplitUnitWeek = "week"
	// SplitUnitDay 表示按天拆表。
	SplitUnitDay = "day"
	// SplitUnitCustomDays 表示按自定义天数拆表。
	SplitUnitCustomDays = "custom_days"
)

const (
	// TimeColumnTypeTime 表示归档时间列由数据库驱动解析为 time.Time。
	TimeColumnTypeTime = "time"
	// TimeColumnTypeString 表示归档时间列是字符串，格式由 time_column_format 控制。
	TimeColumnTypeString = "string"
	// TimeColumnTypeUnix 表示归档时间列是 Unix int64，单位由 time_column_unix_unit 控制。
	TimeColumnTypeUnix = "unix"
)

const (
	// TimeColumnUnixUnitSeconds 表示 Unix int64 按秒解析。
	TimeColumnUnixUnitSeconds = "seconds"
	// TimeColumnUnixUnitMilliseconds 表示 Unix int64 按毫秒解析。
	TimeColumnUnixUnitMilliseconds = "milliseconds"
)

const (
	// ArchiveWindowModeAuto 表示自动追赶稀疏窗口，默认用于窗口化归档。
	ArchiveWindowModeAuto = "auto"
	// ArchiveWindowModeFixed 表示严格按 archive_max_windows_per_run 推进窗口。
	ArchiveWindowModeFixed = "fixed"
)

const (
	// statusPending 表示区间已规划但尚未被 worker 领取。
	statusPending = "pending"
	// statusRunning 表示区间已被某个 worker 领取并正在执行。
	statusRunning = "running"
	// statusDone 表示区间已完整归档到历史表，是否已删热表由删除节点继续推进。
	statusDone = "done"
	// statusDeleting 表示区间正在执行热表删除。
	statusDeleting = "deleting"
	// statusDeleted 表示区间已归档且热表数据已完成删除。
	statusDeleted = "deleted"
	// statusFailed 表示区间上次执行失败，允许后续重试。
	statusFailed = "failed"
)

const (
	// archiveRunModeAll 表示归档和删除都执行。
	archiveRunModeAll = "all"
	// archiveRunModeArchive 表示本轮只做归档搬迁。
	archiveRunModeArchive = "archive"
	// archiveRunModeDelete 表示本轮只做已归档热表删除。
	archiveRunModeDelete = "delete"
)

const (
	// traceArchive 表示归档任务处理量明细前缀。
	traceArchive = "archive"
	// traceArchiveTarget 表示归档目标数量明细。
	traceArchiveTarget = "target"
	// traceArchiveHistory 表示历史表写入明细。
	traceArchiveHistory = "history"
)

const (
	// defaultSafeDelayMinutes 是默认安全查询延迟。
	defaultSafeDelayMinutes = 10
	// defaultLockTTL 是默认归档规划锁 TTL。
	defaultLockTTL = 30 * time.Second
	// defaultLeaseTTL 是默认区间租约 TTL。
	defaultLeaseTTL = 2 * time.Minute
	// defaultBatchSize 是默认单批归档条数。
	defaultBatchSize = 5000
	// defaultDeleteBatchSize 是默认单批删除条数。
	defaultDeleteBatchSize = 5000
	// defaultMaxHistoryTables 是默认最大历史表数。
	defaultMaxHistoryTables = 6
	// maxArchiveBatchSize 表示单批归档和删除的安全上限，避免误配置生成过大的 IN 条件和事务。
	maxArchiveBatchSize = 20000
	// defaultBatchDelay 是单批归档提交后的默认保护性等待时间，避免千万级历史积压时连续搬迁和删除冲击 MySQL。
	defaultBatchDelay = 100 * time.Millisecond
	// maxBatchDelay 是归档批次保护性等待的硬上限，避免误配置让任务长时间空等。
	maxBatchDelay = 5 * time.Second
	// watermarkScanBatchSize 表示水位线推进时单次读取区间数，避免一次性加载全部历史区间。
	watermarkScanBatchSize = 500
	// defaultMaxConcurrentJobs 表示单次归档工作流默认并发 job 数，保守并发能提升多表归档吞吐且不突然打满数据库。
	defaultMaxConcurrentJobs = 2
	// maxConcurrentArchiveJobs 表示单次归档工作流允许的最大 job 并发，防止误配置把同一套数据库 I/O 打满。
	maxConcurrentArchiveJobs = 8

	// defaultArchiveStringTimeLayout 表示字符串时间列未配置格式时的默认 Go layout。
	defaultArchiveStringTimeLayout = time.DateTime

	// historyCleanupTableBatchSize 表示单次保留策略最多淘汰的历史表数量，避免一次性 DROP 大量历史表产生 I/O 抖动。
	historyCleanupTableBatchSize = 2
	// maxArchiveWindowSeconds 表示单个归档/删除窗口最大秒数，避免误配置生成过宽窗口。
	maxArchiveWindowSeconds = 7 * 24 * 3600
	// maxArchiveWindowsPerRun 表示单轮最多规划或删除窗口数，避免周期任务一次追太多历史区间。
	maxArchiveWindowsPerRun = 1000
	// defaultArchiveAutoMaxWindows 表示自动模式单轮最多追赶窗口数，避免空窗口按小时慢慢追平。
	defaultArchiveAutoMaxWindows = 200
	// defaultArchiveAutoLightRows 表示自动模式默认轻量窗口行数阈值。
	defaultArchiveAutoLightRows = 20000
	// defaultArchiveAutoLightElapsed 表示自动模式轻量窗口默认耗时阈值。
	defaultArchiveAutoLightElapsed = 3 * time.Second
	// maxArchiveAutoLightElapsed 表示自动模式轻量窗口耗时阈值硬上限。
	maxArchiveAutoLightElapsed = 30 * time.Second
)

const (
	// tableNameWatermark 是归档水位线控制表名。
	tableNameWatermark = "archive_watermark"
	// tableNameSegment 是归档区间/断点控制表名。
	tableNameSegment = "archive_segment"
)

// archiveWatermarkSchemaTemplate 保存归档水位控制表 DDL 模板。
// DDL 只替换受控表名，避免大段原生 SQL 留在 Go 文件中触发 IDE 注入误报。
//
//go:embed assets/archive_watermark_schema.sql.tmpl
var archiveWatermarkSchemaTemplate string

// archiveSegmentSchemaTemplate 保存归档区间 checkpoint 控制表 DDL 模板。
// 该表负责记录历史表区间、租约和断点游标，模板化后仍由 EnsureSchema 统一执行。
//
//go:embed assets/archive_segment_schema.sql.tmpl
var archiveSegmentSchemaTemplate string

// archiveBatchInsertTemplate 保存归档批次复制到历史表的 INSERT IGNORE SELECT 模板。
// 批次主键集合和时间窗口仍通过 Exec 参数绑定，自定义归档条件只来自已校验配置。
//
//go:embed assets/archive_batch_insert.sql.tmpl
var archiveBatchInsertTemplate string

// archiveDropHistoryTableTemplate 保存历史表淘汰使用的动态 DDL 模板。
// DROP TABLE 只能使用原生 DDL；表名来自历史区间元数据并在渲染前统一反引号保护。
//
//go:embed assets/archive_drop_history_table.sql.tmpl
var archiveDropHistoryTableTemplate string

// archiveCreateHistoryTableTemplate 保存历史表按热表结构自愈创建的动态 DDL 模板。
// CREATE TABLE LIKE 用于复用线上热表结构和索引，避免手工维护历史表 DDL 漂移。
//
//go:embed assets/archive_create_history_table.sql.tmpl
var archiveCreateHistoryTableTemplate string

// Service 封装通用归档控制面、批处理执行链路和查询侧元数据读取能力。
type Service struct {
	svcCtx          *svc.ServiceContext // 服务上下文，提供数据库、Redis 与运行期配置
	controlDatabase svc.DBName          // 归档控制表归属库名
}

// AdminLogQueryMeta 是管理员日志查询返回给接口层的元信息。
type AdminLogQueryMeta = adminlog.Meta

// Option 定义归档服务运行期可插拔配置项。
type Option func(s *Service)

// WithControlDatabase 指定 archive_segment/archive_watermark 控制表归属库。
// 归档热表和历史表库由 archive_jobs[].database 显式指定，避免按表名隐式路由带来的歧义。
func WithControlDatabase(database svc.DBName) Option {
	return func(s *Service) {
		s.controlDatabase = database
	}
}

// Watermark 描述单个归档任务当前的归档复制水位。
// WatermarkTime 是已复制到历史表的排他上界，热表删除另由 delete_delay_days 控制。
type Watermark struct {
	ID              uint64       `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true" json:"id"`                                     // 主键 ID
	JobName         string       `gorm:"column:job_name;type:varchar(128);not null;uniqueIndex:uk_job_name;comment:归档任务名" json:"job_name"`           // 归档任务名
	SourceTableName string       `gorm:"column:table_name;type:varchar(128);not null;index:idx_table_name,priority:1;comment:热表名" json:"table_name"` // 热表名
	WatermarkTime   sql.NullTime `gorm:"column:watermark_time;type:datetime(6);comment:已完整复制到历史表的排他上界" json:"watermark_time"`                        // 已完整复制到历史表的排他上界
	UpdatedAt       time.Time    `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);comment:更新时间" json:"updated_at"`    // 更新时间
}

// TableName 返回归档水位线控制表名。
func (*Watermark) TableName() string {
	return tableNameWatermark
}

// Segment 描述一个已规划的归档时间区间，同时承担 checkpoint 作用。
type Segment struct {
	ID               uint64       `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true" json:"id"`                                                                             // 主键 ID
	JobName          string       `gorm:"column:job_name;type:varchar(128);not null;uniqueIndex:uk_job_range,priority:1;index:idx_job_status_lease,priority:1;comment:归档任务名" json:"job_name"` // 归档任务名
	SourceTableName  string       `gorm:"column:table_name;type:varchar(128);not null;comment:热表名" json:"table_name"`                                                                         // 热表名
	HistoryTableName string       `gorm:"column:history_table_name;type:varchar(128);not null;index:idx_history_table,priority:1;comment:历史表名" json:"history_table_name"`                     // 历史表名
	RangeStart       time.Time    `gorm:"column:range_start;type:datetime(6);not null;uniqueIndex:uk_job_range,priority:2;index:idx_job_range,priority:2;comment:区间起点含边界" json:"range_start"` // 区间起点（含）
	RangeEnd         time.Time    `gorm:"column:range_end;type:datetime(6);not null;uniqueIndex:uk_job_range,priority:3;index:idx_job_range,priority:3;comment:区间终点排他边界" json:"range_end"`    // 区间终点（排他）
	Status           string       `gorm:"column:status;type:varchar(16);not null;index:idx_job_status_lease,priority:2;comment:区间状态，done表示已归档，deleted表示热表已删" json:"status"`                   // 区间状态
	WorkerID         string       `gorm:"column:worker_id;type:varchar(128);not null;default:'';comment:当前持有 worker" json:"worker_id"`                                                        // 当前持有 worker
	LeaseExpiresAt   sql.NullTime `gorm:"column:lease_expires_at;type:datetime(6);index:idx_job_status_lease,priority:3;comment:租约过期时间" json:"lease_expires_at"`                              // 租约过期时间
	LastArchivedID   int64        `gorm:"column:last_archived_id;type:bigint;not null;default:0;comment:最近归档主键游标" json:"last_archived_id"`                                                    // 最近归档主键游标
	LastArchivedTime sql.NullTime `gorm:"column:last_archived_time;type:datetime(6);comment:最近归档时间游标" json:"last_archived_time"`                                                              // 最近归档时间游标
	RowsArchived     int64        `gorm:"column:rows_archived;type:bigint;not null;default:0;comment:累计归档行数" json:"rows_archived"`                                                            // 累计归档行数
	AttemptCount     int          `gorm:"column:attempt_count;type:int unsigned;not null;default:0;comment:领取次数" json:"attempt_count"`                                                        // 领取次数
	ErrorMessage     string       `gorm:"column:error_message;type:varchar(500);not null;default:'';comment:失败摘要" json:"error_message"`                                                       // 失败摘要
	CreatedAt        time.Time    `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);comment:创建时间" json:"created_at"`                                            // 创建时间
	UpdatedAt        time.Time    `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);comment:更新时间" json:"updated_at"`                                            // 更新时间
	CompletedAt      sql.NullTime `gorm:"column:completed_at;type:datetime(6);comment:完成时间" json:"completed_at"`                                                                              // 完成时间
}

// TableName 返回归档区间控制表名。
func (*Segment) TableName() string {
	return tableNameSegment
}

// batchCursorRow 描述单批次扫描到的游标结果。
// 这里只保留主键和时间列，便于后续断点推进与删除校验。
type batchCursorRow struct {
	ID        int64     `gorm:"column:id"`         // 当前批次主键
	CreatedAt time.Time `gorm:"column:created_at"` // 当前批次时间列
}

// archiveSegmentResult 描述单个归档区间本轮执行结果，用于 auto 模式判断是否继续追水位。
type archiveSegmentResult struct {
	RowsArchived int64         // 本轮归档推进的游标行数
	Elapsed      time.Duration // 本轮处理该区间的耗时
}

// deleteSegmentResult 描述单个删除区间本轮执行结果，用于 auto 模式追赶空窗口和稀疏窗口。
type deleteSegmentResult struct {
	RowsDeleted int64         // 本轮删除的热表行数
	Elapsed     time.Duration // 本轮处理该区间的耗时
	Progressed  bool          // 本轮是否删除或完成了一个区间
}

// historyTableItem 描述历史表及其最早覆盖时间，用于 TTL 清理排序。
type historyTableItem struct {
	HistoryTableName string    `gorm:"column:history_table_name"` // 历史表名
	FirstRangeStart  time.Time `gorm:"column:first_range_start"`  // 历史表最早区间起点
}

// NewService 创建归档服务对象。
func NewService(svcCtx *svc.ServiceContext, opts ...Option) *Service {
	s := &Service{
		svcCtx:          svcCtx,
		controlDatabase: svc.DatabaseMain,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// QueryAdminLogs 查询管理员审计日志。
func (s *Service) QueryAdminLogs(ctx context.Context, req *types.AdminLogQueryReq) ([]model.AdminLog, int64, AdminLogQueryMeta, error) {
	if req == nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Errorf("管理员日志查询参数不能为空")
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Tag(err)
	}
	job := s.adminLogQueryJob()
	return adminlog.QueryDirect(ctx, s.adminLogDB(job), req, startTime, endTime, job.QueryWriteDB)
}

// adminLogQueryJob 返回管理员日志热表查询配置。
func (s *Service) adminLogQueryJob() jobConfig {
	if job, ok := s.jobByName(JobNameAdminLog); ok {
		return job
	}
	return jobConfig{
		Name:      JobNameAdminLog,
		Database:  svc.DatabaseMain,
		TableName: model.TableNameAdminLog,
	}
}

// adminLogDB 返回管理员日志查询应使用的连接。
func (s *Service) adminLogDB(job jobConfig) *gorm.DB {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	if job.QueryWriteDB {
		return withWriteResolver(s.jobSourceWriteDB(job))
	}
	if db := s.svcCtx.ReadDB(job.Database); db != nil {
		return db
	}
	return withWriteResolver(s.jobSourceWriteDB(job))
}

// archiveTraceName 拼接归档任务处理量明细名称。
func archiveTraceName(parts ...string) string {
	values := make([]string, 0, len(parts)+1)
	values = append(values, traceArchive)
	values = append(values, parts...)
	return taskstats.JoinDetailName(values...)
}

// EnsureSchema 确保归档控制表存在。
func (s *Service) EnsureSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.Errorf("归档数据库连接为空")
	}
	watermarkSchemaSQL := renderArchiveSQLTemplate(
		archiveWatermarkSchemaTemplate,
		"{{table_name}}", quoteIdent(tableNameWatermark),
	)
	if err := db.WithContext(ctx).Exec(watermarkSchemaSQL).Error; err != nil {
		return errors.Tag(err)
	}
	segmentSchemaSQL := renderArchiveSQLTemplate(
		archiveSegmentSchemaTemplate,
		"{{table_name}}", quoteIdent(tableNameSegment),
	)
	if err := db.WithContext(ctx).Exec(segmentSchemaSQL).Error; err != nil {
		return errors.Tag(err)
	}
	return nil
}

// RunTargets 执行指定归档目标；targets 为空时默认执行全部启用任务。
func (s *Service) RunTargets(ctx context.Context, targets []string, workerID string) error {
	if s == nil || s.svcCtx == nil || !s.svcCtx.CurrentConfig().Archive.Enabled {
		return nil
	}
	// 先把目标名解析成标准化任务配置和执行动作，避免后续执行期反复读取原始配置。
	runs := s.resolveTargets(targets)
	if len(runs) == 0 {
		taskstats.RecordSkip(ctx, archiveTraceName(traceArchiveTarget, taskstats.DetailPartSkipped), 1)
		return nil
	}
	if len(runs) > 0 {
		taskstats.RecordRead(ctx, archiveTraceName(traceArchiveTarget), int64(len(runs)))
	}
	concurrency := s.maxConcurrentJobs()
	if concurrency <= 1 || len(runs) == 1 {
		return s.runTargetsSequentially(ctx, runs, workerID)
	}
	return s.runTargetsConcurrently(ctx, runs, workerID, concurrency)
}

// runTargetsSequentially 按原有串行语义执行归档 job，用于单 job 或显式关闭并发的保守场景。
func (s *Service) runTargetsSequentially(ctx context.Context, runs []jobRunConfig, workerID string) error {
	var runErrs []error
	for _, run := range runs {
		if err := s.runJob(ctx, run, workerID); err != nil {
			runErrs = append(runErrs, errors.Wrapf(err, "归档任务执行失败: job=%s mode=%s worker_id=%s", run.Job.Name, run.Mode, workerID))
		}
	}
	if len(runErrs) > 0 {
		return errors.Join(runErrs...)
	}
	return nil
}

// runTargetsConcurrently 对多个互相独立的归档 job 做有界并发，令牌数来自配置并被硬上限保护。
// 每个 job 内部仍沿用 segment 领取、租约和 checkpoint 机制，失败重试不会从头扫热表。
func (s *Service) runTargetsConcurrently(ctx context.Context, runs []jobRunConfig, workerID string, concurrency int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, len(runs))
	var wg sync.WaitGroup
launchLoop:
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			errCh <- errors.Wrapf(err, "归档任务启动前上下文已取消: worker_id=%s", workerID)
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			errCh <- errors.Wrapf(ctx.Err(), "归档任务等待并发令牌时上下文已取消: worker_id=%s", workerID)
			break launchLoop
		}
		wg.Add(1)
		run := run
		go func() {
			defer wg.Done()
			defer func() {
				<-sem
			}()
			if err := s.runJob(ctx, run, workerID); err != nil {
				errCh <- errors.Wrapf(err, "归档任务执行失败: job=%s mode=%s worker_id=%s", run.Job.Name, run.Mode, workerID)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	runErrs := make([]error, 0, len(errCh))
	for err := range errCh {
		if err != nil {
			runErrs = append(runErrs, err)
		}
	}
	if len(runErrs) > 0 {
		return errors.Join(runErrs...)
	}
	return nil
}

// runJob 执行单个归档任务的完整主流程。
// 执行顺序必须固定为：规划/搬迁归档窗口 -> 推进归档 watermark -> 按删除窗口清理热表 -> 清理过期历史表。
func (s *Service) runJob(ctx context.Context, run jobRunConfig, workerID string) error {
	job := run.Job
	sourceDB := s.jobSourceWriteDB(job) // sourceDB 表示归档热表和历史表所在库的主库连接。
	controlDB := s.jobControlWriteDB()  // controlDB 表示 archive_segment/archive_watermark 所在库的主库连接。
	if sourceDB == nil {
		return errors.Errorf("归档源库连接为空: job=%s database=%s", job.Name, job.Database)
	}
	if controlDB == nil {
		return errors.Errorf("归档控制库连接为空: job=%s control_database=%s", job.Name, s.controlDatabaseName())
	}
	if err := s.EnsureSchema(ctx, controlDB); err != nil {
		return errors.Tag(err)
	}
	if err := s.ensureArchiveAccessPath(ctx, sourceDB, job); err != nil {
		return errors.Tag(err)
	}
	if run.Mode == archiveRunModeAll || run.Mode == archiveRunModeArchive {
		if err := s.prepareSegments(ctx, job, sourceDB, controlDB); err != nil {
			return errors.Tag(err)
		}
		processedArchive := 0
		var lastResult *archiveSegmentResult
		for {
			if !shouldContinueArchiveRun(job, processedArchive, lastResult) {
				break
			}
			// 每次只领取一个区间，处理完成后再继续抢占，避免单 worker 长时间垄断大量任务。
			segment, err := s.claimNextSegment(ctx, job, controlDB, workerID)
			if err != nil {
				return errors.Tag(err)
			}
			if segment == nil {
				break
			}
			result, err := s.processSegment(ctx, job, sourceDB, controlDB, segment, workerID)
			if err != nil {
				_ = s.markSegmentFailed(context.Background(), controlDB, segment.ID, workerID, err)
				return errors.Tag(err)
			}
			lastResult = &result
			processedArchive++
		}
		if err := s.advanceWatermark(ctx, job, controlDB); err != nil {
			return errors.Tag(err)
		}
	}
	if !job.DeleteDisabled && (run.Mode == archiveRunModeAll || run.Mode == archiveRunModeDelete) {
		if err := s.deleteArchivedSegments(ctx, job, sourceDB, controlDB, workerID); err != nil {
			return errors.Tag(err)
		}
		return s.cleanupHistoryTables(ctx, job, sourceDB, controlDB)
	}
	return nil
}

// prepareSegments 根据当前 watermark 与安全归档上界预先切分待归档区间。
// 这里通过短时分布式锁保证多个 worker 不会重复规划同一批区间。
func (s *Service) prepareSegments(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB) error {
	lockKey, err := s.archiveJobPlanKey(job.Name)
	if err != nil {
		return errors.Tag(err)
	}
	return redislock.WithLock(ctx, s.redisClient(), lockKey, s.lockTTL(), func(lockCtx context.Context) error {
		upperBound, ok := s.archiveUpperBound(lockCtx, job, controlDB)
		if !ok {
			return nil
		}
		watermark, err := s.loadWatermark(lockCtx, controlDB, job.Name)
		if err != nil {
			return errors.Tag(err)
		}
		var cursor time.Time
		if watermark != nil && watermark.WatermarkTime.Valid {
			// 已存在 watermark 时，从已完整归档的排他上界继续向后规划。
			cursor = watermark.WatermarkTime.Time
		} else {
			// 首次运行没有水位线时，需要先定位热表中最早可归档数据。
			minTime, hasData, innerErr := s.minArchivableTime(lockCtx, job, sourceDB, upperBound)
			if innerErr != nil {
				return errors.Wrapf(innerErr, "定位归档起始时间失败 job=%s table=%s", job.Name, job.TableName)
			}
			if !hasData {
				return nil
			}
			cursor = alignInitialArchiveCursor(minTime, job)
			if job.StartAt.Valid {
				// start_at 用于限制首次规划的最早归档边界，避免新接入任务规划规则外区间。
				startAt := alignInitialArchiveCursor(job.StartAt.Time, job)
				if cursor.Before(startAt) {
					cursor = startAt
				}
			}
		}
		plannedWindows := 0
		planLimit := archivePlanWindowLimit(job)
		for cursor.Before(upperBound) {
			nextBoundary := nextArchiveSegmentBoundary(cursor, job)
			if nextBoundary.After(upperBound) {
				nextBoundary = upperBound
			}
			if !nextBoundary.After(cursor) {
				return errors.Errorf("归档区间计算异常: start=%s end=%s", cursor.Format(time.DateTime), nextBoundary.Format(time.DateTime))
			}
			// 区间记录依赖唯一键防重，因此即使重复执行规划动作也不会产生脏数据。
			segment := Segment{
				JobName:          job.Name,
				SourceTableName:  job.TableName,
				HistoryTableName: buildHistoryTableName(job, cursor, nextBoundary),
				RangeStart:       cursor,
				RangeEnd:         nextBoundary,
				Status:           statusPending,
			}
			if !identifierPattern.MatchString(segment.HistoryTableName) {
				return errors.Errorf("归档历史表名不合法: job=%s table=%s", job.Name, segment.HistoryTableName)
			}
			if err := controlDB.WithContext(lockCtx).Clauses(clause.OnConflict{DoNothing: true}).Create(&segment).Error; err != nil {
				return errors.Tag(err)
			}
			cursor = nextBoundary
			plannedWindows++
			if planLimit > 0 && plannedWindows >= planLimit {
				return nil
			}
		}
		return nil
	})
}

// claimNextSegment 领取下一个可执行区间，并写入 worker 与租约信息。
// 这里通过事务和行级锁保证同一时刻只有一个 worker 能成功拿到某个区间。
func (s *Service) claimNextSegment(ctx context.Context, job jobConfig, controlDB *gorm.DB, workerID string) (*Segment, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(s.leaseTTL())
	upperBound, ok := s.archiveUpperBound(ctx, job, controlDB)
	if !ok {
		return nil, nil
	}
	var claimed Segment
	err := controlDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("job_name = ?", job.Name).
			Where("range_start < ?", upperBound).
			Where("(status = ? OR status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?))", statusPending, statusFailed, statusRunning, now).
			Order("range_start ASC").
			Take(&claimed).Error
		if err != nil {
			return errors.Tag(err)
		}
		claimed.Status = statusRunning
		claimed.WorkerID = workerID
		claimed.LeaseExpiresAt = sql.NullTime{Time: leaseExpiresAt, Valid: true}
		claimed.AttemptCount++
		claimed.ErrorMessage = ""
		claimed.UpdatedAt = now
		return tx.Model(&Segment{}).
			Where("id = ?", claimed.ID).
			Updates(map[string]any{
				"status":           claimed.Status,
				"worker_id":        claimed.WorkerID,
				"lease_expires_at": claimed.LeaseExpiresAt,
				"attempt_count":    claimed.AttemptCount,
				"error_message":    claimed.ErrorMessage,
				"updated_at":       claimed.UpdatedAt,
			}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &claimed, nil
}

// processSegment 循环处理单个区间下的所有批次，直到区间内数据已完整复制到历史表。
func (s *Service) processSegment(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB, segment *Segment, workerID string) (result archiveSegmentResult, err error) {
	startedAt := time.Now()
	defer func() {
		result.Elapsed = time.Since(startedAt)
	}()
	if segment == nil {
		return result, nil
	}
	if err := s.ensureHistoryTable(ctx, sourceDB, job, segment.HistoryTableName); err != nil {
		return result, errors.Tag(err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, errors.Tag(err)
		}
		rows, err := s.loadBatchCursorRows(ctx, sourceDB, job, segment)
		if err != nil {
			return result, errors.Tag(err)
		}
		if len(rows) > 0 {
			taskstats.RecordRead(ctx, archiveTraceName(job.Name, archiveRunModeArchive, taskstats.DetailPartRows), int64(len(rows)))
		}
		if len(rows) == 0 {
			// 只有确认区间内已经没有待复制热数据，才能把该区间标记为 done；热表删除由独立删除窗口继续推进。
			return result, s.markSegmentDone(ctx, controlDB, segment.ID, workerID)
		}
		if err = s.archiveBatch(ctx, sourceDB, controlDB, job, segment, rows, workerID); err != nil {
			return result, errors.Tag(err)
		}
		taskstats.RecordInsert(ctx, archiveTraceName(job.Name, traceArchiveHistory, taskstats.DetailPartRows), int64(len(rows)))
		result.RowsArchived += int64(len(rows))
		batchSize := job.BatchSize
		if batchSize <= 0 {
			batchSize = defaultBatchSize
		}
		if len(rows) >= batchSize {
			// 批次事务已经提交后再短暂等待，避免在大积压场景下连续 INSERT...SELECT 抢占业务库 I/O。
			if err = waitArchiveBatch(ctx, s.batchDelay()); err != nil {
				return result, errors.Tag(err)
			}
		}
	}
}

// archiveBatch 执行一批游标数据的“写历史表 -> 校验 -> 推进 checkpoint”。
// 归档和热表删除解耦，确保可以先归档两天前窗口，再按 hot_keep_days 或 delete_delay_days 延后删除。
func (s *Service) archiveBatch(ctx context.Context, sourceDB *gorm.DB, controlDB *gorm.DB, job jobConfig, segment *Segment, rows []batchCursorRow, workerID string) error {
	if len(rows) == 0 {
		return nil
	}
	columnList, err := archiveTableColumnList(ctx, sourceDB, job.TableName)
	if err != nil {
		return errors.Tag(err)
	}
	chunkSize := job.BatchSize
	if chunkSize <= 0 {
		chunkSize = len(rows)
	}
	for start := 0; start < len(rows); start += chunkSize {
		end := start + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		ids := make([]int64, 0, len(chunk))
		for _, row := range chunk {
			ids = append(ids, row.ID)
		}
		lastRow := chunk[len(chunk)-1]
		if err := s.archiveBatchChunk(ctx, sourceDB, job, segment, ids, columnList); err != nil {
			return errors.Tag(err)
		}
		if err := s.updateSegmentCheckpoint(ctx, controlDB, segment, len(ids), lastRow.CreatedAt, lastRow.ID, workerID); err != nil {
			return errors.Tag(err)
		}
		segment.LastArchivedID = lastRow.ID
		segment.LastArchivedTime = sql.NullTime{Time: lastRow.CreatedAt, Valid: true}
		segment.RowsArchived += int64(len(ids))
		if end < len(rows) {
			// 子批次已独立提交并推进 checkpoint 后再限速，避免小事务连续写入历史表压垮 MySQL。
			if err := waitArchiveBatch(ctx, s.batchDelay()); err != nil {
				return errors.Tag(err)
			}
		}
	}
	return nil
}

// archiveBatchChunk 在源库事务内完成单个子批次的复制和校验。
// 该事务只覆盖当前批次主键集合，降低千万级历史归档时的行锁持有范围。
func (s *Service) archiveBatchChunk(ctx context.Context, sourceDB *gorm.DB, job jobConfig, segment *Segment, ids []int64, columnList string) error {
	if len(ids) == 0 {
		return nil
	}
	if segment == nil {
		return errors.Errorf("归档区间为空: job=%s table=%s", job.Name, job.TableName)
	}
	return sourceDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		// 历史表写入使用 INSERT IGNORE，依赖主键天然防重，支持任务重试和补跑。
		// 时间窗口和自定义归档条件在复制事务内再次校验，避免游标读取后数据状态变化导致越界行进入历史表。
		insertSQL := archiveBatchInsertSQL(job, segment, columnList)
		insertArgs := append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)
		if err := tx.Exec(insertSQL, insertArgs...).Error; err != nil {
			return errors.Tag(err)
		}

		var copiedCount int64
		if err := tx.Table(segment.HistoryTableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)...).
			Count(&copiedCount).Error; err != nil {
			return errors.Tag(err)
		}
		if copiedCount != int64(len(ids)) {
			return errors.Errorf("历史表校验失败: table=%s want=%d got=%d", segment.HistoryTableName, len(ids), copiedCount)
		}
		if job.isStringTimeColumn() {
			if err := s.ensureStringTimeHistoryMatchesHot(ctx, tx, job, segment.HistoryTableName, ids); err != nil {
				return errors.Tag(err)
			}
		}

		return nil
	})
}

// deleteArchivedSegments 按删除窗口领取已归档区间，并分批删除热表数据。
// 删除只处理 status=done 的区间，且每批删除前会校验对应主键已存在于历史表，避免归档缺口导致数据丢失。
func (s *Service) deleteArchivedSegments(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB, workerID string) error {
	processed := 0
	var lastResult *deleteSegmentResult
	for {
		if !shouldContinueDeleteRun(job, processed, lastResult) {
			return nil
		}
		segment, err := s.claimNextDeleteSegment(ctx, job, controlDB, workerID)
		if err != nil {
			return errors.Tag(err)
		}
		if segment == nil {
			return nil
		}
		result, err := s.processDeleteSegment(ctx, job, sourceDB, controlDB, segment, workerID)
		if err != nil {
			_ = s.markSegmentDeleteFailed(context.Background(), controlDB, segment.ID, workerID, err)
			return errors.Tag(err)
		}
		if !result.Progressed {
			return nil
		}
		lastResult = &result
		processed++
	}
}

// claimNextDeleteSegment 领取下一个达到删除上界的已归档区间。
// 这里复用 archive_segment 的状态和租约，确保同一区间不会被多个删除 worker 同时处理。
func (s *Service) claimNextDeleteSegment(ctx context.Context, job jobConfig, controlDB *gorm.DB, workerID string) (*Segment, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(s.leaseTTL())
	upperBound, ok := s.deleteUpperBound(ctx, job)
	if !ok {
		return nil, nil
	}
	var claimed Segment
	err := controlDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("job_name = ?", job.Name).
			Where("range_start < ?", upperBound).
			Where("(status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?))", statusDone, statusDeleting, now).
			Order("range_start ASC").
			Take(&claimed).Error
		if err != nil {
			return errors.Tag(err)
		}
		claimed.Status = statusDeleting
		claimed.WorkerID = workerID
		claimed.LeaseExpiresAt = sql.NullTime{Time: leaseExpiresAt, Valid: true}
		claimed.AttemptCount++
		claimed.ErrorMessage = ""
		claimed.UpdatedAt = now
		return tx.Model(&Segment{}).
			Where("id = ?", claimed.ID).
			Updates(map[string]any{
				"status":           claimed.Status,
				"worker_id":        claimed.WorkerID,
				"lease_expires_at": claimed.LeaseExpiresAt,
				"attempt_count":    claimed.AttemptCount,
				"error_message":    claimed.ErrorMessage,
				"updated_at":       claimed.UpdatedAt,
			}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &claimed, nil
}

// processDeleteSegment 删除单个已归档区间内当前删除窗口的热表数据。
// Progressed 表示本轮是否确实删除或完成了一个区间，调用方据此避免在未到删除上界时重复抢占同一区间。
func (s *Service) processDeleteSegment(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB, segment *Segment, workerID string) (result deleteSegmentResult, err error) {
	startedAt := time.Now()
	defer func() {
		result.Elapsed = time.Since(startedAt)
	}()
	if segment == nil {
		return result, nil
	}
	if !tableExists(ctx, sourceDB, segment.HistoryTableName) {
		return result, errors.Errorf("归档历史表不存在，禁止删除热表数据: job=%s table=%s", job.Name, segment.HistoryTableName)
	}
	deleteBatchSize := job.DeleteBatchSize
	if deleteBatchSize <= 0 {
		deleteBatchSize = defaultDeleteBatchSize
	}
	upperBound, ok := s.deleteUpperBound(ctx, job)
	if !ok {
		if err := s.markSegmentDeletePartial(ctx, controlDB, segment.ID, workerID); err != nil {
			return result, errors.Tag(err)
		}
		return result, nil
	}
	deleteRangeEnd := segment.RangeEnd
	if upperBound.Before(deleteRangeEnd) {
		deleteRangeEnd = upperBound
	}
	if !deleteRangeEnd.After(segment.RangeStart) {
		if err := s.markSegmentDeletePartial(ctx, controlDB, segment.ID, workerID); err != nil {
			return result, errors.Tag(err)
		}
		return result, nil
	}
	if job.hasDeleteSecondWindow() {
		rows, err := s.loadDeleteCursorRows(ctx, sourceDB, job, segment, deleteRangeEnd)
		if err != nil {
			return result, errors.Tag(err)
		}
		if len(rows) == 0 {
			if deleteRangeEnd.Before(segment.RangeEnd) {
				if err := s.markSegmentDeletePartial(ctx, controlDB, segment.ID, workerID); err != nil {
					return result, errors.Tag(err)
				}
				return result, nil
			}
			if err := s.markSegmentDeleted(ctx, controlDB, segment.ID, workerID); err != nil {
				return result, errors.Tag(err)
			}
			result.Progressed = true
			return result, nil
		}
		// 删除窗口从当前区间最早剩余热数据所在窗口开始，确保按固定时间片逐步清理大表。
		windowStart := alignWindowEnd(rows[0].CreatedAt, job.DeleteWindowSeconds)
		windowEnd := windowStart.Add(time.Duration(job.DeleteWindowSeconds) * time.Second)
		if windowEnd.Before(deleteRangeEnd) {
			deleteRangeEnd = windowEnd
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, errors.Tag(err)
		}
		rows, err := s.loadDeleteCursorRows(ctx, sourceDB, job, segment, deleteRangeEnd)
		if err != nil {
			return result, errors.Tag(err)
		}
		if len(rows) > 0 {
			taskstats.RecordRead(ctx, archiveTraceName(job.Name, archiveRunModeDelete, taskstats.DetailPartRows), int64(len(rows)))
		}
		if len(rows) == 0 {
			if deleteRangeEnd.Before(segment.RangeEnd) {
				if err := s.markSegmentDeletePartial(ctx, controlDB, segment.ID, workerID); err != nil {
					return result, errors.Tag(err)
				}
				result.Progressed = result.RowsDeleted > 0
				return result, nil
			}
			if err := s.markSegmentDeleted(ctx, controlDB, segment.ID, workerID); err != nil {
				return result, errors.Tag(err)
			}
			result.Progressed = true
			return result, nil
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err = s.deleteBatchChunk(ctx, sourceDB, job, segment, deleteRangeEnd, ids); err != nil {
			return result, errors.Tag(err)
		}
		taskstats.RecordDelete(ctx, archiveTraceName(job.Name, archiveRunModeDelete, taskstats.DetailPartRows), int64(len(rows)))
		result.RowsDeleted += int64(len(rows))
		if len(rows) >= deleteBatchSize {
			if err = waitArchiveBatch(ctx, s.batchDelay()); err != nil {
				return result, errors.Tag(err)
			}
		}
	}
}

// loadDeleteCursorRows 读取当前区间下一批可删除热表主键。
// 删除游标不需要额外 checkpoint：已删除行会从热表消失，下一轮自然从当前区间剩余最小主键继续。
func (s *Service) loadDeleteCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time) ([]batchCursorRow, error) {
	batchSize := job.DeleteBatchSize
	if batchSize <= 0 {
		batchSize = defaultDeleteBatchSize
	}
	if job.isStringTimeColumn() {
		type dateStringCursorRow struct {
			ID        int64  `gorm:"column:id"`         // 当前批次主键
			CreatedAt string `gorm:"column:created_at"` // 当前批次字符串时间
		}
		rawRows := make([]dateStringCursorRow, 0, batchSize)
		if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rawRows).Error; err != nil {
			return nil, errors.Tag(err)
		}
		rows := make([]batchCursorRow, 0, len(rawRows))
		for _, raw := range rawRows {
			createdAt, err := parseArchiveStringTime(raw.CreatedAt, job.TimeColumnFormat)
			if err != nil {
				return nil, errors.Wrapf(err, "待删除归档字符串时间非法: table=%s column=%s id=%d value=%s", job.TableName, job.TimeColumn, raw.ID, raw.CreatedAt)
			}
			rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
		}
		return rows, nil
	}
	if job.isUnixSecondsTimeColumn() {
		type unixSecondsCursorRow struct {
			ID        int64 `gorm:"column:id"`         // 当前批次主键
			CreatedAt int64 `gorm:"column:created_at"` // 当前批次 Unix int64 时间
		}
		rawRows := make([]unixSecondsCursorRow, 0, batchSize)
		if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rawRows).Error; err != nil {
			return nil, errors.Tag(err)
		}
		rows := make([]batchCursorRow, 0, len(rawRows))
		for _, raw := range rawRows {
			createdAt, err := parseArchiveUnixTime(raw.CreatedAt, job.TimeColumnUnixUnit)
			if err != nil {
				return nil, errors.Wrapf(err, "待删除归档 Unix 时间非法: table=%s column=%s unit=%s id=%d value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, raw.ID, raw.CreatedAt)
			}
			rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
		}
		return rows, nil
	}
	rows := make([]batchCursorRow, 0, batchSize)
	if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// buildLoadDeleteCursorRowsQuery 构造热表删除游标读取查询。
// 查询只扫描可删除半开窗口，并叠加归档和清理条件。
func buildLoadDeleteCursorRowsQuery(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time, batchSize int) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("? AS id, ? AS created_at", clause.Column{Name: job.PrimaryKey}, clause.Column{Name: job.TimeColumn})
	query = applyArchiveTimeRangeWhere(query, job, segment.RangeStart, rangeEnd)
	if condition := deleteConditionSQL(job); condition != "" {
		// 删除条件包含归档条件和清理附加条件，确保游标阶段和事务内删除阶段使用同一业务边界。
		query = query.Where("(" + condition + ")")
	}
	return query.
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.TimeColumn}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.PrimaryKey}}).
		Limit(batchSize)
}

// deleteBatchChunk 在源库事务内校验历史表已存在对应主键后，按主键删除热表数据。
// 校验与删除同事务执行，避免 DBA 手工误删历史表或归档补偿未完成时仍继续清热表。
func (s *Service) deleteBatchChunk(ctx context.Context, sourceDB *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if segment == nil {
		return errors.Errorf("归档区间为空: job=%s table=%s", job.Name, job.TableName)
	}
	if !rangeEnd.After(segment.RangeStart) || rangeEnd.After(segment.RangeEnd) {
		return errors.Errorf("删除窗口边界异常: job=%s table=%s range_start=%s range_end=%s segment_end=%s",
			job.Name, job.TableName, segment.RangeStart.Format(time.DateTime), rangeEnd.Format(time.DateTime), segment.RangeEnd.Format(time.DateTime))
	}
	return sourceDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		var copiedCount int64
		if err := tx.Table(segment.HistoryTableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)...).
			Count(&copiedCount).Error; err != nil {
			return errors.Tag(err)
		}
		if copiedCount != int64(len(ids)) {
			return errors.Errorf("删除前历史表校验失败: table=%s want=%d got=%d", segment.HistoryTableName, len(ids), copiedCount)
		}
		result := tx.Table(job.TableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, rangeEnd)...)...)
		if condition := deleteConditionSQL(job); condition != "" {
			// 清理阶段再次叠加条件和当前删除窗口，避免读取待删主键后业务状态变化导致误删或跨窗口删除。
			result = result.Where("(" + condition + ")")
		}
		deleteResult := result.Delete(nil)
		if deleteResult.Error != nil {
			return errors.Tag(deleteResult.Error)
		}
		if deleteResult.RowsAffected != int64(len(ids)) {
			return errors.Errorf("热表删除校验失败: table=%s want=%d got=%d", job.TableName, len(ids), deleteResult.RowsAffected)
		}
		return nil
	})
}

// updateSegmentCheckpoint 在源库子批次提交成功后推进归档区间 checkpoint。
// 控制库更新不和源库事务强绑定，重试时历史表主键幂等能保证继续向前推进。
func (s *Service) updateSegmentCheckpoint(ctx context.Context, controlDB *gorm.DB, segment *Segment, archivedRows int, lastTime time.Time, lastID int64, workerID string) error {
	if segment == nil || archivedRows <= 0 {
		return nil
	}
	now := time.Now()
	result := controlDB.WithContext(ctx).Clauses(dbresolver.Write).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ?", statusRunning, workerID).
		Updates(map[string]any{
			"last_archived_id":   lastID,
			"last_archived_time": sql.NullTime{Time: lastTime, Valid: true},
			"rows_archived":      gorm.Expr("rows_archived + ?", archivedRows),
			"worker_id":          workerID,
			"lease_expires_at":   sql.NullTime{Time: now.Add(s.leaseTTL()), Valid: true},
			"updated_at":         now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间 checkpoint 更新失败，租约可能已被其他 worker 接管: segment_id=%d worker_id=%s", segment.ID, workerID)
	}
	return nil
}

// ensureStringTimeHistoryMatchesHot 校验字符串时间批次冷热表时间值一致。
func (s *Service) ensureStringTimeHistoryMatchesHot(ctx context.Context, tx *gorm.DB, job jobConfig, historyTable string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var mismatchCount int64
	if err := buildStringTimeHistoryMatchQuery(ctx, tx, job, historyTable, ids).Count(&mismatchCount).Error; err != nil {
		return errors.Tag(err)
	}
	if mismatchCount > 0 {
		return errors.Errorf("历史表业务日期校验失败: table=%s mismatch=%d", historyTable, mismatchCount)
	}
	return nil
}

// buildStringTimeHistoryMatchQuery 构造字符串时间冷热表一致性校验查询。
// 热表主键集合来自当前归档批次，历史表名来自已领取区间；仅统计同主键但业务日期不同的脏数据。
func buildStringTimeHistoryMatchQuery(ctx context.Context, db *gorm.DB, job jobConfig, historyTable string, ids []int64) *gorm.DB {
	primaryKey := quoteIdent(job.PrimaryKey)
	timeColumn := quoteIdent(job.TimeColumn)
	return db.WithContext(ctx).
		Table(quoteIdent(job.TableName)+" AS hot").
		Joins(fmt.Sprintf("JOIN %s AS history ON history.%s = hot.%s", quoteIdent(historyTable), primaryKey, primaryKey)).
		Where(fmt.Sprintf("hot.%s IN ?", primaryKey), ids).
		Where(fmt.Sprintf("history.%s <> hot.%s", timeColumn, timeColumn))
}

// advanceWatermark 只按“最长连续完成区间”推进水位线，不允许跳跃推进。
func (s *Service) advanceWatermark(ctx context.Context, job jobConfig, controlDB *gorm.DB) error {
	lockKey, err := s.archiveJobWatermarkKey(job.Name)
	if err != nil {
		return errors.Tag(err)
	}
	return redislock.WithLock(ctx, s.redisClient(), lockKey, s.lockTTL(), func(lockCtx context.Context) error {
		var watermarkTime *time.Time
		current, err := s.loadWatermark(lockCtx, controlDB, job.Name)
		if err != nil {
			return errors.Tag(err)
		}
		cursor := time.Time{}
		hasCursor := false
		if current != nil && current.WatermarkTime.Valid {
			cursor = current.WatermarkTime.Time
			hasCursor = true
		}

		for {
			query := controlDB.WithContext(lockCtx).
				Where("job_name = ?", job.Name).
				Order("range_start ASC").
				Limit(watermarkScanBatchSize)
			if hasCursor {
				// 已推进过的历史区间不再反复读取，降低控制表长期运行后的扫描成本。
				query = query.Where("range_end > ?", cursor)
			}
			var segments []Segment
			if err := query.Find(&segments).Error; err != nil {
				return errors.Tag(err)
			}
			if len(segments) == 0 {
				break
			}
			progressed := false
			for _, item := range segments {
				if !isArchivedSegmentStatus(item.Status) {
					return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
				}
				if hasCursor && item.RangeStart.After(cursor) {
					// 区间必须连续或重叠完成才能推进 watermark，避免跳过中间断点。
					return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
				}
				if hasCursor && !item.RangeEnd.After(cursor) {
					// 已经被当前水位线覆盖的重叠区间直接跳过。
					continue
				}
				currentEnd := item.RangeEnd
				watermarkTime = &currentEnd
				cursor = currentEnd
				hasCursor = true
				progressed = true
			}
			if len(segments) < watermarkScanBatchSize || !progressed {
				break
			}
		}
		return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
	})
}

// saveWatermarkIfAdvanced 在目标水位线确实向前推进时落库。
func (s *Service) saveWatermarkIfAdvanced(ctx context.Context, controlDB *gorm.DB, job jobConfig, current *Watermark, watermarkTime *time.Time) error {
	if watermarkTime == nil {
		return nil
	}
	if current != nil && current.WatermarkTime.Valid && !watermarkTime.After(current.WatermarkTime.Time) {
		return nil
	}
	record := Watermark{
		JobName:         job.Name,
		SourceTableName: job.TableName,
		WatermarkTime:   sql.NullTime{Time: *watermarkTime, Valid: true},
		UpdatedAt:       time.Now(),
	}
	return controlDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"table_name", "watermark_time", "updated_at"}),
	}).Create(&record).Error
}

// cleanupHistoryTables 在历史表数量超过上限时删除最老历史表，控制表数量和元数据成本。
func (s *Service) cleanupHistoryTables(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB) error {
	maxTables := job.MaxHistoryTables
	if maxTables <= 0 {
		return nil
	}
	lockKey, err := s.archiveJobCleanupKey(job.Name)
	if err != nil {
		return errors.Tag(err)
	}
	return redislock.WithLock(ctx, s.redisClient(), lockKey, s.lockTTL(), func(lockCtx context.Context) error {
		var items []historyTableItem
		if err := buildHistoryCleanupItemsQuery(lockCtx, controlDB, job).Scan(&items).Error; err != nil {
			return errors.Tag(err)
		}
		if len(items) <= maxTables {
			return nil
		}
		expired := items[maxTables:]
		if len(expired) > historyCleanupTableBatchSize {
			// 历史表文件可能很大，单次只淘汰少量表，剩余表留给下一轮低峰归档任务继续处理。
			expired = expired[:historyCleanupTableBatchSize]
		}
		expiredNames := make([]string, 0, len(expired))
		for idx, item := range expired {
			expiredNames = append(expiredNames, item.HistoryTableName)
			if tableExists(lockCtx, sourceDB, item.HistoryTableName) {
				if err := sourceDB.WithContext(lockCtx).Exec(archiveDropHistoryTableSQL(item.HistoryTableName)).Error; err != nil {
					return errors.Tag(err)
				}
			}
			if idx < len(expired)-1 {
				// DROP TABLE 完成后让出 I/O，避免连续删除多个大历史表文件造成磁盘抖动。
				if err := waitArchiveBatch(lockCtx, s.batchDelay()); err != nil {
					return errors.Tag(err)
				}
			}
		}
		if len(expiredNames) > 0 {
			// 历史表已经按保留策略淘汰后，同步清理已完成区间元数据，避免控制表无限增长。
			if err := controlDB.WithContext(lockCtx).
				Where("job_name = ? AND status = ? AND history_table_name IN ?", job.Name, statusDeleted, expiredNames).
				Delete(&Segment{}).Error; err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}

// buildHistoryCleanupItemsQuery 构造历史表保留策略查询。
// 只返回所有区间都已 deleted 的历史表，避免误删未清完热表的历史表。
func buildHistoryCleanupItemsQuery(ctx context.Context, controlDB *gorm.DB, job jobConfig) *gorm.DB {
	return controlDB.WithContext(ctx).
		Model(&Segment{}).
		Select("? AS history_table_name, MIN(?) AS first_range_start", clause.Column{Name: "history_table_name"}, clause.Column{Name: "range_start"}).
		Where(clause.Eq{Column: clause.Column{Name: "job_name"}, Value: job.Name}).
		Where(clause.Neq{Column: clause.Column{Name: "history_table_name"}, Value: ""}).
		Group("history_table_name").
		Having("SUM(CASE WHEN ? <> ? THEN 1 ELSE 0 END) = 0", clause.Column{Name: "status"}, statusDeleted).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "first_range_start"}, Desc: true})
}

// ensureHistoryTable 确保目标历史表存在，并直接复用热表结构与索引定义。
func (s *Service) ensureHistoryTable(ctx context.Context, db *gorm.DB, job jobConfig, historyTable string) error {
	if tableExists(ctx, db, historyTable) {
		return nil
	}
	return db.WithContext(ctx).Exec(archiveCreateHistoryTableSQL(historyTable, job.TableName)).Error
}

// ensureArchiveAccessPath 校验热表是否具备按时间列增序扫描的索引访问路径。
// 归档和删除依赖 time_column 半开窗口索引，缺少左前缀索引时失败。
func (s *Service) ensureArchiveAccessPath(ctx context.Context, db *gorm.DB, job jobConfig) error {
	if db == nil {
		return errors.Errorf("归档源库连接为空: job=%s database=%s", job.Name, job.Database)
	}
	indexes, err := db.WithContext(ctx).Migrator().GetIndexes(job.TableName)
	if err != nil {
		return errors.Tag(err)
	}
	for _, index := range indexes {
		if archiveIndexHasTimePrefix(index.Columns(), job.TimeColumn) {
			return nil
		}
	}
	return errors.Errorf("归档热表缺少时间列左前缀索引，请先由 DBA 补充生产索引: job=%s table=%s recommended_index=(%s,%s)",
		job.Name, job.TableName, job.TimeColumn, job.PrimaryKey)
}

// loadBatchCursorRows 读取当前区间下一批待归档记录的游标信息。
// 排序固定为“时间列升序 + 主键升序”，便于稳定断点续跑。
func (s *Service) loadBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	if job.isStringTimeColumn() {
		return s.loadStringTimeBatchCursorRows(ctx, db, job, segment)
	}
	if job.isUnixSecondsTimeColumn() {
		return s.loadUnixSecondsBatchCursorRows(ctx, db, job, segment)
	}
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	rows := make([]batchCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// loadUnixSecondsBatchCursorRows 读取 Unix int64 时间列的批次游标，并转换为控制表时间。
func (s *Service) loadUnixSecondsBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	type unixSecondsCursorRow struct {
		ID        int64 `gorm:"column:id"`         // 当前批次主键
		CreatedAt int64 `gorm:"column:created_at"` // 当前批次 Unix int64 时间
	}
	rawRows := make([]unixSecondsCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rawRows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	rows := make([]batchCursorRow, 0, len(rawRows))
	for _, raw := range rawRows {
		createdAt, err := parseArchiveUnixTime(raw.CreatedAt, job.TimeColumnUnixUnit)
		if err != nil {
			return nil, errors.Wrapf(err, "归档 Unix 时间非法: table=%s column=%s unit=%s id=%d value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, raw.ID, raw.CreatedAt)
		}
		rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
	}
	return rows, nil
}

// loadStringTimeBatchCursorRows 读取字符串时间列的批次游标，并按配置格式完成严格校验。
func (s *Service) loadStringTimeBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	type dateStringCursorRow struct {
		ID        int64  `gorm:"column:id"`         // 当前批次主键
		CreatedAt string `gorm:"column:created_at"` // 当前批次字符串时间
	}
	rawRows := make([]dateStringCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rawRows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	rows := make([]batchCursorRow, 0, len(rawRows))
	for _, raw := range rawRows {
		createdAt, err := parseArchiveStringTime(raw.CreatedAt, job.TimeColumnFormat)
		if err != nil {
			return nil, errors.Wrapf(err, "归档字符串时间非法: table=%s column=%s id=%d value=%s", job.TableName, job.TimeColumn, raw.ID, raw.CreatedAt)
		}
		rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
	}
	return rows, nil
}

// buildLoadBatchCursorRowsQuery 构造归档游标读取查询。
// 查询按时间半开窗口和主键断点稳定排序，使用 GORM clause 表达式避免在 Go 文件中拼完整 SELECT SQL。
func buildLoadBatchCursorRowsQuery(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, batchSize int) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("? AS id, ? AS created_at", clause.Column{Name: job.PrimaryKey}, clause.Column{Name: job.TimeColumn})
	query = applyArchiveTimeRangeWhere(query, job, segment.RangeStart, segment.RangeEnd)
	if condition := archiveConditionSQL(job); condition != "" {
		// 自定义归档条件只影响当前 job 的待归档行集合，时间窗口仍作为主索引边界保证扫描可控。
		query = query.Where("(" + condition + ")")
	}
	if segment.LastArchivedTime.Valid {
		query = applyArchiveCursorResumeWhere(query, job, segment.LastArchivedTime.Time, segment.LastArchivedID)
	}
	return query.
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.TimeColumn}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.PrimaryKey}}).
		Limit(batchSize)
}

// applyArchiveTimeRangeWhere 按时间列类型追加半开时间窗口条件。
// 字符串和 Unix 整数时间列会先转换为对应存储格式，再绑定查询参数。
func applyArchiveTimeRangeWhere(query *gorm.DB, job jobConfig, rangeStart time.Time, rangeEnd time.Time) *gorm.DB {
	if job.isStringTimeColumn() {
		return query.Where(
			fmt.Sprintf("%s >= ? AND %s < ?", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn)),
			formatArchiveStringTimeArg(job, rangeStart),
			formatArchiveStringTimeArg(job, rangeEnd),
		)
	}
	return query.
		Where(clause.Gte{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, rangeStart)}).
		Where(clause.Lt{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, rangeEnd)})
}

// applyArchiveCursorResumeWhere 追加时间列和主键组成的断点续跑条件。
// 同一业务时间下用主键打破并列；特殊时间列类型需先转换为对应数据库字段类型。
func applyArchiveCursorResumeWhere(query *gorm.DB, job jobConfig, lastTime time.Time, lastID int64) *gorm.DB {
	if job.isStringTimeColumn() {
		lastDate := formatArchiveStringTimeArg(job, lastTime)
		return query.Where(
			fmt.Sprintf("%s > ? OR (%s = ? AND %s > ?)", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn), quoteIdent(job.PrimaryKey)),
			lastDate,
			lastDate,
			lastID,
		)
	}
	if job.isUnixSecondsTimeColumn() {
		lastUnix := archiveUnixTimeArg(job, lastTime)
		return query.Where(
			fmt.Sprintf("%s > ? OR (%s = ? AND %s > ?)", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn), quoteIdent(job.PrimaryKey)),
			lastUnix,
			lastUnix,
			lastID,
		)
	}
	// 断点续跑时先比较时间列，再用主键打破同时间戳并列，避免重复或漏扫。
	resumeCondition := clause.Or(
		clause.Gt{Column: clause.Column{Name: job.TimeColumn}, Value: lastTime},
		clause.And(
			clause.Eq{Column: clause.Column{Name: job.TimeColumn}, Value: lastTime},
			clause.Gt{Column: clause.Column{Name: job.PrimaryKey}, Value: lastID},
		),
	)
	return query.Where(resumeCondition)
}

// minArchivableTime 返回热表中“早于归档上界”的最早时间点。
// 首次运行还没有 watermark 时，会以它为起点规划首个归档区间。
func (s *Service) minArchivableTime(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	if job.isStringTimeColumn() {
		return s.minArchivableStringTime(ctx, job, db, upperBound)
	}
	if job.isUnixSecondsTimeColumn() {
		return s.minArchivableUnixSeconds(ctx, job, db, upperBound)
	}
	// minArchivableTimeResult 承接 GORM 聚合结果；空值表示当前归档条件下没有可规划数据。
	type minArchivableTimeResult struct {
		MinTime sql.NullTime `gorm:"column:min_time"` // MinTime 表示满足时间上界和归档条件的最早业务时间
	}
	var row minArchivableTimeResult
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	return row.MinTime.Time, true, nil
}

// minArchivableUnixSeconds 返回 Unix int64 列中早于归档上界的最早业务时间。
func (s *Service) minArchivableUnixSeconds(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	type result struct {
		MinTime sql.NullInt64 `gorm:"column:min_time"` // min_time 表示最早 Unix int64 时间
	}
	var row result
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	minTime, err := parseArchiveUnixTime(row.MinTime.Int64, job.TimeColumnUnixUnit)
	if err != nil {
		return time.Time{}, false, errors.Wrapf(err, "归档最早 Unix 时间非法: table=%s column=%s unit=%s value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, row.MinTime.Int64)
	}
	return minTime, true, nil
}

// minArchivableStringTime 返回字符串时间列中早于归档上界的最早业务时间。
func (s *Service) minArchivableStringTime(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	type result struct {
		MinTime sql.NullString `gorm:"column:min_time"` // min_time 表示最早字符串时间
	}
	var row result
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	minTime, err := parseArchiveStringTime(row.MinTime.String, job.TimeColumnFormat)
	if err != nil {
		return time.Time{}, false, errors.Wrapf(err, "归档最早字符串时间非法: table=%s column=%s value=%s", job.TableName, job.TimeColumn, row.MinTime.String)
	}
	return minTime, true, nil
}

// buildMinArchivableTimeQuery 构造首次归档规划使用的最早时间查询。
// 查询使用 GORM 链式表达式承载 MIN 聚合、时间上界和自定义归档谓词，避免在业务代码中保留完整 Raw SQL。
func buildMinArchivableTimeQuery(ctx context.Context, db *gorm.DB, job jobConfig, upperBound time.Time) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("MIN(?) AS min_time", clause.Column{Name: job.TimeColumn}).
		Where(clause.Lt{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, upperBound)})
	if job.isUnixSecondsTimeColumn() {
		// Unix 整数列忽略 0 或负数，避免异常历史值把首个归档窗口拖到 1970。
		query = query.Where(clause.Gt{Column: clause.Column{Name: job.TimeColumn}, Value: int64(0)})
	}
	// 首次规划时只从满足归档条件的数据里寻找起点，避免条件过滤后的空表仍生成无意义区间。
	if condition := archiveConditionSQL(job); condition != "" {
		// 自定义归档条件已在配置加载阶段校验为 WHERE 谓词，这里作为 GORM 附加条件接入，避免回退 Raw SQL。
		query = query.Where("(" + condition + ")")
	}
	return query
}

// archiveUpperBound 计算当前安全可归档上界。
// 上界同时受归档延迟和 safe_time 约束，并对齐到窗口结束时间。
func (s *Service) archiveUpperBound(ctx context.Context, job jobConfig, controlDB *gorm.DB) (time.Time, bool) {
	now := time.Now()
	safeTime := now.Add(-time.Duration(s.safeDelayMinutes()) * time.Minute)

	upperBound := now.AddDate(0, 0, -job.ArchiveDelayDays)
	if job.isDateOnlyStringTimeColumn() {
		upperBound = startOfDay(upperBound)
		safeTime = startOfDay(safeTime)
	}
	if safeTime.Before(upperBound) {
		upperBound = safeTime
	}
	if job.hasArchiveSecondWindow() {
		upperBound = alignWindowEnd(upperBound, job.ArchiveWindowSeconds)
	}
	if watermark, err := s.loadWatermark(ctx, controlDB, job.Name); err == nil && watermark != nil && watermark.WatermarkTime.Valid {
		if !upperBound.After(watermark.WatermarkTime.Time) {
			return time.Time{}, false
		}
	}
	return upperBound, true
}

// deleteUpperBound 计算当前安全可删除上界。
// 删除默认按 hot_keep_days 控制，也可通过 delete_delay_days 和 delete_window_seconds 做独立周期。
func (s *Service) deleteUpperBound(ctx context.Context, job jobConfig) (time.Time, bool) {
	now := time.Now()
	safeTime := now.Add(-time.Duration(s.safeDelayMinutes()) * time.Minute)
	upperBound := now.AddDate(0, 0, -job.DeleteDelayDays)
	if job.isDateOnlyStringTimeColumn() {
		upperBound = startOfDay(upperBound)
		safeTime = startOfDay(safeTime)
	}
	if safeTime.Before(upperBound) {
		upperBound = safeTime
	}
	if job.hasDeleteSecondWindow() {
		upperBound = alignWindowEnd(upperBound, job.DeleteWindowSeconds)
	}
	if !upperBound.After(time.Time{}) {
		return time.Time{}, false
	}
	return upperBound, true
}

// loadWatermark 读取指定归档任务当前的水位线记录。
func (s *Service) loadWatermark(ctx context.Context, db *gorm.DB, jobName string) (*Watermark, error) {
	if db == nil || !tableExists(ctx, db, tableNameWatermark) {
		return nil, nil
	}
	var item Watermark
	err := db.WithContext(ctx).Where("job_name = ?", jobName).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &item, nil
}

// markSegmentDone 把区间标记为完成，并清理运行中的租约信息。
func (s *Service) markSegmentDone(ctx context.Context, db *gorm.DB, segmentID uint64, workerID string) error {
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segmentID).
		Where("status = ? AND worker_id = ?", statusRunning, workerID).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    "",
			"completed_at":     sql.NullTime{Time: now, Valid: true},
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间完成状态更新失败，租约可能已被其他 worker 接管: segment_id=%d worker_id=%s", segmentID, workerID)
	}
	return nil
}

// markSegmentDeleted 把已归档区间标记为热表删除完成。
// deleted 状态表示该时间窗已经只保留在历史表中，后续历史表 TTL 清理才允许统计这类区间。
func (s *Service) markSegmentDeleted(ctx context.Context, db *gorm.DB, segmentID uint64, workerID string) error {
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segmentID).
		Where("status = ? AND worker_id = ?", statusDeleting, workerID).
		Updates(map[string]any{
			"status":           statusDeleted,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    "",
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间删除状态更新失败，租约可能已被其他 worker 接管: segment_id=%d worker_id=%s", segmentID, workerID)
	}
	return nil
}

// markSegmentDeletePartial 释放当前删除租约，并保持区间为已归档状态。
// 当 delete_window_seconds 只允许本轮删除大区间中的一个时间片时，剩余数据交给下一次删除调度继续处理。
func (s *Service) markSegmentDeletePartial(ctx context.Context, db *gorm.DB, segmentID uint64, workerID string) error {
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segmentID).
		Where("status = ? AND worker_id = ?", statusDeleting, workerID).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间部分删除状态更新失败，租约可能已被其他 worker 接管: segment_id=%d worker_id=%s", segmentID, workerID)
	}
	return nil
}

// markSegmentFailed 把区间标记为失败，并写入裁剪后的错误摘要以便后续排障与重试。
func (s *Service) markSegmentFailed(ctx context.Context, db *gorm.DB, segmentID uint64, workerID string, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
		if len(message) > 500 {
			message = message[:500]
		}
	}
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segmentID).
		Where("status = ? AND worker_id = ?", statusRunning, workerID).
		Updates(map[string]any{
			"status":           statusFailed,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    message,
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	return nil
}

// markSegmentDeleteFailed 把删除失败区间恢复为 done，并保留错误摘要供下一轮删除任务重试。
// 删除失败不能把已归档区间改成 failed，否则归档 worker 会误以为需要重新搬迁该区间。
func (s *Service) markSegmentDeleteFailed(ctx context.Context, db *gorm.DB, segmentID uint64, workerID string, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
		if len(message) > 500 {
			message = message[:500]
		}
	}
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segmentID).
		Where("status = ? AND worker_id = ?", statusDeleting, workerID).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    message,
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	return nil
}

// resolveTargets 把工作流 targets 解析为已启用归档任务；为空时默认执行全部启用任务。
// target 支持 `job#archive` / `job#delete` 后缀，用于给归档和删除分别配置不同周期。
func (s *Service) resolveTargets(targets []string) []jobRunConfig {
	jobs := s.normalizedJobs()
	if len(targets) == 0 {
		runs := make([]jobRunConfig, 0, len(jobs))
		for _, job := range jobs {
			runs = append(runs, jobRunConfig{Job: job, Mode: archiveRunModeAll})
		}
		return runs
	}
	targetSet := make(map[string]string, len(targets))
	for _, target := range targets {
		name, mode := splitArchiveTarget(target)
		if name == "" {
			continue
		}
		if existingMode, ok := targetSet[name]; ok && existingMode != mode {
			// 同一 job 同时出现 archive/delete 后缀时合并为 all，避免目标去重后遗漏任一动作。
			targetSet[name] = archiveRunModeAll
			continue
		}
		targetSet[name] = mode
	}
	filtered := make([]jobRunConfig, 0, len(targetSet))
	for _, job := range jobs {
		if mode, ok := targetSet[job.Name]; ok {
			filtered = append(filtered, jobRunConfig{Job: job, Mode: mode})
		}
	}
	return filtered
}

// normalizedJobs 把配置文件里的归档任务归一化为运行期结构，并补齐默认值。
func (s *Service) normalizedJobs() []jobConfig {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	cfg := s.svcCtx.CurrentConfig().Archive
	items := make([]jobConfig, 0, len(cfg.Jobs))
	for idx, item := range cfg.Jobs {
		if !item.Enabled {
			continue
		}

		// 归档配置异常时只跳过当前 job，并输出结构化通知，避免一个坏配置影响其它 job。
		if err := validateArchiveJobConfig(item); err != nil {
			s.notifyArchiveJobConfigInvalid(idx, item, err)
			continue
		}

		job := jobConfig{
			Name:                    strings.TrimSpace(item.Name),
			Database:                normalizeArchiveDatabaseName(item.Database),
			TableName:               strings.TrimSpace(item.TableName),
			TimeColumn:              strings.TrimSpace(item.TimeColumn),
			TimeColumnType:          normalizeTimeColumnType(item.TimeColumnType),
			TimeColumnFormat:        strings.TrimSpace(item.TimeColumnFormat),
			TimeColumnUnixUnit:      normalizeTimeColumnUnixUnit(item.TimeColumnType, item.TimeColumnUnixUnit),
			PrimaryKey:              strings.TrimSpace(item.PrimaryKey),
			ArchiveCondition:        strings.TrimSpace(item.ArchiveCondition),
			DeleteCondition:         strings.TrimSpace(item.DeleteCondition),
			SplitUnit:               normalizeSplitUnit(item.SplitUnit),
			CustomDays:              item.CustomDays,
			HotKeepDays:             item.HotKeepDays,
			ArchiveDelayDays:        item.ArchiveDelayDays,
			ArchiveWindowSeconds:    item.ArchiveWindowSeconds,
			ArchiveWindowMode:       normalizeArchiveWindowMode(item.ArchiveWindowMode),
			ArchiveMaxWindowsPerRun: item.ArchiveMaxWindowsPerRun,
			ArchiveAutoMaxWindows:   item.ArchiveAutoMaxWindows,
			ArchiveAutoLightRows:    item.ArchiveAutoLightRows,
			DeleteDisabled:          item.DeleteDisabled,
			DeleteDelayDays:         item.DeleteDelayDays,
			DeleteWindowSeconds:     item.DeleteWindowSeconds,
			DeleteMaxWindowsPerRun:  item.DeleteMaxWindowsPerRun,
			BatchSize:               item.BatchSize,
			DeleteBatchSize:         item.DeleteBatchSize,
			MaxHistoryTables:        item.MaxHistoryTables,
			HistoryTablePrefix:      strings.TrimSpace(item.HistoryTablePrefix),
			HistoryTableNameRule:    strings.TrimSpace(item.HistoryTableNameRule),
			QueryWriteDB:            item.QueryWriteDB,
		}
		if strings.TrimSpace(item.StartAt) != "" {
			startAt, err := parseArchiveStartAt(item.StartAt)
			if err != nil {
				s.notifyArchiveJobConfigInvalid(idx, item, err)
				continue
			}
			job.StartAt = sql.NullTime{Time: startAt, Valid: true}
		}
		if job.TimeColumn == "" {
			job.TimeColumn = "created_at"
		}
		if job.PrimaryKey == "" {
			job.PrimaryKey = "id"
		}
		if job.TimeColumnFormat == "" {
			job.TimeColumnFormat = defaultArchiveStringTimeFormat(job.TimeColumnType)
		}
		if job.HotKeepDays <= 0 {
			job.HotKeepDays = 30
		}
		if job.ArchiveDelayDays <= 0 {
			job.ArchiveDelayDays = job.HotKeepDays
		}
		if job.DeleteDelayDays <= 0 {
			job.DeleteDelayDays = job.HotKeepDays
		}
		job.ArchiveWindowSeconds = normalizeArchiveWindowSeconds(job.ArchiveWindowSeconds)
		job.DeleteWindowSeconds = normalizeArchiveWindowSeconds(job.DeleteWindowSeconds)
		if job.DeleteWindowSeconds <= 0 && job.ArchiveWindowSeconds > 0 {
			job.DeleteWindowSeconds = job.ArchiveWindowSeconds
		}
		if job.ArchiveWindowSeconds > 0 && job.ArchiveMaxWindowsPerRun <= 0 {
			job.ArchiveMaxWindowsPerRun = 1
		}
		if job.DeleteWindowSeconds > 0 && job.DeleteMaxWindowsPerRun <= 0 {
			// 删除窗口大于归档窗口时，按比例清理多个已归档小窗口。
			job.DeleteMaxWindowsPerRun = 1
			if job.ArchiveWindowSeconds > 0 && job.DeleteWindowSeconds > job.ArchiveWindowSeconds {
				job.DeleteMaxWindowsPerRun = (job.DeleteWindowSeconds + job.ArchiveWindowSeconds - 1) / job.ArchiveWindowSeconds
			}
		}
		job.ArchiveMaxWindowsPerRun = capArchiveWindowsPerRun(job.ArchiveMaxWindowsPerRun)
		job.DeleteMaxWindowsPerRun = capArchiveWindowsPerRun(job.DeleteMaxWindowsPerRun)
		if job.BatchSize <= 0 {
			job.BatchSize = positiveOr(cfg.DefaultBatchSize, defaultBatchSize)
		}
		if job.BatchSize > maxArchiveBatchSize {
			job.BatchSize = maxArchiveBatchSize
		}
		job.ArchiveAutoMaxWindows = normalizeArchiveAutoMaxWindows(job)
		job.ArchiveAutoLightRows = normalizeArchiveAutoLightRows(job.ArchiveAutoLightRows)
		job.ArchiveAutoLightElapsed = normalizeArchiveAutoLightElapsed(item.ArchiveAutoLightMs)
		if job.DeleteBatchSize <= 0 {
			job.DeleteBatchSize = positiveOr(cfg.DefaultDeleteBatchSize, defaultDeleteBatchSize)
		}
		if job.DeleteBatchSize > maxArchiveBatchSize {
			job.DeleteBatchSize = maxArchiveBatchSize
		}
		if job.MaxHistoryTables <= 0 {
			job.MaxHistoryTables = positiveOr(cfg.DefaultMaxHistoryTable, defaultMaxHistoryTables)
		}
		if job.HistoryTablePrefix == "" {
			job.HistoryTablePrefix = fmt.Sprintf("%s_archive", job.TableName)
		}
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

// jobByName 根据归档任务名查找对应的运行期配置。
func (s *Service) jobByName(name string) (jobConfig, bool) {
	name = strings.TrimSpace(name)
	for _, job := range s.normalizedJobs() {
		if job.Name == name {
			return job, true
		}
	}
	return jobConfig{}, false
}

// jobSourceWriteDB 返回归档热表/历史表所在源库主库连接。
func (s *Service) jobSourceWriteDB(job jobConfig) *gorm.DB {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	return s.svcCtx.WriteDB(job.Database)
}

// jobControlWriteDB 返回归档控制表所在数据库写连接。
func (s *Service) jobControlWriteDB() *gorm.DB {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	return s.svcCtx.WriteDB(s.controlDatabaseName())
}

// controlDatabaseName 返回归档控制表所在数据库，未指定时回退主库。
func (s *Service) controlDatabaseName() svc.DBName {
	if s == nil || strings.TrimSpace(string(s.controlDatabase)) == "" {
		return svc.DatabaseMain
	}
	return svc.NormalizeDBName(s.controlDatabase)
}

// redisClient 返回归档服务使用的 Redis 客户端，用于规划和推进阶段的分布式锁。
func (s *Service) redisClient() redis.UniversalClient {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	return s.svcCtx.Rds
}

// ensureRedisNamespace 校验归档服务已经注入 Redis 命名空间配置。
func (s *Service) ensureRedisNamespace() error {
	if s == nil || s.svcCtx == nil {
		return errors.Errorf("归档服务上下文未初始化，无法获取 app_id")
	}
	if runtimecfg.AppID() == "" {
		return errors.Errorf("归档服务缺少 app_id 配置")
	}
	return nil
}

// archiveJobPlanKey 返回当前 app_id 作用域下的区间规划锁 key。
func (s *Service) archiveJobPlanKey(jobName string) (string, error) {
	if err := s.ensureRedisNamespace(); err != nil {
		return "", errors.Tag(err)
	}
	return keys.ArchiveJobPlanRedisKey(jobName), nil
}

// archiveJobWatermarkKey 返回当前 app_id 作用域下的水位推进锁 key。
func (s *Service) archiveJobWatermarkKey(jobName string) (string, error) {
	if err := s.ensureRedisNamespace(); err != nil {
		return "", errors.Tag(err)
	}
	return keys.ArchiveJobWatermarkRedisKey(jobName), nil
}

// archiveJobCleanupKey 返回当前 app_id 作用域下的历史表清理锁 key。
func (s *Service) archiveJobCleanupKey(jobName string) (string, error) {
	if err := s.ensureRedisNamespace(); err != nil {
		return "", errors.Tag(err)
	}
	return keys.ArchiveJobCleanupRedisKey(jobName), nil
}

// safeDelayMinutes 返回当前归档模块生效的安全延迟分钟数。
func (s *Service) safeDelayMinutes() int {
	if s == nil || s.svcCtx == nil {
		return defaultSafeDelayMinutes
	}
	return positiveOr(s.svcCtx.CurrentConfig().Archive.SafeDelayMinutes, defaultSafeDelayMinutes)
}

// lockTTL 返回规划区间与推进 watermark 时使用的短锁 TTL。
func (s *Service) lockTTL() time.Duration {
	if s == nil || s.svcCtx == nil {
		return defaultLockTTL
	}
	seconds := positiveOr(s.svcCtx.CurrentConfig().Archive.LockTTLSeconds, int(defaultLockTTL/time.Second))
	return time.Duration(seconds) * time.Second
}

// leaseTTL 返回单个归档区间被 worker 领取后的租约有效期。
func (s *Service) leaseTTL() time.Duration {
	if s == nil || s.svcCtx == nil {
		return defaultLeaseTTL
	}
	seconds := positiveOr(s.svcCtx.CurrentConfig().Archive.LeaseTTLSeconds, int(defaultLeaseTTL/time.Second))
	return time.Duration(seconds) * time.Second
}

// maxConcurrentJobs 返回单次归档工作流允许同时执行的 job 数。
// 该值只控制不同归档目标之间的并发，单个目标内部仍按 batchSize 和 segment checkpoint 分批推进。
func (s *Service) maxConcurrentJobs() int {
	if s == nil || s.svcCtx == nil {
		return defaultMaxConcurrentJobs
	}
	concurrency := positiveOr(s.svcCtx.CurrentConfig().Archive.MaxConcurrentJobs, defaultMaxConcurrentJobs)
	if concurrency > maxConcurrentArchiveJobs {
		return maxConcurrentArchiveJobs
	}
	return concurrency
}

// batchDelay 返回单批归档提交后的保护性等待时间。
// 配置值用于在历史积压很大时主动降低归档写删速率，硬上限防止误配置导致维护任务长时间空等。
func (s *Service) batchDelay() time.Duration {
	if s == nil || s.svcCtx == nil {
		return defaultBatchDelay
	}
	milliseconds := s.svcCtx.CurrentConfig().Archive.BatchDelayMilliseconds
	if milliseconds <= 0 {
		return defaultBatchDelay
	}
	delay := time.Duration(milliseconds) * time.Millisecond
	if delay > maxBatchDelay {
		return maxBatchDelay
	}
	return delay
}
