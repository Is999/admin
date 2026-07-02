package taskreport

import (
	"context"
	"sort"
	"strings"
	"time"

	"admin/common/i18n"
	"admin/helper"
	"admin/internal/requestctx"
	"admin/internal/task/taskwire"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// reportPageSize 表示单轮读取 Asynq 任务详情的上限。
	reportPageSize = 100
	// reportDefaultMaxItems 限制日报最多聚合的周期来源任务数，避免 Redis 扫描失控。
	reportDefaultMaxItems = 2000
	// reportTopLimit 控制 Lark 日报中明细列表的最大条数。
	reportTopLimit = 8
	// reportMaxPages 控制日报按队列状态翻页的最大页数。
	reportMaxPages = 50
)

// QueueManager 描述任务运行日报读取任务系统数据所需的最小能力。
type QueueManager interface {
	ListQueues(ctx context.Context) (*types.TaskQueueListResp, error)                                // ListQueues 返回队列概览。
	ListTasks(ctx context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error)         // ListTasks 分页读取任务列表。
	GetWorkflowStatus(ctx context.Context, workflowID string) (*types.TaskWorkflowStatusResp, error) // GetWorkflowStatus 读取工作流状态。
	CompletedRetention() time.Duration                                                               // CompletedRetention 返回 completed 保留期。
}

// ReportRequest 描述一次任务运行日报统计请求。
type ReportRequest struct {
	WindowStart time.Time // 统计窗口开始时间，默认取执行时间往前 24 小时
	WindowEnd   time.Time // 统计窗口结束时间，默认取任务执行时间
	GeneratedAt time.Time // 报告生成时间；为空时使用当前时间
	MaxItems    int       // 最大聚合任务数；<=0 时使用默认上限
}

// Report 汇总一个统计窗口内周期任务和工作流执行情况。
type Report struct {
	WindowStart           time.Time         // 统计窗口开始时间
	WindowEnd             time.Time         // 统计窗口结束时间
	GeneratedAt           time.Time         // 报告生成时间
	TotalTaskExecutions   int               // 周期来源任务执行总数，包含触发任务与节点任务
	SuccessTaskExecutions int               // 成功任务数
	FailedTaskExecutions  int               // 失败任务数
	PeriodicTriggerTotal  int               // 周期触发入口任务总数
	PeriodicTriggerOK     int               // 周期触发入口成功数
	PeriodicTriggerFailed int               // 周期触发入口失败数
	NodeTaskTotal         int               // 周期工作流节点任务总数
	WorkflowTotal         int               // 周期来源工作流实例数
	WorkflowSuccess       int               // 成功工作流实例数
	WorkflowFailed        int               // 失败工作流实例数
	WorkflowRunning       int               // 仍在运行的工作流实例数
	WorkflowUnknown       int               // 状态已过期或无法确认的工作流实例数
	TraceTotalCount       int64             // 任务执行统计中累计处理总量
	TraceReadCount        int64             // 读取数量
	TraceWriteCount       int64             // 写入数量，包含 insert/update/upsert
	TraceDeleteCount      int64             // 删除数量
	TraceErrorCount       int64             // 隔离错误数量
	AverageDurationMS     int64             // 周期来源任务平均耗时
	MaxDurationMS         int64             // 周期来源任务最大耗时
	QueueSummaries        []QueueSummary    // 队列维度摘要
	PeriodicTasks         []PeriodicSummary // 周期任务维度摘要
	Workflows             []WorkflowSummary // 工作流维度摘要
	FailureTasks          []TaskSummary     // 失败任务明细
	SlowTasks             []TaskSummary     // 慢任务明细
	Truncated             bool              // 是否因上限截断了统计明细
	RetentionWarning      string            // 保留时间不足提示
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
	ID           string // Asynq 任务 ID
	Name         string // 任务展示名
	Type         string // 任务类型
	State        string // 任务状态
	Queue        string // 队列
	PeriodicName string // 周期任务名称
	WorkflowID   string // 工作流实例 ID
	WorkflowName string // 工作流名称
	WorkflowNode string // 工作流节点
	StartedAt    string // 开始时间
	FinishedAt   string // 完成或失败时间
	DurationMS   int64  // 耗时毫秒
	Error        string // 错误摘要
}

// workflowStatus 记录单个工作流实例在统计窗口内的最终状态。
type workflowStatus struct {
	Name   string // 工作流展示名称
	Status string // 工作流当前状态
}

// reportWindow 表示日报统计窗口和是否需要时间过滤。
type reportWindow struct {
	start    time.Time // 窗口开始时间
	end      time.Time // 窗口结束时间
	hasRange bool      // 是否启用窗口过滤
}

// periodicAgg 聚合单个周期任务在窗口内的执行次数和耗时。
type periodicAgg struct {
	item       PeriodicSummary // 对外输出的周期任务摘要
	durationMS int64           // 参与平均耗时计算的总毫秒数
	durationN  int64           // 参与平均耗时计算的样本数
}

// workflowAgg 聚合单个工作流在窗口内的实例状态和节点耗时。
type workflowAgg struct {
	item       WorkflowSummary     // 对外输出的工作流摘要
	ids        map[string]struct{} // 已计入的工作流实例 ID，避免重复计数
	statuses   map[string]string   // 工作流实例 ID 到最终状态的映射
	durationMS int64               // 参与平均耗时计算的总毫秒数
	durationN  int64               // 参与平均耗时计算的样本数
}

// Service 构建任务系统运行日报。
type Service struct {
	manager QueueManager // 任务队列数据读取入口
}

// NewService 创建任务运行日报服务。
func NewService(manager QueueManager) *Service {
	return &Service{manager: manager}
}

