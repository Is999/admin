package taskreport

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"admin/common/i18n"
	"admin/helper"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"
	"admin/internal/task/taskwire"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// reportPageSize 表示单轮读取 Asynq 任务详情的上限。
	reportPageSize = 100
	// reportTopLimit 控制 Lark 日报中明细列表的最大条数。
	reportTopLimit = 8
	// reportPagePauseEvery 控制跨页扫描的让出频率，避免长窗口统计连续压 Redis。
	reportPagePauseEvery = 10
	// reportPagePause 表示跨页扫描让出时间。
	reportPagePause = 20 * time.Millisecond
	// reportTimeBucket 表示日报时间分布聚合粒度。
	reportTimeBucket = time.Hour
	// reportTraceDetailLimit 控制单个错误任务展示的统计明细数量。
	reportTraceDetailLimit = 3
	// reportGlobalScanLimit 限制单份日报跨队列、跨状态读取的任务详情总数，超限时显式标记日报不完整。
	reportGlobalScanLimit = 10_000
	// reportGlobalScanByteLimit 限制单份日报加载 Redis 任务详情和运行快照的估算总字节数。
	reportGlobalScanByteLimit int64 = 64 << 20
	// reportPageByteLimit 限制单个 Asynq 原生详情页的估算加载字节数。
	reportPageByteLimit int64 = 8 << 20
	// reportQueueLimit 限制单份日报读取队列元数据和终态集合的队列数量。
	reportQueueLimit = 32
	// asynqArchivedTrimThreshold 是 Asynq v0.26.0 开始裁剪 archived 记录时的实际可见数量。
	// 上游 Lua 按闭区间删除最旧记录，会比声明上限多裁一条，因此稳定可见上限是 9,999 条。
	asynqArchivedTrimThreshold = 9_999
)

// QueueManager 描述任务运行日报读取任务系统数据所需的最小能力。
type QueueManager interface {
	ListReportQueues(ctx context.Context, limit int) (*types.TaskQueueListResp, bool, error)                                                                       // ListReportQueues 返回有界队列概览和是否截断。
	ListReportTasks(ctx context.Context, req *types.ListTaskItemsReq, offset, limit int, byteBudget int64) (*types.TaskListResp, types.TaskReportScanUsage, error) // ListReportTasks 批量读取日报终态任务，并返回受控资源消耗。
	GetWorkflowStatusSummaries(ctx context.Context, workflowIDs []string) (map[string]*types.TaskWorkflowStatusResp, error)                                        // GetWorkflowStatusSummaries 批量读取工作流轻量状态。
	CompletedRetention() time.Duration                                                                                                                             // CompletedRetention 返回 completed 保留期。
	ArchivedRetention() time.Duration                                                                                                                              // ArchivedRetention 返回 archived 保留期。
}

// ReportRequest 描述一次任务运行日报统计请求。
type ReportRequest struct {
	WindowStart       time.Time // 统计窗口开始时间，默认取生成基准时间往前 24 小时
	WindowEnd         time.Time // 统计窗口结束时间，默认取生成基准时间
	GeneratedAt       time.Time // 报告生成时间；为空时使用当前时间
	ExcludeWorkflowID string    // 排除当前日报工作流，避免报告把自身计入本窗口
}

// Report 汇总一个统计窗口内周期任务和工作流执行情况。
type Report struct {
	WindowStart           time.Time           // 统计窗口开始时间
	WindowEnd             time.Time           // 统计窗口结束时间
	GeneratedAt           time.Time           // 报告生成时间
	TotalTaskExecutions   int                 // 周期来源任务执行总数，包含触发任务与节点任务
	SuccessTaskExecutions int                 // 成功任务数
	FailedTaskExecutions  int                 // 失败任务数
	PeriodicTriggerTotal  int                 // 周期触发入口任务总数
	PeriodicTriggerOK     int                 // 周期触发入口成功数
	PeriodicTriggerFailed int                 // 周期触发入口失败数
	NodeTaskTotal         int                 // 周期工作流节点任务总数
	WorkflowTotal         int                 // 当前窗口任务涉及的去重工作流实例数
	WorkflowSuccess       int                 // 成功工作流实例数
	WorkflowFailed        int                 // 失败工作流实例数
	WorkflowRunning       int                 // 仍在运行的工作流实例数
	WorkflowUnknown       int                 // 状态已过期或无法确认的工作流实例数
	TraceTotalCount       int64               // 任务执行统计中累计处理总量
	TraceReadCount        int64               // 读取数量
	TraceWriteCount       int64               // 写入数量，包含 insert/update/upsert
	TraceDeleteCount      int64               // 删除数量
	TraceErrorCount       int64               // 隔离错误数量
	AverageDurationMS     int64               // 周期来源任务平均耗时
	MaxDurationMS         int64               // 周期来源任务最大耗时
	QueueSummaries        []QueueSummary      // 队列维度摘要
	PeriodicTasks         []PeriodicSummary   // 周期任务维度摘要
	Workflows             []WorkflowSummary   // 工作流维度摘要
	TimeBuckets           []TimeBucketSummary // 小时时间段分布摘要
	FailureTasks          []TaskSummary       // 失败任务明细
	SlowTasks             []TaskSummary       // 慢任务明细
	TraceErrorTasks       []TaskSummary       // 处理量错误任务明细
	IntegrityWarnings     []string            // 保留期、裁剪或扫描上限导致的数据完整性提示
}

// QueueSummary 描述队列在日报窗口内和当前时刻的摘要。
type QueueSummary struct {
	Name           string // 队列名称
	TaskExecutions int    // 当前窗口周期来源任务数
	Success        int    // 当前窗口成功任务数
	Failed         int    // 当前窗口失败任务数
	Triggers       int    // 当前窗口周期触发任务数
	NodeTasks      int    // 当前窗口节点任务数
	Pending        int    // 当前 pending 数
	Active         int    // 当前 active 数
	Scheduled      int    // 当前 scheduled 数
	Retry          int    // 当前 retry 数
	Archived       int    // 当前 archived 数
	Completed      int    // 当前 completed 数
	Aggregating    int    // 当前 aggregating 数
}

