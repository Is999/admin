package taskqueue

import (
	"context"
	"sort"
	"strings"
	"time"

	"admin/helper"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// taskDailyReportHour 表示日报统计窗口的固定结束小时。
	taskDailyReportHour = 10
	// taskDailyReportPageSize 表示单轮读取 Asynq 任务详情的上限。
	taskDailyReportPageSize = 100
	// taskDailyReportDefaultMaxItems 限制日报最多聚合的周期来源任务数，避免 Redis 扫描失控。
	taskDailyReportDefaultMaxItems = 2000
	// taskDailyReportTopLimit 控制 Lark 日报中明细列表的最大条数。
	taskDailyReportTopLimit = 8
)

// TaskDailyReportRequest 描述一次任务运行日报统计请求。
type TaskDailyReportRequest struct {
	WindowStart time.Time // 统计窗口开始时间，通常为昨日 10:00
	WindowEnd   time.Time // 统计窗口结束时间，通常为今日 10:00
	GeneratedAt time.Time // 报告生成时间；为空时使用当前时间
	MaxItems    int       // 最大聚合任务数；<=0 时使用默认上限
}

// TaskDailyReport 汇总一个统计窗口内周期任务和工作流执行情况。
type TaskDailyReport struct {
	WindowStart           time.Time                     // 统计窗口开始时间
	WindowEnd             time.Time                     // 统计窗口结束时间
	GeneratedAt           time.Time                     // 报告生成时间
	TotalTaskExecutions   int                           // 周期来源任务执行总数，包含触发任务与节点任务
	SuccessTaskExecutions int                           // 成功任务数
	FailedTaskExecutions  int                           // 失败任务数
	PeriodicTriggerTotal  int                           // 周期触发入口任务总数
	PeriodicTriggerOK     int                           // 周期触发入口成功数
	PeriodicTriggerFailed int                           // 周期触发入口失败数
	NodeTaskTotal         int                           // 周期工作流节点任务总数
	WorkflowTotal         int                           // 周期来源工作流实例数
	WorkflowSuccess       int                           // 成功工作流实例数
	WorkflowFailed        int                           // 失败工作流实例数
	WorkflowRunning       int                           // 仍在运行的工作流实例数
	WorkflowUnknown       int                           // 状态已过期或无法确认的工作流实例数
	TraceTotalCount       int64                         // 任务执行统计中累计处理总量
	TraceReadCount        int64                         // 读取数量
	TraceWriteCount       int64                         // 写入数量，包含 insert/update/upsert
	TraceDeleteCount      int64                         // 删除数量
	TraceErrorCount       int64                         // 隔离错误数量
	AverageDurationMS     int64                         // 周期来源任务平均耗时
	MaxDurationMS         int64                         // 周期来源任务最大耗时
	QueueSummaries        []TaskDailyReportQueueSummary // 队列维度摘要
	PeriodicTasks         []TaskDailyReportPeriodic     // 周期任务维度摘要
	Workflows             []TaskDailyReportWorkflow     // 工作流维度摘要
	FailureTasks          []TaskDailyReportTask         // 失败任务明细
	SlowTasks             []TaskDailyReportTask         // 慢任务明细
	Truncated             bool                          // 是否因上限截断了统计明细
	RetentionWarning      string                        // 保留时间不足提示
}