// Window 返回当前执行时间往前 24 小时的统计窗口。
func Window(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
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
	req = normalizeRequest(req)
	window := reportWindow{start: req.WindowStart, end: req.WindowEnd, hasRange: true}
	queueSummaries, err := s.queueSummaries(ctx)
	if err != nil {
		return Report{}, errors.Tag(err)
	}
	items := make([]types.TaskItem, 0)
	truncated := false
	for _, summary := range queueSummaries {
		for _, state := range []string{"archived", "completed"} {
			if len(items) >= req.MaxItems {
				truncated = true
				break
			}
			stateItems, stateTruncated, err := s.stateItems(ctx, summary.Name, state, window, req.MaxItems-len(items))
			if err != nil {
				return Report{}, errors.Tag(err)
			}
			items = append(items, stateItems...)
			truncated = truncated || stateTruncated
		}
	}
	statuses := s.workflowStatuses(ctx, items)
	warning := s.retentionWarning(ctx, req.WindowStart, req.WindowEnd)
	return buildReport(req, queueSummaries, items, statuses, truncated, warning), nil
}

// normalizeRequest 补齐日报窗口、生成时间和最大聚合数量默认值。
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
	if req.MaxItems <= 0 {
		req.MaxItems = reportDefaultMaxItems
	}
	return req
}

// queueSummaries 读取队列当前积压概览，空队列时补默认展示队列。
func (s *Service) queueSummaries(ctx context.Context) ([]QueueSummary, error) {
	resp, err := s.manager.ListQueues(ctx)
	if err != nil {
		return nil, errors.Tag(err)
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// stateItems 分页读取指定状态下周期来源任务，并按窗口和上限截断。
func (s *Service) stateItems(ctx context.Context, queueName string, state string, window reportWindow, limit int) ([]types.TaskItem, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	result := make([]types.TaskItem, 0)
	truncated := false
	for page := 1; page <= reportMaxPages; page++ {
		resp, err := s.manager.ListTasks(ctx, &types.ListTaskItemsReq{
			Queue:     queueName,
			State:     state,
			Page:      page,
			PageSize:  reportPageSize,
			StartTime: formatWindowTime(window.start),
			EndTime:   formatWindowTime(window.end),
		})
		if err != nil {
			return nil, false, errors.Tag(err)
		}
		if resp == nil || len(resp.Tasks) == 0 {
			break
		}
		for _, item := range resp.Tasks {
			if !taskItemHasPeriodicSource(item) || !reportContains(window, item) {
				continue
			}
			result = append(result, item)
			if len(result) >= limit {
				return result, true, nil
			}
		}
		if len(resp.Tasks) < reportPageSize || int64(page*reportPageSize) >= resp.Total {
			return result, false, nil
		}
		truncated = page == reportMaxPages
	}
	return result, truncated, nil
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

// workflowStatuses 批量读取任务关联工作流状态，读取失败时保留任务里的名称兜底。
func (s *Service) workflowStatuses(ctx context.Context, items []types.TaskItem) map[string]workflowStatus {
	ids := make(map[string]string)
	for _, item := range items {
		workflowID := itemWorkflowID(item)
		if workflowID == "" {
			continue
		}
		ids[workflowID] = itemWorkflowName(item)
	}
	statuses := make(map[string]workflowStatus, len(ids))
	for workflowID, fallbackName := range ids {
		meta, err := s.manager.GetWorkflowStatus(ctx, workflowID)
		if err != nil || meta == nil {
			statuses[workflowID] = workflowStatus{Name: fallbackName}
			continue
		}
		statuses[workflowID] = workflowStatus{
			Name:   helper.FirstNonEmptyString(strings.TrimSpace(meta.WorkflowName), fallbackName),
			Status: strings.TrimSpace(meta.Status),
		}
	}
	return statuses
}

// retentionWarning 在 completed 保留期不足覆盖日报窗口时给出运维提示。
func (s *Service) retentionWarning(ctx context.Context, start, end time.Time) string {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ""
	}
	window := end.Sub(start)
	if s.manager.CompletedRetention() > window {
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
func buildReport(req ReportRequest, queues []QueueSummary, items []types.TaskItem, statuses map[string]workflowStatus, truncated bool, warning string) Report {
	report := Report{
		WindowStart:      req.WindowStart,
		WindowEnd:        req.WindowEnd,
		GeneratedAt:      req.GeneratedAt,
		QueueSummaries:   append([]QueueSummary(nil), queues...),
		Truncated:        truncated,
		RetentionWarning: strings.TrimSpace(warning),
	}
	queueIndex := make(map[string]int, len(report.QueueSummaries))
	for idx := range report.QueueSummaries {
		queueIndex[report.QueueSummaries[idx].Name] = idx
	}
	periodicAggs := make(map[string]*periodicAgg)
	workflowAggs := make(map[string]*workflowAgg)
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

// workflowStatusValue 优先使用工作流快照状态，缺失时按任务终态推断。
func workflowStatusValue(workflowID string, taskState string, statuses map[string]workflowStatus) string {
	if status, ok := statuses[workflowID]; ok {
		switch strings.TrimSpace(status.Status) {
		case "success", "failed", "running", "pending":
			return strings.TrimSpace(status.Status)
		}
	}
	if taskState == asynq.TaskStateArchived.String() {
		return "failed"
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
	report.PeriodicTasks = limitPeriodics(report.PeriodicTasks)
	report.Workflows = limitWorkflows(report.Workflows)
	report.FailureTasks = limitTasks(report.FailureTasks)
	report.SlowTasks = limitTasks(report.SlowTasks)
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