// PeriodicSummary 描述单个周期任务的执行摘要。
type PeriodicSummary struct {
	Name           string // 周期任务名称
	WorkflowName   string // 工作流名称
	Queue          string // 主要队列
	TaskExecutions int    // 任务执行数
	Triggers       int    // 入口触发次数
	NodeTasks      int    // 节点任务数
	Success        int    // 成功任务数
	Failed         int    // 失败任务数
	AverageMS      int64  // 平均耗时
	MaxMS          int64  // 最大耗时
	LastAt         string // 最近活动时间
}

// WorkflowSummary 描述单个工作流名称下的实例摘要。
type WorkflowSummary struct {
	Name      string // 工作流名称
	Periodic  string // 主要周期任务名称
	Queue     string // 主要队列
	Total     int    // 工作流实例数
	Success   int    // 成功实例数
	Failed    int    // 失败实例数
	Running   int    // 运行中实例数
	Unknown   int    // 未知实例数
	NodeTasks int    // 节点任务数
	AverageMS int64  // 节点平均耗时
	MaxMS     int64  // 节点最大耗时
	LastAt    string // 最近活动时间
}

// TaskSummary 描述日报中的任务明细。
type TaskSummary struct {
	ID           string   // Asynq 任务 ID
	Name         string   // 任务展示名
	Type         string   // 任务类型
	State        string   // 任务状态
	Queue        string   // 队列
	PeriodicName string   // 周期任务名称
	WorkflowID   string   // 工作流实例 ID
	WorkflowName string   // 工作流名称
	WorkflowNode string   // 工作流节点
	StartedAt    string   // 开始时间
	FinishedAt   string   // 完成或失败时间
	DurationMS   int64    // 耗时毫秒
	Error        string   // 错误摘要
	TraceErrors  int64    // 执行统计中的隔离错误数量
	TraceDetails []string // 执行统计错误明细
}

// TimeBucketSummary 描述一个时间段内的任务与处理量分布。
type TimeBucketSummary struct {
	StartAt           string // 时间段开始，RFC3339
	EndAt             string // 时间段结束，RFC3339
	TaskExecutions    int    // 任务执行数
	Success           int    // 成功任务数
	Failed            int    // 失败任务数
	Triggers          int    // 周期触发任务数
	NodeTasks         int    // 工作流节点任务数
	TraceTotalCount   int64  // 处理总量
	TraceReadCount    int64  // 读取数量
	TraceWriteCount   int64  // 写入数量，包含 insert/update/upsert
	TraceDeleteCount  int64  // 删除数量
	TraceErrorCount   int64  // 隔离错误数量
	AverageDurationMS int64  // 平均耗时毫秒
	MaxDurationMS     int64  // 最大耗时毫秒
}

// workflowStatus 是日报聚合时读取到的工作流状态快照。
type workflowStatus struct {
	Name   string // 工作流展示名称
	Status string // 工作流当前状态
}

// reportWindow 表示日报统计窗口，按左闭右开边界过滤任务。
type reportWindow struct {
	start    time.Time // 窗口开始时间
	end      time.Time // 窗口结束时间
	hasRange bool      // 是否启用窗口过滤
}

// stateItemsResult 保存一次队列状态扫描结果和完整性边界。
type stateItemsResult struct {
	items                  []types.TaskItem // 当前扫描到的周期来源任务
	archiveAtTrimThreshold bool             // archived 是否达到 Asynq 裁剪阈值
	truncated              bool             // 是否因日报全局扫描预算主动截断
	scanned                int              // 已读取的原始任务详情数，用于扣减全局预算
	bytes                  int64            // 已允许加载的任务详情和运行快照估算字节数
}

// periodicAgg 暂存单个周期配置的执行统计。
type periodicAgg struct {
	item       PeriodicSummary // 对外输出的周期任务摘要
	durationMS int64           // 参与平均耗时计算的总毫秒数
	durationN  int64           // 参与平均耗时计算的样本数
}

// workflowAgg 暂存同名工作流下多个实例的执行统计。
type workflowAgg struct {
	item       WorkflowSummary     // 对外输出的工作流摘要
	ids        map[string]struct{} // 已计入的工作流实例 ID，避免重复计数
	statuses   map[string]string   // 工作流实例 ID 到最终状态的映射
	durationMS int64               // 参与平均耗时计算的总毫秒数
	durationN  int64               // 参与平均耗时计算的样本数
}

// timeBucketAgg 暂存单个小时时间段内的任务统计。
type timeBucketAgg struct {
	item       TimeBucketSummary // 对外输出的时间段摘要
	durationMS int64             // 参与平均耗时计算的总毫秒数
	durationN  int64             // 参与平均耗时计算的样本数
}

// Service 构建任务系统运行日报。
type Service struct {
	manager QueueManager // 任务队列数据读取入口
}

// NewService 创建任务运行日报服务。
func NewService(manager QueueManager) *Service {
	return &Service{manager: manager}
}

// Window 返回指定基准时间往前 24 小时的统计窗口。
func Window(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	// Asynq 终态 zset 和任务消息只保留秒级时间，窗口必须使用相同精度才能保证相邻日报不漏不重。
	now = now.Truncate(time.Second)
	return now.Add(-24 * time.Hour), now
}