// TaskDailyReportQueueSummary 描述队列在日报窗口内和当前时刻的摘要。
type TaskDailyReportQueueSummary struct {
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

// TaskDailyReportPeriodic 描述单个周期任务的执行摘要。
type TaskDailyReportPeriodic struct {
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

// TaskDailyReportWorkflow 描述单个工作流名称下的实例摘要。
type TaskDailyReportWorkflow struct {
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

// TaskDailyReportTask 描述日报中的任务明细。
type TaskDailyReportTask struct {
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

type taskDailyReportWorkflowStatus struct {
	Name   string
	Status string
}

type taskDailyReportPeriodicAgg struct {
	item       TaskDailyReportPeriodic
	durationMS int64
	durationN  int64
}

type taskDailyReportWorkflowAgg struct {
	item       TaskDailyReportWorkflow
	ids        map[string]struct{}
	statuses   map[string]string
	durationMS int64
	durationN  int64
}

// TaskDailyReportWindow 返回当前时间对应的昨日 10 点到今日 10 点统计窗口。
func TaskDailyReportWindow(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	localNow := now.In(now.Location())
	end := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), taskDailyReportHour, 0, 0, 0, localNow.Location())
	if localNow.Before(end) {
		end = end.AddDate(0, 0, -1)
	}
	return end.Add(-24 * time.Hour), end
}

// BuildTaskDailyReport 汇总指定窗口内由周期调度触发的 completed/archived 任务。
func (m *Manager) BuildTaskDailyReport(ctx context.Context, req TaskDailyReportRequest) (TaskDailyReport, error) {
	if !m.IsEnabled() {
		return TaskDailyReport{}, ErrTaskQueueDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req = normalizeTaskDailyReportRequest(req)
	timeRange := taskListTimeRange{start: req.WindowStart, end: req.WindowEnd, hasRange: true}
	queues := m.dailyReportQueueNames()
	queueSummaries := make([]TaskDailyReportQueueSummary, 0, len(queues))
	items := make([]types.TaskItem, 0)
	truncated := false
	for _, queueName := range queues {
		queueSummaries = append(queueSummaries, m.dailyReportQueueSummary(ctx, queueName))
		for _, state := range []string{"archived", "completed"} {
			if len(items) >= req.MaxItems {
				truncated = true
				break
			}
			stateItems, stateTruncated, err := m.dailyReportStateItems(ctx, queueName, state, timeRange, req.MaxItems-len(items))
			if err != nil {
				return TaskDailyReport{}, errors.Tag(err)
			}
			items = append(items, stateItems...)
			truncated = truncated || stateTruncated
		}
	}
	statuses := m.dailyReportWorkflowStatuses(ctx, items)
	warning := m.taskDailyReportRetentionWarning(req.WindowStart, req.WindowEnd)
	return buildTaskDailyReport(req, queueSummaries, items, statuses, truncated, warning), nil
}

func normalizeTaskDailyReportRequest(req TaskDailyReportRequest) TaskDailyReportRequest {
	now := req.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	if req.WindowStart.IsZero() || req.WindowEnd.IsZero() {
		req.WindowStart, req.WindowEnd = TaskDailyReportWindow(now)
	}
	if req.GeneratedAt.IsZero() {
		req.GeneratedAt = now
	}
	if req.MaxItems <= 0 {
		req.MaxItems = taskDailyReportDefaultMaxItems
	}
	return req
}

func (m *Manager) dailyReportQueueNames() []string {
	rawQueueNames, err := m.inspector.Queues()
	if err == nil {
		if queues := normalizeDailyReportQueueNames(m.visibleQueueNames(rawQueueNames)); len(queues) > 0 {
			return queues
		}
	}
	if queues := normalizeDailyReportQueueNames(m.configuredQueueNames()); len(queues) > 0 {
		return queues
	}
	return []string{QueueCritical, QueueDefault, QueueMaintenance}
}

func normalizeDailyReportQueueNames(queueNames []string) []string {
	seen := make(map[string]struct{}, len(queueNames))
	result := make([]string, 0, len(queueNames))
	for _, queueName := range queueNames {
		queueName = strings.TrimSpace(queueName)
		if queueName == "" {
			continue
		}
		if _, ok := seen[queueName]; ok {
			continue
		}
		seen[queueName] = struct{}{}
		result = append(result, queueName)
	}
	sort.Strings(result)
	return result
}

func (m *Manager) dailyReportQueueSummary(ctx context.Context, queueName string) TaskDailyReportQueueSummary {
	summary := TaskDailyReportQueueSummary{Name: strings.TrimSpace(queueName)}
	if summary.Name == "" {
		return summary
	}
	info, err := m.inspector.GetQueueInfo(m.namespacedQueueName(summary.Name))
	if err != nil || info == nil {
		return summary
	}
	summary.Pending = info.Pending
	summary.Active = info.Active
	summary.Scheduled = info.Scheduled
	summary.Retry = info.Retry
	summary.Archived = info.Archived
	summary.Completed = info.Completed
	summary.Aggregating = info.Aggregating
	return summary
}

func (m *Manager) dailyReportStateItems(ctx context.Context, queueName string, state string, timeRange taskListTimeRange, limit int) ([]types.TaskItem, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	internalQueue := m.namespacedQueueName(queueName)
	result := make([]types.TaskItem, 0)
	truncated := false
	for page := 1; page <= taskListFilterMaxPages; page++ {
		infos, total, err := m.listTaskInfoPage(ctx, internalQueue, "", state, page, taskDailyReportPageSize, timeRange)
		if err != nil {
			return nil, false, errors.Tag(err)
		}
		if len(infos) == 0 {
			break
		}
		for _, info := range infos {
			item := m.toTaskItem(ctx, info)
			if !taskItemHasPeriodicSource(item) || !taskDailyReportContains(timeRange, item) {
				continue
			}
			result = append(result, item)
			if len(result) >= limit {
				return result, true, nil
			}
		}
		if len(infos) < taskDailyReportPageSize || int64(page*taskDailyReportPageSize) >= total {
			return result, false, nil
		}
		truncated = page == taskListFilterMaxPages
	}
	return result, truncated, nil
}

// taskDailyReportContains 使用左闭右开窗口，避免 10 点边界任务被相邻两天日报重复统计。
func taskDailyReportContains(timeRange taskListTimeRange, item types.TaskItem) bool {
	if !timeRange.hasRange {
		return true
	}
	itemTime, ok := taskItemPrimaryTime(item)
	if !ok {
		return false
	}
	if !timeRange.start.IsZero() && itemTime.Before(timeRange.start) {
		return false
	}
	if !timeRange.end.IsZero() && !itemTime.Before(timeRange.end) {
		return false
	}
	return true
}

func (m *Manager) dailyReportWorkflowStatuses(ctx context.Context, items []types.TaskItem) map[string]taskDailyReportWorkflowStatus {
	ids := make(map[string]string)
	for _, item := range items {
		workflowID := dailyReportWorkflowID(item)
		if workflowID == "" {
			continue
		}
		ids[workflowID] = dailyReportWorkflowName(item)
	}
	statuses := make(map[string]taskDailyReportWorkflowStatus, len(ids))
	for workflowID, fallbackName := range ids {
		meta, _, err := m.readWorkflowStatus(ctx, workflowID)
		if err != nil || meta == nil {
			statuses[workflowID] = taskDailyReportWorkflowStatus{Name: fallbackName}
			continue
		}
		statuses[workflowID] = taskDailyReportWorkflowStatus{
			Name:   helper.FirstNonEmptyString(strings.TrimSpace(meta.WorkflowName), fallbackName),
			Status: strings.TrimSpace(meta.Status),
		}
	}
	return statuses
}

func (m *Manager) taskDailyReportRetentionWarning(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ""
	}
	window := end.Sub(start)
	if m.completedRetention() > window {
		return ""
	}
	return "completed_retention_seconds 不大于统计窗口，成功任务可能在日报边界被 Asynq 清理；建议设置为 90000 秒以上。"
}

func buildTaskDailyReport(req TaskDailyReportRequest, queues []TaskDailyReportQueueSummary, items []types.TaskItem, statuses map[string]taskDailyReportWorkflowStatus, truncated bool, warning string) TaskDailyReport {
	report := TaskDailyReport{
		WindowStart:      req.WindowStart,
		WindowEnd:        req.WindowEnd,
		GeneratedAt:      req.GeneratedAt,
		QueueSummaries:   append([]TaskDailyReportQueueSummary(nil), queues...),
		Truncated:        truncated,
		RetentionWarning: strings.TrimSpace(warning),
	}
	queueIndex := make(map[string]int, len(report.QueueSummaries))
	for idx := range report.QueueSummaries {
		queueIndex[report.QueueSummaries[idx].Name] = idx
	}
	periodicAggs := make(map[string]*taskDailyReportPeriodicAgg)
	workflowAggs := make(map[string]*taskDailyReportWorkflowAgg)
	workflowIDs := make(map[string]string)
	var totalDurationMS int64
	var totalDurationN int64
	for _, item := range items {
		if !taskItemHasPeriodicSource(item) {
			continue
		}
		task := dailyReportTaskFromItem(item)
		report.TotalTaskExecutions++
		if task.State == asynq.TaskStateArchived.String() {
			report.FailedTaskExecutions++
			report.FailureTasks = append(report.FailureTasks, task)
		} else {
			report.SuccessTaskExecutions++
		}
		if item.TaskType == TypeWorkflowTrigger {
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
		queuePos := ensureDailyReportQueue(&report, queueIndex, task.Queue)
		addDailyReportQueueTask(&report.QueueSummaries[queuePos], item)
		addDailyReportPeriodic(periodicAggs, task, item)
		addDailyReportWorkflow(workflowAggs, workflowIDs, task, item, statuses)
	}
	if totalDurationN > 0 {
		report.AverageDurationMS = totalDurationMS / totalDurationN
	}
	finalizeDailyReportWorkflows(workflowAggs)
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
	sortDailyReport(&report)
	return report
}

func ensureDailyReportQueue(report *TaskDailyReport, queueIndex map[string]int, queue string) int {
	queue = strings.TrimSpace(queue)
	if queue == "" {
		queue = "unknown"
	}
	if idx, ok := queueIndex[queue]; ok {
		return idx
	}
	report.QueueSummaries = append(report.QueueSummaries, TaskDailyReportQueueSummary{Name: queue})
	idx := len(report.QueueSummaries) - 1
	queueIndex[queue] = idx
	return idx
}

func addDailyReportQueueTask(summary *TaskDailyReportQueueSummary, item types.TaskItem) {
	if summary == nil {
		return
	}
	summary.TaskExecutions++
	if item.State == asynq.TaskStateArchived.String() {
		summary.Failed++
	} else {
		summary.Success++
	}
	if item.TaskType == TypeWorkflowTrigger {
		summary.Triggers++
		return
	}
	summary.NodeTasks++
}

func addDailyReportPeriodic(aggs map[string]*taskDailyReportPeriodicAgg, task TaskDailyReportTask, item types.TaskItem) {
	name := helper.FirstNonEmptyString(task.PeriodicName, task.WorkflowName, task.Name, "unknown")
	agg := aggs[name]
	if agg == nil {
		agg = &taskDailyReportPeriodicAgg{item: TaskDailyReportPeriodic{Name: name}}
		aggs[name] = agg
	}
	agg.item.TaskExecutions++
	agg.item.WorkflowName = helper.FirstNonEmptyString(agg.item.WorkflowName, task.WorkflowName)
	agg.item.Queue = helper.FirstNonEmptyString(agg.item.Queue, task.Queue)
	if item.TaskType == TypeWorkflowTrigger {
		agg.item.Triggers++
	} else {
		agg.item.NodeTasks++
	}
	if item.State == asynq.TaskStateArchived.String() {
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
	agg.item.LastAt = laterRFC3339(agg.item.LastAt, dailyReportTaskTime(task))
}

func addDailyReportWorkflow(aggs map[string]*taskDailyReportWorkflowAgg, ids map[string]string, task TaskDailyReportTask, item types.TaskItem, statuses map[string]taskDailyReportWorkflowStatus) {
	workflowName := task.WorkflowName
	workflowID := task.WorkflowID
	if workflowID != "" {
		if status, ok := statuses[workflowID]; ok {
			workflowName = helper.FirstNonEmptyString(status.Name, workflowName)
		}
	}
	if workflowName == "" {
		return
	}
	agg := aggs[workflowName]
	if agg == nil {
		agg = &taskDailyReportWorkflowAgg{
			item:     TaskDailyReportWorkflow{Name: workflowName},
			ids:      make(map[string]struct{}),
			statuses: make(map[string]string),
		}
		aggs[workflowName] = agg
	}
	agg.item.Periodic = helper.FirstNonEmptyString(agg.item.Periodic, task.PeriodicName)
	agg.item.Queue = helper.FirstNonEmptyString(agg.item.Queue, task.Queue)
	if workflowID != "" {
		ids[workflowID] = workflowName
		if _, ok := agg.ids[workflowID]; !ok {
			agg.ids[workflowID] = struct{}{}
			agg.item.Total++
		}
		status := dailyReportWorkflowStatus(workflowID, item.State, statuses)
		agg.statuses[workflowID] = strongerDailyReportWorkflowStatus(agg.statuses[workflowID], status)
	}
	if item.TaskType != TypeWorkflowTrigger {
		agg.item.NodeTasks++
		if task.DurationMS > 0 {
			agg.durationMS += task.DurationMS
			agg.durationN++
			if task.DurationMS > agg.item.MaxMS {
				agg.item.MaxMS = task.DurationMS
			}
		}
	}
	agg.item.LastAt = laterRFC3339(agg.item.LastAt, dailyReportTaskTime(task))
}

func dailyReportWorkflowStatus(workflowID string, taskState string, statuses map[string]taskDailyReportWorkflowStatus) string {
	if status, ok := statuses[workflowID]; ok && strings.TrimSpace(status.Status) != "" {
		return strings.TrimSpace(status.Status)
	}
	if taskState == asynq.TaskStateArchived.String() {
		return WorkflowStatusFailed
	}
	if taskState == asynq.TaskStateCompleted.String() {
		return WorkflowStatusSuccess
	}
	return ""
}

func strongerDailyReportWorkflowStatus(current string, next string) string {
	if dailyReportWorkflowStatusRank(next) > dailyReportWorkflowStatusRank(current) {
		return next
	}
	return current
}

func dailyReportWorkflowStatusRank(status string) int {
	switch status {
	case WorkflowStatusFailed:
		return 4
	case WorkflowStatusRunning, WorkflowStatusPending:
		return 3
	case WorkflowStatusSuccess:
		return 2
	case "":
		return 0
	default:
		return 1
	}
}

func finalizeDailyReportWorkflows(aggs map[string]*taskDailyReportWorkflowAgg) {
	for _, agg := range aggs {
		if agg.durationN > 0 {
			agg.item.AverageMS = agg.durationMS / agg.durationN
		}
		for _, status := range agg.statuses {
			switch status {
			case WorkflowStatusSuccess:
				agg.item.Success++
			case WorkflowStatusFailed:
				agg.item.Failed++
			case WorkflowStatusRunning, WorkflowStatusPending:
				agg.item.Running++
			default:
				agg.item.Unknown++
			}
		}
	}
}

func addTraceCounts(report *TaskDailyReport, item types.TaskItem) {
	if report == nil || item.ExecutionTrace == nil || item.ExecutionTrace.Empty() {
		return
	}
	report.TraceTotalCount += item.ExecutionTrace.TotalCount
	report.TraceReadCount += item.ExecutionTrace.ReadCount
	report.TraceWriteCount += item.ExecutionTrace.InsertCount + item.ExecutionTrace.UpdateCount + item.ExecutionTrace.UpsertCount
	report.TraceDeleteCount += item.ExecutionTrace.DeleteCount
	report.TraceErrorCount += item.ExecutionTrace.ErrorCount
}

func dailyReportTaskFromItem(item types.TaskItem) TaskDailyReportTask {
	return TaskDailyReportTask{
		ID:           strings.TrimSpace(item.ID),
		Name:         strings.TrimSpace(item.TaskName),
		Type:         strings.TrimSpace(item.TaskType),
		State:        strings.TrimSpace(item.State),
		Queue:        strings.TrimSpace(item.Queue),
		PeriodicName: taskItemPeriodicName(item),
		WorkflowID:   dailyReportWorkflowID(item),
		WorkflowName: dailyReportWorkflowName(item),
		WorkflowNode: dailyReportWorkflowNode(item),
		StartedAt:    strings.TrimSpace(item.StartedAt),
		FinishedAt:   taskItemFinishedAt(item),
		DurationMS:   item.DurationMS,
		Error:        strings.TrimSpace(item.LastErr),
	}
}

func dailyReportWorkflowID(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.WorkflowID),
		strings.TrimSpace(item.Headers[headerWorkflowID]),
		taskItemJSONField(item.Payload, taskSearchFieldWorkflowID),
		taskItemJSONField(item.Result, taskSearchFieldWorkflowID),
	)
}

func dailyReportWorkflowName(item types.TaskItem) string {
	return taskItemWorkflowName(item)
}

func dailyReportWorkflowNode(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[headerWorkflowNode]),
		taskItemJSONField(item.Payload, taskSearchFieldWorkflowNode),
		taskItemJSONField(item.Result, taskSearchFieldWorkflowNode),
	)
}

func taskItemFinishedAt(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.CompletedAt),
		strings.TrimSpace(item.LastFailedAt),
	)
}

func dailyReportTaskTime(task TaskDailyReportTask) string {
	return helper.FirstNonEmptyString(task.FinishedAt, task.StartedAt)
}

func laterRFC3339(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	leftAt, leftErr := time.Parse(time.RFC3339, left)
	rightAt, rightErr := time.Parse(time.RFC3339, right)
	if leftErr != nil || rightErr != nil {
		if right > left {
			return right
		}
		return left
	}
	if rightAt.After(leftAt) {
		return right
	}
	return left
}

func sortDailyReport(report *TaskDailyReport) {
	sort.Slice(report.QueueSummaries, func(i, j int) bool {
		return report.QueueSummaries[i].Name < report.QueueSummaries[j].Name
	})
	sort.Slice(report.PeriodicTasks, func(i, j int) bool {
		return dailyReportPeriodicLess(report.PeriodicTasks[i], report.PeriodicTasks[j])
	})
	sort.Slice(report.Workflows, func(i, j int) bool {
		return dailyReportWorkflowLess(report.Workflows[i], report.Workflows[j])
	})
	sort.Slice(report.FailureTasks, func(i, j int) bool {
		return dailyReportTaskTimeAfter(report.FailureTasks[i], report.FailureTasks[j])
	})
	sort.Slice(report.SlowTasks, func(i, j int) bool {
		if report.SlowTasks[i].DurationMS == report.SlowTasks[j].DurationMS {
			return report.SlowTasks[i].Name < report.SlowTasks[j].Name
		}
		return report.SlowTasks[i].DurationMS > report.SlowTasks[j].DurationMS
	})
	report.PeriodicTasks = limitDailyReportPeriodics(report.PeriodicTasks)
	report.Workflows = limitDailyReportWorkflows(report.Workflows)
	report.FailureTasks = limitDailyReportTasks(report.FailureTasks)
	report.SlowTasks = limitDailyReportTasks(report.SlowTasks)
}