// Build 汇总指定窗口内由周期调度触发的 completed/archived 任务。
func (s *Service) Build(ctx context.Context, req ReportRequest) (Report, error) {
	if s == nil || s.manager == nil {
		return Report{}, errors.Errorf("任务运行日报管理器为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req.WindowStart.IsZero() != req.WindowEnd.IsZero() {
		return Report{}, errors.Errorf("任务运行日报统计窗口必须同时提供开始和结束时间")
	}
	req = normalizeRequest(req)
	if req.WindowStart.Nanosecond() != 0 || req.WindowEnd.Nanosecond() != 0 {
		return Report{}, errors.Errorf("任务运行日报统计窗口必须按整秒对齐 start=%s end=%s", req.WindowStart.Format(time.RFC3339Nano), req.WindowEnd.Format(time.RFC3339Nano))
	}
	if !req.WindowEnd.After(req.WindowStart) {
		return Report{}, errors.Errorf("任务运行日报统计窗口非法 start=%s end=%s", req.WindowStart.Format(time.RFC3339), req.WindowEnd.Format(time.RFC3339))
	}
	window := reportWindow{start: req.WindowStart, end: req.WindowEnd, hasRange: true}
	queueSummaries, queueLimited, err := s.queueSummaries(ctx)
	if err != nil {
		return Report{}, errors.Tag(err)
	}
	items := make([]types.TaskItem, 0)
	archiveTrimQueues := make(map[string]struct{})
	truncatedStates := make(map[string]struct{})
	remainingScanBudget := reportGlobalScanLimit
	remainingByteBudget := reportGlobalScanByteLimit
	// 先扫描 archived，确保全局预算紧张时优先保留失败证据，再扫描 completed。
	for _, state := range []string{"archived", "completed"} {
		for _, summary := range queueSummaries {
			stateKey := summary.Name + "/" + state
			if state == asynq.TaskStateArchived.String() && summary.Archived >= asynqArchivedTrimThreshold {
				archiveTrimQueues[summary.Name] = struct{}{}
			}
			if remainingScanBudget <= 0 || remainingByteBudget <= 0 {
				truncatedStates[stateKey] = struct{}{}
				continue
			}
			stateResult, err := s.stateItems(ctx, summary.Name, state, window, remainingScanBudget, remainingByteBudget)
			if err != nil {
				return Report{}, errors.Tag(err)
			}
			remainingScanBudget -= stateResult.scanned
			remainingByteBudget -= stateResult.bytes
			items = append(items, stateResult.items...)
			if stateResult.archiveAtTrimThreshold {
				archiveTrimQueues[summary.Name] = struct{}{}
			}
			if stateResult.truncated {
				truncatedStates[stateKey] = struct{}{}
			}
		}
	}
	items = dedupeReportItems(items)
	if excludedID := strings.TrimSpace(req.ExcludeWorkflowID); excludedID != "" {
		filtered := items[:0]
		for _, item := range items {
			if itemWorkflowID(item) != excludedID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	statuses, err := s.workflowStatuses(ctx, items)
	if err != nil {
		return Report{}, errors.Tag(err)
	}
	warnings := s.integrityWarnings(ctx, req.WindowStart, req.GeneratedAt, archiveTrimQueues, truncatedStates, queueLimited)
	return buildReport(req, queueSummaries, items, statuses, warnings), nil
}

// normalizeRequest 补齐日报窗口和生成时间。
func normalizeRequest(req ReportRequest) ReportRequest {
	now := req.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	if req.WindowStart.IsZero() || req.WindowEnd.IsZero() {
		req.WindowStart, req.WindowEnd = Window(now)
	}
	if req.GeneratedAt.IsZero() {
		req.GeneratedAt = now
	}
	return req
}

// queueSummaries 读取队列当前积压概览，空队列时补默认展示队列。
func (s *Service) queueSummaries(ctx context.Context) ([]QueueSummary, bool, error) {
	resp, limited, err := s.manager.ListReportQueues(ctx, reportQueueLimit)
	if err != nil {
		return nil, false, errors.Tag(err)
	}
	if resp == nil {
		return nil, false, errors.Errorf("任务运行日报队列概览为空")
	}
	result := make([]QueueSummary, 0, len(resp.Queues))
	for _, item := range resp.Queues {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		result = append(result, QueueSummary{
			Name:        name,
			Pending:     item.Pending,
			Active:      item.Active,
			Scheduled:   item.Scheduled,
			Retry:       item.Retry,
			Archived:    item.Archived,
			Completed:   item.Completed,
			Aggregating: item.Aggregating,
		})
	}
	if len(result) == 0 {
		result = append(result,
			QueueSummary{Name: taskwire.QueueCritical},
			QueueSummary{Name: taskwire.QueueDefault},
			QueueSummary{Name: taskwire.QueueMaintenance},
		)
	}
	return result, limited, nil
}

// stateItems 分页读取指定状态下周期来源任务，直到覆盖整个统计窗口。
func (s *Service) stateItems(ctx context.Context, queueName string, state string, window reportWindow, scanBudget int, byteBudget int64) (stateItemsResult, error) {
	if scanBudget <= 0 || byteBudget <= 0 {
		return stateItemsResult{truncated: true}, nil
	}
	// 任务列表接口的结束时间为闭区间；减一纳秒后按秒格式化，使日报查询保持右开边界。
	queryEnd := window.end
	if !queryEnd.IsZero() {
		queryEnd = queryEnd.Add(-time.Nanosecond)
	}
	result := stateItemsResult{items: make([]types.TaskItem, 0)}
	seenTaskIDs := make(map[string]struct{})
	expectedTotal := int64(-1)
	for offset := 0; ; {
		if err := ctx.Err(); err != nil {
			return stateItemsResult{}, errors.Tag(err)
		}
		if scanBudget-result.scanned < reportPageSize {
			result.truncated = true
			break
		}
		remainingBytes := byteBudget - result.bytes
		if remainingBytes <= 0 {
			result.truncated = true
			break
		}
		pageByteBudget := min(remainingBytes, reportPageByteLimit)
		resp, usage, err := s.manager.ListReportTasks(ctx, &types.ListTaskItemsReq{
			Queue:     queueName,
			State:     state,
			StartTime: formatWindowTime(window.start),
			EndTime:   formatWindowTime(queryEnd),
		}, offset, reportPageSize, pageByteBudget)
		if err != nil {
			return stateItemsResult{}, errors.Tag(err)
		}
		if usage.Tasks < 0 || usage.Tasks > reportPageSize || result.scanned+usage.Tasks > scanBudget {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描条数预算返回非法 queue=%s state=%s scanned=%d remaining=%d", queueName, state, usage.Tasks, scanBudget-result.scanned)
		}
		if usage.Bytes < 0 || usage.Bytes > pageByteBudget || result.bytes+usage.Bytes > byteBudget {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描字节预算返回非法 queue=%s state=%s bytes=%d remaining=%d", queueName, state, usage.Bytes, byteBudget-result.bytes)
		}
		result.scanned += usage.Tasks
		result.bytes += usage.Bytes
		if resp == nil {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描响应为空 queue=%s state=%s", queueName, state)
		}
		if resp.Total < 0 || len(resp.Tasks) > reportPageSize || int64(offset+len(resp.Tasks)) > resp.Total {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描分页响应非法 queue=%s state=%s offset=%d limit=%d tasks=%d total=%d", queueName, state, offset, reportPageSize, len(resp.Tasks), resp.Total)
		}
		if state == asynq.TaskStateArchived.String() && resp.Total >= asynqArchivedTrimThreshold {
			result.archiveAtTrimThreshold = true
		}
		if usage.ByteLimited {
			result.truncated = true
			break
		}
		if len(resp.Tasks) == 0 && int64(offset) < resp.Total {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描提前结束 queue=%s state=%s offset=%d total=%d", queueName, state, offset, resp.Total)
		}
		if expectedTotal < 0 {
			expectedTotal = resp.Total
		} else if resp.Total != expectedTotal {
			return stateItemsResult{}, errors.Errorf("日报任务状态扫描期间总数变化 queue=%s state=%s before=%d after=%d", queueName, state, expectedTotal, resp.Total)
		}
		if resp.Total > int64(scanBudget) {
			result.truncated = true
		}
		for _, item := range resp.Tasks {
			identity := reportTaskIdentity(item)
			if identity == "" {
				return stateItemsResult{}, errors.Errorf("日报任务状态扫描返回空任务标识 queue=%s state=%s", queueName, state)
			}
			if _, exists := seenTaskIDs[identity]; exists {
				return stateItemsResult{}, errors.Errorf("日报任务状态扫描返回重复任务 queue=%s state=%s task_id=%s", queueName, state, item.ID)
			}
			seenTaskIDs[identity] = struct{}{}
			if !taskItemHasPeriodicSource(item) || !reportContains(window, item) {
				continue
			}
			result.items = append(result.items, item)
		}
		offset += len(resp.Tasks)
		if len(resp.Tasks) == 0 || int64(offset) >= resp.Total {
			break
		}
		if result.scanned >= scanBudget {
			result.truncated = true
			break
		}
		if err := pauseReportPage(ctx, offset/reportPageSize); err != nil {
			return stateItemsResult{}, errors.Tag(err)
		}
	}
	if !result.truncated && expectedTotal >= 0 && int64(len(seenTaskIDs)) != expectedTotal {
		return stateItemsResult{}, errors.Errorf("日报任务状态扫描数量不一致 queue=%s state=%s unique=%d total=%d", queueName, state, len(seenTaskIDs), expectedTotal)
	}
	return result, nil
}

// reportTaskIdentity 返回任务跨状态去重使用的稳定标识。
func reportTaskIdentity(item types.TaskItem) string {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return ""
	}
	return strings.TrimSpace(item.Queue) + "\x00" + id
}

// dedupeReportItems 防止任务在日报扫描期间迁移状态后被 completed/archived 重复计数。
func dedupeReportItems(items []types.TaskItem) []types.TaskItem {
	result := make([]types.TaskItem, 0, len(items))
	indexes := make(map[string]int, len(items))
	for _, item := range items {
		identity := reportTaskIdentity(item)
		if identity == "" {
			result = append(result, item)
			continue
		}
		index, ok := indexes[identity]
		if !ok {
			indexes[identity] = len(result)
			result = append(result, item)
			continue
		}
		currentTime, currentOK := taskItemPrimaryTime(result[index])
		candidateTime, candidateOK := taskItemPrimaryTime(item)
		if !currentOK || (candidateOK && !candidateTime.Before(currentTime)) {
			result[index] = item
		}
	}
	return result
}

// pauseReportPage 在长窗口扫描中按页轻量让出，避免维护任务连续占用 Redis。
func pauseReportPage(ctx context.Context, page int) error {
	if reportPagePauseEvery <= 0 || reportPagePause <= 0 || page%reportPagePauseEvery != 0 {
		return nil
	}
	timer := time.NewTimer(reportPagePause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// reportContains 使用左闭右开窗口，避免边界任务被相邻两次日报重复统计。
func reportContains(window reportWindow, item types.TaskItem) bool {
	if !window.hasRange {
		return true
	}
	itemTime, ok := taskItemPrimaryTime(item)
	if !ok {
		return false
	}
	if !window.start.IsZero() && itemTime.Before(window.start) {
		return false
	}
	if !window.end.IsZero() && !itemTime.Before(window.end) {
		return false
	}
	return true
}

// workflowStatuses 读取任务关联工作流状态；仅快照不存在时标记未知，读取故障交给任务重试。
func (s *Service) workflowStatuses(ctx context.Context, items []types.TaskItem) (map[string]workflowStatus, error) {
	ids := make(map[string]string)
	for _, item := range items {
		workflowID := itemWorkflowID(item)
		if workflowID == "" {
			continue
		}
		ids[workflowID] = itemWorkflowName(item)
	}
	statuses := make(map[string]workflowStatus, len(ids))
	if len(ids) == 0 {
		return statuses, nil
	}
	workflowIDs := make([]string, 0, len(ids))
	for workflowID := range ids {
		workflowIDs = append(workflowIDs, workflowID)
	}
	sort.Strings(workflowIDs)
	summaries, err := s.manager.GetWorkflowStatusSummaries(ctx, workflowIDs)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, errors.Wrap(err, "批量读取日报工作流状态失败")
	}
	for _, workflowID := range workflowIDs {
		fallbackName := ids[workflowID]
		meta := summaries[workflowID]
		if meta == nil {
			statuses[workflowID] = workflowStatus{Name: fallbackName, Status: "unknown"}
			continue
		}
		status := strings.TrimSpace(meta.Status)
		switch status {
		case "success", "failed", "running", "pending":
		default:
			status = "unknown"
		}
		statuses[workflowID] = workflowStatus{
			Name:   helper.FirstNonEmptyString(strings.TrimSpace(meta.WorkflowName), fallbackName),
			Status: status,
		}
	}
	return statuses, nil
}

// integrityWarnings 汇总终态保留、Asynq 裁剪和主动资源上限导致的完整性风险。
func (s *Service) integrityWarnings(ctx context.Context, start, generatedAt time.Time, archiveQueues, truncatedStates map[string]struct{}, queueLimited bool) []string {
	warnings := make([]string, 0, 4)
	if warning := s.retentionWarning(ctx, start, generatedAt); warning != "" {
		warnings = append(warnings, warning)
	}
	locale := reportLocale(ctx)
	if names := sortedStringSet(archiveQueues); len(names) > 0 {
		warnings = append(warnings, i18n.MessageByKey(i18n.MsgKeyTaskReportArchiveTrimWarning, locale, strings.Join(names, ", ")))
	}
	if names := sortedStringSet(truncatedStates); len(names) > 0 {
		warnings = append(warnings, i18n.MessageByKey(i18n.MsgKeyTaskReportScanLimitWarning, locale, strings.Join(names, ", "), reportGlobalScanLimit, reportGlobalScanByteLimit/(1<<20)))
	}
	if queueLimited {
		warnings = append(warnings, i18n.MessageByKey(i18n.MsgKeyTaskReportQueueLimitWarning, locale, reportQueueLimit))
	}
	return warnings
}

// sortedStringSet 把集合转换为稳定排序列表，保证日报和测试输出可复现。
func sortedStringSet(items map[string]struct{}) []string {
	result := make([]string, 0, len(items))
	for item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

// retentionWarning 在窗口最早数据已达到任一终态任务保留期时给出运维提示。
func (s *Service) retentionWarning(ctx context.Context, start, generatedAt time.Time) string {
	if start.IsZero() || generatedAt.IsZero() || generatedAt.Before(start) {
		return ""
	}
	retention := s.manager.CompletedRetention()
	if archivedRetention := s.manager.ArchivedRetention(); archivedRetention > 0 && (retention <= 0 || archivedRetention < retention) {
		retention = archivedRetention
	}
	if retention > generatedAt.Sub(start) {
		return ""
	}
	return i18n.MessageByKey(i18n.MsgKeyTaskReportRetentionWarning, reportLocale(ctx))
}

// reportLocale 返回日报任务上下文语言；周期任务未携带请求语言时使用默认语言。
func reportLocale(ctx context.Context) string {
	if meta := requestctx.FromContext(ctx); meta != nil {
		return meta.Locale
	}
	return ""
}

// buildReport 将任务明细聚合为队列、周期配置、工作流和异常明细四类摘要。
func buildReport(req ReportRequest, queues []QueueSummary, items []types.TaskItem, statuses map[string]workflowStatus, warnings []string) Report {
	report := Report{
		WindowStart:       req.WindowStart,
		WindowEnd:         req.WindowEnd,
		GeneratedAt:       req.GeneratedAt,
		QueueSummaries:    append([]QueueSummary(nil), queues...),
		IntegrityWarnings: append([]string(nil), warnings...),
	}
	queueIndex := make(map[string]int, len(report.QueueSummaries))
	for idx := range report.QueueSummaries {
		queueIndex[report.QueueSummaries[idx].Name] = idx
	}
	periodicAggs := make(map[string]*periodicAgg)
	workflowAggs := make(map[string]*workflowAgg)
	timeBucketAggs := make(map[string]*timeBucketAgg)
	workflowIDs := make(map[string]string)
	var totalDurationMS int64
	var totalDurationN int64
	for _, item := range items {
		if !taskItemHasPeriodicSource(item) {
			continue
		}
		task := taskFromItem(item)
		report.TotalTaskExecutions++
		if task.State == asynq.TaskStateArchived.String() {
			report.FailedTaskExecutions++
			report.FailureTasks = append(report.FailureTasks, task)
		} else {
			report.SuccessTaskExecutions++
		}
		if item.TaskType == taskwire.TypeWorkflowTrigger {
			report.PeriodicTriggerTotal++
			if task.State == asynq.TaskStateArchived.String() {
				report.PeriodicTriggerFailed++
			} else {
				report.PeriodicTriggerOK++
			}
		} else {
			report.NodeTaskTotal++
		}
		if task.DurationMS > 0 {
			totalDurationMS += task.DurationMS
			totalDurationN++
			if task.DurationMS > report.MaxDurationMS {
				report.MaxDurationMS = task.DurationMS
			}
			report.SlowTasks = append(report.SlowTasks, task)
		}
		addTraceCounts(&report, item)
		if task.TraceErrors > 0 {
			report.TraceErrorTasks = append(report.TraceErrorTasks, task)
		}
		addTimeBucket(timeBucketAggs, task, item)
		queuePos := ensureQueue(&report, queueIndex, task.Queue)
		addQueueTask(&report.QueueSummaries[queuePos], item)
		addPeriodic(periodicAggs, task, item)
		addWorkflow(workflowAggs, workflowIDs, task, item, statuses)
	}
	if totalDurationN > 0 {
		report.AverageDurationMS = totalDurationMS / totalDurationN
	}
	finalizeWorkflows(workflowAggs)
	report.WorkflowTotal = len(workflowIDs)
	for _, agg := range workflowAggs {
		report.WorkflowSuccess += agg.item.Success
		report.WorkflowFailed += agg.item.Failed
		report.WorkflowRunning += agg.item.Running
		report.WorkflowUnknown += agg.item.Unknown
		report.Workflows = append(report.Workflows, agg.item)
	}
	for _, agg := range periodicAggs {
		if agg.durationN > 0 {
			agg.item.AverageMS = agg.durationMS / agg.durationN
		}
		report.PeriodicTasks = append(report.PeriodicTasks, agg.item)
	}
	for _, agg := range timeBucketAggs {
		if agg.durationN > 0 {
			agg.item.AverageDurationMS = agg.durationMS / agg.durationN
		}
		report.TimeBuckets = append(report.TimeBuckets, agg.item)
	}
	sortReport(&report)
	return report
}

// ensureQueue 返回队列摘要下标，任务队列不在概览中时动态补位。
func ensureQueue(report *Report, queueIndex map[string]int, queueName string) int {
	queueName = strings.TrimSpace(queueName)
	if queueName == "" {
		queueName = "unknown"
	}
	if idx, ok := queueIndex[queueName]; ok {
		return idx
	}
	report.QueueSummaries = append(report.QueueSummaries, QueueSummary{Name: queueName})
	idx := len(report.QueueSummaries) - 1
	queueIndex[queueName] = idx
	return idx
}

// addQueueTask 将单条任务计入队列维度摘要。
func addQueueTask(summary *QueueSummary, item types.TaskItem) {
	summary.TaskExecutions++
	if item.State == asynq.TaskStateArchived.String() {
		summary.Failed++
	} else {
		summary.Success++
	}
	if item.TaskType == taskwire.TypeWorkflowTrigger {
		summary.Triggers++
	} else {
		summary.NodeTasks++
	}
}

// addPeriodic 将单条任务计入周期配置维度摘要。
func addPeriodic(aggs map[string]*periodicAgg, task TaskSummary, item types.TaskItem) {
	name := helper.FirstNonEmptyString(strings.TrimSpace(task.PeriodicName), "unknown")
	agg := aggs[name]
	if agg == nil {
		agg = &periodicAgg{item: PeriodicSummary{Name: name}}
		aggs[name] = agg
	}
	agg.item.TaskExecutions++
	agg.item.WorkflowName = helper.FirstNonEmptyString(agg.item.WorkflowName, task.WorkflowName)
	agg.item.Queue = helper.FirstNonEmptyString(agg.item.Queue, task.Queue)
	if item.TaskType == taskwire.TypeWorkflowTrigger {
		agg.item.Triggers++
	} else {
		agg.item.NodeTasks++
	}
	if task.State == asynq.TaskStateArchived.String() {
		agg.item.Failed++
	} else {
		agg.item.Success++
	}
	if task.DurationMS > 0 {
		agg.durationMS += task.DurationMS
		agg.durationN++
		if task.DurationMS > agg.item.MaxMS {
			agg.item.MaxMS = task.DurationMS
		}
	}
	if taskTimeAfter(taskTime(task), agg.item.LastAt) {
		agg.item.LastAt = taskTime(task)
	}
}

// addWorkflow 将单条任务计入工作流维度摘要，并按实例 ID 去重。
func addWorkflow(aggs map[string]*workflowAgg, ids map[string]string, task TaskSummary, item types.TaskItem, statuses map[string]workflowStatus) {
	workflowID := strings.TrimSpace(task.WorkflowID)
	if workflowID == "" {
		return
	}
	workflowName := helper.FirstNonEmptyString(strings.TrimSpace(task.WorkflowName), "unknown")
	if status, ok := statuses[workflowID]; ok && strings.TrimSpace(status.Name) != "" {
		workflowName = strings.TrimSpace(status.Name)
	}
	ids[workflowID] = workflowName
	agg := aggs[workflowName]
	if agg == nil {
		agg = &workflowAgg{
			item:     WorkflowSummary{Name: workflowName},
			ids:      make(map[string]struct{}),
			statuses: make(map[string]string),
		}
		aggs[workflowName] = agg
	}
	if _, exists := agg.ids[workflowID]; !exists {
		agg.ids[workflowID] = struct{}{}
		agg.item.Total++
	}
	agg.statuses[workflowID] = mergeWorkflowStatus(agg.statuses[workflowID], workflowStatusValue(workflowID, task.State, statuses))
	agg.item.Periodic = helper.FirstNonEmptyString(agg.item.Periodic, task.PeriodicName)
	agg.item.Queue = helper.FirstNonEmptyString(agg.item.Queue, task.Queue)
	if item.TaskType != taskwire.TypeWorkflowTrigger {
		agg.item.NodeTasks++
		if task.DurationMS > 0 {
			agg.durationMS += task.DurationMS
			agg.durationN++
			if task.DurationMS > agg.item.MaxMS {
				agg.item.MaxMS = task.DurationMS
			}
		}
	}
	if taskTimeAfter(taskTime(task), agg.item.LastAt) {
		agg.item.LastAt = taskTime(task)
	}
}

// workflowStatusValue 优先保留工作流终态，其次使用任务归档失败证据覆盖陈旧非终态。
func workflowStatusValue(workflowID string, taskState string, statuses map[string]workflowStatus) string {
	status := strings.TrimSpace(statuses[workflowID].Status)
	if status == "success" || status == "failed" {
		return status
	}
	if taskState == asynq.TaskStateArchived.String() {
		return "failed"
	}
	if status == "running" || status == "pending" {
		return status
	}
	if status == "unknown" {
		return "unknown"
	}
	if taskState == asynq.TaskStateCompleted.String() {
		return "success"
	}
	return "unknown"
}

// mergeWorkflowStatus 合并同一工作流实例的多条任务状态，失败优先于运行中和成功。
func mergeWorkflowStatus(current string, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" || current == "unknown" {
		return helper.FirstNonEmptyString(next, "unknown")
	}
	if next == "" || next == "unknown" {
		return current
	}
	if current == "failed" || next == "failed" {
		return "failed"
	}
	if current == "running" || current == "pending" || next == "running" || next == "pending" {
		return "running"
	}
	return "success"
}

// finalizeWorkflows 汇总每个工作流名称下的实例状态和平均耗时。
func finalizeWorkflows(aggs map[string]*workflowAgg) {
	for _, agg := range aggs {
		if agg.durationN > 0 {
			agg.item.AverageMS = agg.durationMS / agg.durationN
		}
		agg.item.Success = 0
		agg.item.Failed = 0
		agg.item.Running = 0
		agg.item.Unknown = 0
		for _, status := range agg.statuses {
			switch status {
			case "success":
				agg.item.Success++
			case "failed":
				agg.item.Failed++
			case "running", "pending":
				agg.item.Running++
			default:
				agg.item.Unknown++
			}
		}
	}
}

// addTraceCounts 累加任务处理量快照中的读写删和错误统计。
func addTraceCounts(report *Report, item types.TaskItem) {
	if item.ExecutionTrace == nil {
		return
	}
	report.TraceTotalCount += item.ExecutionTrace.TotalCount
	report.TraceReadCount += item.ExecutionTrace.ReadCount
	report.TraceWriteCount += item.ExecutionTrace.InsertCount + item.ExecutionTrace.UpdateCount + item.ExecutionTrace.UpsertCount
	report.TraceDeleteCount += item.ExecutionTrace.DeleteCount
	report.TraceErrorCount += item.ExecutionTrace.ErrorCount
}

// addTimeBucket 将单条任务计入小时时间段分布。
func addTimeBucket(aggs map[string]*timeBucketAgg, task TaskSummary, item types.TaskItem) {
	taskAt, ok := taskBucketTime(task)
	if !ok {
		return
	}
	bucketStart := taskAt.Truncate(reportTimeBucket)
	bucketEnd := bucketStart.Add(reportTimeBucket)
	key := bucketStart.Format(time.RFC3339)
	agg := aggs[key]
	if agg == nil {
		agg = &timeBucketAgg{item: TimeBucketSummary{
			StartAt: bucketStart.Format(time.RFC3339),
			EndAt:   bucketEnd.Format(time.RFC3339),
		}}
		aggs[key] = agg
	}
	agg.item.TaskExecutions++
	if task.State == asynq.TaskStateArchived.String() {
		agg.item.Failed++
	} else {
		agg.item.Success++
	}
	if item.TaskType == taskwire.TypeWorkflowTrigger {
		agg.item.Triggers++
	} else {
		agg.item.NodeTasks++
	}
	if task.DurationMS > 0 {
		agg.durationMS += task.DurationMS
		agg.durationN++
		if task.DurationMS > agg.item.MaxDurationMS {
			agg.item.MaxDurationMS = task.DurationMS
		}
	}
	if item.ExecutionTrace == nil {
		return
	}
	agg.item.TraceTotalCount += item.ExecutionTrace.TotalCount
	agg.item.TraceReadCount += item.ExecutionTrace.ReadCount
	agg.item.TraceWriteCount += item.ExecutionTrace.InsertCount + item.ExecutionTrace.UpdateCount + item.ExecutionTrace.UpsertCount
	agg.item.TraceDeleteCount += item.ExecutionTrace.DeleteCount
	agg.item.TraceErrorCount += item.ExecutionTrace.ErrorCount
}

// taskBucketTime 返回任务参与时间分布聚合的时间点。
func taskBucketTime(task TaskSummary) (time.Time, bool) {
	raw := taskTime(task)
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// taskTraceErrorCount 返回任务执行统计中的隔离错误数量。
func taskTraceErrorCount(snapshot *taskstats.Snapshot) int64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.ErrorCount
}

// taskTraceErrorDetails 提取错误动作明细，便于日报直接定位异常对象。
func taskTraceErrorDetails(snapshot *taskstats.Snapshot) []string {
	if snapshot == nil || snapshot.ErrorCount <= 0 {
		return nil
	}
	result := make([]string, 0, reportTraceDetailLimit)
	for _, detail := range snapshot.Details {
		if detail.Action != taskstats.ActionError || detail.Count <= 0 {
			continue
		}
		name := strings.TrimSpace(detail.Name)
		if name == "" {
			name = taskstats.DetailNameDefault
		}
		result = append(result, name+" "+strconv.FormatInt(detail.Count, 10))
		if len(result) >= reportTraceDetailLimit {
			break
		}
	}
	return result
}

// taskFromItem 将任务列表项转换为日报明细项。
func taskFromItem(item types.TaskItem) TaskSummary {
	return TaskSummary{
		ID:           item.ID,
		Name:         helper.FirstNonEmptyString(strings.TrimSpace(item.TaskName), strings.TrimSpace(item.TaskType)),
		Type:         item.TaskType,
		State:        item.State,
		Queue:        item.Queue,
		PeriodicName: strings.TrimSpace(item.Headers[taskwire.HeaderPeriodicName]),
		WorkflowID:   itemWorkflowID(item),
		WorkflowName: itemWorkflowName(item),
		WorkflowNode: strings.TrimSpace(item.Headers[taskwire.HeaderWorkflowNode]),
		StartedAt:    item.StartedAt,
		FinishedAt:   taskFinishedAt(item),
		DurationMS:   item.DurationMS,
		Error:        strings.TrimSpace(item.LastErr),
		TraceErrors:  taskTraceErrorCount(item.ExecutionTrace),
		TraceDetails: taskTraceErrorDetails(item.ExecutionTrace),
	}
}

// taskItemHasPeriodicSource 判断任务是否来自周期调度链路。
func taskItemHasPeriodicSource(item types.TaskItem) bool {
	return strings.TrimSpace(item.Headers[taskwire.HeaderTaskSource]) == taskwire.WorkflowSourcePeriodic
}

// itemWorkflowID 从任务字段和 header 中提取工作流实例 ID。
func itemWorkflowID(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.WorkflowID),
		strings.TrimSpace(item.Headers[taskwire.HeaderWorkflowID]),
	)
}

// itemWorkflowName 从任务 header 中提取工作流名称。
func itemWorkflowName(item types.TaskItem) string {
	return strings.TrimSpace(item.Headers[taskwire.HeaderWorkflowName])
}

// taskFinishedAt 按任务状态返回成功或失败终态时间。
func taskFinishedAt(item types.TaskItem) string {
	if item.State == asynq.TaskStateArchived.String() {
		return item.LastFailedAt
	}
	return item.CompletedAt
}

// taskItemPrimaryTime 返回任务参与窗口过滤的主时间。
func taskItemPrimaryTime(item types.TaskItem) (time.Time, bool) {
	for _, raw := range []string{taskFinishedAt(item), item.StartedAt, item.NextProcessAt} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// taskTime 返回任务排序使用的终态时间，缺失时回退开始时间。
func taskTime(task TaskSummary) string {
	for _, value := range []string{task.FinishedAt, task.StartedAt} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// taskTimeAfter 比较 RFC3339 时间文本，解析失败时退回字符串比较。
func taskTimeAfter(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return false
	}
	if right == "" {
		return true
	}
	leftTime, leftErr := time.Parse(time.RFC3339, left)
	rightTime, rightErr := time.Parse(time.RFC3339, right)
	if leftErr != nil || rightErr != nil {
		return left > right
	}
	return leftTime.After(rightTime)
}

// sortReport 稳定排序日报摘要，并裁剪 Lark 展示明细数量。
func sortReport(report *Report) {
	sort.Slice(report.QueueSummaries, func(i, j int) bool {
		return report.QueueSummaries[i].Name < report.QueueSummaries[j].Name
	})
	sort.Slice(report.PeriodicTasks, func(i, j int) bool {
		return periodicLess(report.PeriodicTasks[i], report.PeriodicTasks[j])
	})
	sort.Slice(report.Workflows, func(i, j int) bool {
		return workflowLess(report.Workflows[i], report.Workflows[j])
	})
	sort.Slice(report.TimeBuckets, func(i, j int) bool {
		return report.TimeBuckets[i].StartAt < report.TimeBuckets[j].StartAt
	})
	sort.Slice(report.FailureTasks, func(i, j int) bool {
		return taskTimeAfter(taskTime(report.FailureTasks[i]), taskTime(report.FailureTasks[j]))
	})
	sort.Slice(report.SlowTasks, func(i, j int) bool {
		left, right := report.SlowTasks[i], report.SlowTasks[j]
		if left.DurationMS != right.DurationMS {
			return left.DurationMS > right.DurationMS
		}
		return taskTimeAfter(taskTime(left), taskTime(right))
	})
	sort.Slice(report.TraceErrorTasks, func(i, j int) bool {
		left, right := report.TraceErrorTasks[i], report.TraceErrorTasks[j]
		if left.TraceErrors != right.TraceErrors {
			return left.TraceErrors > right.TraceErrors
		}
		return taskTimeAfter(taskTime(left), taskTime(right))
	})
	report.PeriodicTasks = limitPeriodics(report.PeriodicTasks)
	report.Workflows = limitWorkflows(report.Workflows)
	report.FailureTasks = limitTasks(report.FailureTasks)
	report.SlowTasks = limitTasks(report.SlowTasks)
	report.TraceErrorTasks = limitTasks(report.TraceErrorTasks)
}

// periodicLess 定义周期摘要排序：失败数、执行数、最近时间、名称。
func periodicLess(left, right PeriodicSummary) bool {
	if left.Failed != right.Failed {
		return left.Failed > right.Failed
	}
	if left.TaskExecutions != right.TaskExecutions {
		return left.TaskExecutions > right.TaskExecutions
	}
	if left.LastAt != right.LastAt {
		return taskTimeAfter(left.LastAt, right.LastAt)
	}
	return left.Name < right.Name
}

// workflowLess 定义工作流摘要排序：失败数、实例数、最近时间、名称。
func workflowLess(left, right WorkflowSummary) bool {
	if left.Failed != right.Failed {
		return left.Failed > right.Failed
	}
	if left.Total != right.Total {
		return left.Total > right.Total
	}
	if left.LastAt != right.LastAt {
		return taskTimeAfter(left.LastAt, right.LastAt)
	}
	return left.Name < right.Name
}

// limitPeriodics 裁剪周期摘要展示数量。
func limitPeriodics(items []PeriodicSummary) []PeriodicSummary {
	if len(items) <= reportTopLimit {
		return items
	}
	return append([]PeriodicSummary(nil), items[:reportTopLimit]...)
}

// limitWorkflows 裁剪工作流摘要展示数量。
func limitWorkflows(items []WorkflowSummary) []WorkflowSummary {
	if len(items) <= reportTopLimit {
		return items
	}
	return append([]WorkflowSummary(nil), items[:reportTopLimit]...)
}

// limitTasks 裁剪任务明细展示数量。
func limitTasks(items []TaskSummary) []TaskSummary {
	if len(items) <= reportTopLimit {
		return items
	}
	return append([]TaskSummary(nil), items[:reportTopLimit]...)
}

// formatWindowTime 将时间窗口转换为任务列表接口使用的 RFC3339 文本。
func formatWindowTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