func dailyReportTaskTimeAfter(left, right TaskDailyReportTask) bool {
	leftTime := dailyReportTaskTime(left)
	rightTime := dailyReportTaskTime(right)
	if leftTime == rightTime {
		return left.Name < right.Name
	}
	return laterRFC3339(leftTime, rightTime) == leftTime
}

func dailyReportPeriodicLess(left, right TaskDailyReportPeriodic) bool {
	if left.Failed != right.Failed {
		return left.Failed > right.Failed
	}
	if left.Triggers != right.Triggers {
		return left.Triggers > right.Triggers
	}
	if left.TaskExecutions != right.TaskExecutions {
		return left.TaskExecutions > right.TaskExecutions
	}
	return left.Name < right.Name
}

func dailyReportWorkflowLess(left, right TaskDailyReportWorkflow) bool {
	if left.Failed != right.Failed {
		return left.Failed > right.Failed
	}
	if left.Total != right.Total {
		return left.Total > right.Total
	}
	if left.NodeTasks != right.NodeTasks {
		return left.NodeTasks > right.NodeTasks
	}
	return left.Name < right.Name
}

func limitDailyReportPeriodics(items []TaskDailyReportPeriodic) []TaskDailyReportPeriodic {
	if len(items) <= taskDailyReportTopLimit {
		return items
	}
	return append([]TaskDailyReportPeriodic(nil), items[:taskDailyReportTopLimit]...)
}

func limitDailyReportWorkflows(items []TaskDailyReportWorkflow) []TaskDailyReportWorkflow {
	if len(items) <= taskDailyReportTopLimit {
		return items
	}
	return append([]TaskDailyReportWorkflow(nil), items[:taskDailyReportTopLimit]...)
}

func limitDailyReportTasks(items []TaskDailyReportTask) []TaskDailyReportTask {
	if len(items) <= taskDailyReportTopLimit {
		return items
	}
	return append([]TaskDailyReportTask(nil), items[:taskDailyReportTopLimit]...)
}
