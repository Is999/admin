package taskqueue

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/helper"
	"admin/internal/types"

	"github.com/hibiken/asynq"
)

// periodicNextRun 表示一个有效周期任务的下一次触发摘要。
type periodicNextRun struct {
	PeriodicName  string // 周期任务展示名，对应 x-app-periodic-name
	WorkflowName  string // 关联工作流名称
	NextProcessAt string // 下一次触发时间，RFC3339
}

// taskListTimeRange 表示任务列表时间过滤条件。
type taskListTimeRange struct {
	start    time.Time // 开始时间，零值表示不限制
	end      time.Time // 结束时间，零值表示不限制
	hasRange bool      // 是否启用时间过滤
}

// parseTaskListTimeRange 解析任务列表时间范围。
func parseTaskListTimeRange(start, end string) (taskListTimeRange, error) {
	var result taskListTimeRange
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start != "" {
		parsed, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return result, errors.Wrap(err, "解析任务列表开始时间失败")
		}
		result.start = parsed
		result.hasRange = true
	}
	if end != "" {
		parsed, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return result, errors.Wrap(err, "解析任务列表结束时间失败")
		}
		result.end = parsed
		result.hasRange = true
	}
	if !result.start.IsZero() && !result.end.IsZero() && result.start.After(result.end) {
		return result, errors.Errorf("任务列表开始时间不能晚于结束时间")
	}
	return result, nil
}

// Contains 判断任务是否命中时间范围；未启用时间范围时全部放行。
func (r taskListTimeRange) Contains(item types.TaskItem) bool {
	if !r.hasRange {
		return true
	}
	itemTime, ok := taskItemPrimaryTime(item)
	if !ok {
		return false
	}
	if !r.start.IsZero() && itemTime.Before(r.start) {
		return false
	}
	if !r.end.IsZero() && itemTime.After(r.end) {
		return false
	}
	return true
}

// sortTaskItemsByTimeDesc 按任务活动时间倒序排序，时间相同再按任务 ID 降序稳定展示。
func sortTaskItemsByTimeDesc(items []types.TaskItem) {
	slices.SortFunc(items, func(left, right types.TaskItem) int {
		leftTime, _ := taskItemPrimaryTime(left)
		rightTime, _ := taskItemPrimaryTime(right)
		switch {
		case leftTime.After(rightTime):
			return -1
		case leftTime.Before(rightTime):
			return 1
		default:
			return strings.Compare(right.ID, left.ID)
		}
	})
}

// taskItemPrimaryTime 返回任务列表排序和时间过滤使用的活动时间。
func taskItemPrimaryTime(item types.TaskItem) (time.Time, bool) {
	candidates := taskItemPrimaryTimeCandidates(item)
	for _, rawValue := range candidates {
		rawValue = strings.TrimSpace(rawValue)
		if rawValue == "" {
			continue
		}
		parsedValue, err := time.Parse(time.RFC3339, rawValue)
		if err == nil {
			return parsedValue, true
		}
	}
	return time.Time{}, false
}

// taskItemPrimaryTimeCandidates 按任务生命周期收集活动时间候选字段。
func taskItemPrimaryTimeCandidates(item types.TaskItem) []string {
	switch strings.TrimSpace(strings.ToLower(item.State)) {
	case "scheduled":
		return []string{item.NextProcessAt, item.Deadline}
	case "pending", "aggregating":
		return []string{item.NextProcessAt, item.Deadline}
	default:
		return []string{item.CompletedAt, item.LastFailedAt, item.StartedAt, item.NextProcessAt, item.Deadline}
	}
}

// taskItemMatchesListFilters 判断任务是否命中任务列表的链路筛选条件。
// taskName 覆盖展示名、任务类型、工作流元数据、任务头和载荷摘要。
func taskItemMatchesListFilters(item types.TaskItem, workflowID string, taskName string) bool {
	workflowID = strings.TrimSpace(workflowID)
	taskName = strings.TrimSpace(taskName)
	if workflowID != "" && !taskItemMatchesWorkflowID(item, workflowID) {
		return false
	}
	if taskName == "" {
		return true
	}
	needle := strings.ToLower(taskName)
	for _, candidate := range taskItemSearchCandidates(item) {
		if strings.Contains(strings.ToLower(strings.TrimSpace(candidate)), needle) {
			return true
		}
	}
	return false
}

// taskItemMatchesWorkflowID 判断任务是否属于指定工作流实例。
// 工作流 ID 可能来自标准字段、任务头或旧任务 payload；这里逐层兜底，避免历史任务无法通过链路筛选定位。
func taskItemMatchesWorkflowID(item types.TaskItem, workflowID string) bool {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return true
	}
	for _, candidate := range taskItemWorkflowIDCandidates(item) {
		if strings.TrimSpace(candidate) == workflowID {
			return true
		}
	}
	return false
}

// taskItemWorkflowIDCandidates 收集工作流 ID 候选值。
// 候选值只包含精确 ID 字段，不包含任意 header 全量扫描，避免误把其它值撞成同一 workflowId。
func taskItemWorkflowIDCandidates(item types.TaskItem) []string {
	candidates := []string{
		item.WorkflowID,
		item.Headers[headerWorkflowID],
	}
	appendTaskSearchJSONFields(&candidates, item.Payload, taskSearchFieldWorkflowID)
	appendTaskSearchJSONFields(&candidates, item.Result, taskSearchFieldWorkflowID)
	return candidates
}

// taskItemSearchCandidates 收集任务名称检索候选值。
// 这里保持只读转换，不访问 Redis/MySQL，避免带 taskName 筛选的受控扫描退化为更重的外部查询。
func taskItemSearchCandidates(item types.TaskItem) []string {
	candidates := []string{
		item.ID,
		item.TaskName,
		item.TaskType,
		item.WorkflowID,
		item.Group,
		item.Headers[headerTaskName],
		item.Headers[headerTaskSource],
		item.Headers[HeaderPeriodicName],
		item.Headers[headerWorkflowID],
		item.Headers[headerWorkflowName],
		item.Headers[headerWorkflowNode],
	}
	appendTaskSearchJSONFields(&candidates, item.Payload,
		taskSearchFieldTaskName,
		taskSearchFieldTaskType,
		taskSearchFieldWorkflowID,
		taskSearchFieldWorkflowName,
		taskSearchFieldWorkflowNode,
		taskSearchFieldTaskSource,
		"periodicName",
		taskSearchFieldMode,
		taskSearchFieldUniqueKey,
	)
	appendTaskSearchJSONFields(&candidates, item.Result,
		taskSearchFieldTaskName,
		taskSearchFieldTaskType,
		taskSearchFieldWorkflowID,
		taskSearchFieldWorkflowName,
		taskSearchFieldWorkflowNode,
		taskSearchFieldTaskSource,
		"periodicName",
		taskSearchFieldMode,
		taskSearchFieldUniqueKey,
	)
	return candidates
}

// appendTaskSearchJSONFields 从 JSON 对象中追加指定字段值。
// payload/result 字段不存在或 JSON 非对象时直接跳过。
func appendTaskSearchJSONFields(candidates *[]string, raw json.RawMessage, fieldNames ...string) {
	if candidates == nil || len(raw) == 0 || len(fieldNames) == 0 {
		return
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil || len(payloadMap) == 0 {
		return
	}
	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}
		value := strings.TrimSpace(anyToString(payloadMap[fieldName]))
		if value == "" {
			continue
		}
		*candidates = append(*candidates, value)
	}
}

// periodicNextRuns 计算当前有效周期任务的下一次触发时间，用于列表补齐 completed/archived 的 nextProcessAt。
func (m *Manager) periodicNextRuns(now time.Time) []periodicNextRun {
	if m == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	items := m.allPeriodicTasks()
	result := make([]periodicNextRun, 0, len(items))
	for _, item := range items {
		normalized, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr != nil {
			continue
		}
		cronspec, err := periodicTaskCronspec(normalized)
		if err != nil {
			continue
		}
		schedule, err := periodicCronParser.Parse(cronspec)
		if err != nil {
			continue
		}
		nextAt := schedule.Next(now)
		if nextAt.IsZero() {
			continue
		}
		result = append(result, periodicNextRun{
			PeriodicName:  periodicTaskDisplayName(normalized),
			WorkflowName:  strings.TrimSpace(normalized.Workflow),
			NextProcessAt: nextAt.Format(time.RFC3339),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NextProcessAt == result[j].NextProcessAt {
			return result[i].PeriodicName < result[j].PeriodicName
		}
		return result[i].NextProcessAt < result[j].NextProcessAt
	})
	return result
}

// periodicNextProcessAtForTaskItem 从周期任务配置中为历史任务补齐下一次触发时间。
func periodicNextProcessAtForTaskItem(item types.TaskItem, periodicNextRuns []periodicNextRun) string {
	if len(periodicNextRuns) == 0 || !taskItemHasPeriodicSource(item) {
		return ""
	}
	periodicName := taskItemPeriodicName(item)
	if periodicName != "" {
		for _, nextRun := range periodicNextRuns {
			if nextRun.PeriodicName == periodicName {
				return nextRun.NextProcessAt
			}
		}
	}
	workflowName := taskItemWorkflowName(item)
	if workflowName == "" {
		return ""
	}
	for _, nextRun := range periodicNextRuns {
		if nextRun.WorkflowName == workflowName {
			return nextRun.NextProcessAt
		}
	}
	return ""
}

// taskItemHasPeriodicSource 判断任务是否由周期调度触发。
func taskItemHasPeriodicSource(item types.TaskItem) bool {
	if taskItemPeriodicName(item) != "" {
		return true
	}
	source := helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[headerTaskSource]),
		taskItemJSONField(item.Payload, "source"),
		taskItemJSONField(item.Payload, taskSearchFieldTaskSource),
		taskItemJSONField(item.Result, taskSearchFieldTaskSource),
	)
	return source == WorkflowSourcePeriodic
}

// taskItemPeriodicName 从任务头或 JSON 字段中读取周期任务名称。
func taskItemPeriodicName(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[HeaderPeriodicName]),
		taskItemJSONField(item.Payload, "periodicName"),
		taskItemJSONField(item.Result, "periodicName"),
	)
}

// taskItemWorkflowName 从任务头或 JSON 字段中读取工作流名称。
func taskItemWorkflowName(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[headerWorkflowName]),
		taskItemJSONField(item.Payload, taskSearchFieldWorkflowName),
		taskItemJSONField(item.Result, taskSearchFieldWorkflowName),
	)
}

// taskItemJSONField 从任务 JSON 对象读取首层字符串字段。
func taskItemJSONField(raw json.RawMessage, fieldName string) string {
	fieldName = strings.TrimSpace(fieldName)
	if len(raw) == 0 || fieldName == "" {
		return ""
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil || len(payloadMap) == 0 {
		return ""
	}
	return strings.TrimSpace(anyToString(payloadMap[fieldName]))
}

// toTaskItem 把 Asynq 任务详情转换成对外返回结构。
// 返回前把内部队列名和聚合分组还原成逻辑名称。
func (m *Manager) toTaskItem(ctx context.Context, info *asynq.TaskInfo) types.TaskItem {
	return m.toTaskItemWithPeriodicNextRuns(ctx, info, nil)
}

// toTaskItemWithPeriodicNextRuns 在任务基础字段之外补齐周期任务下一次触发时间。
func (m *Manager) toTaskItemWithPeriodicNextRuns(ctx context.Context, info *asynq.TaskInfo, periodicNextRuns []periodicNextRun) types.TaskItem {
	if info == nil {
		return types.TaskItem{}
	}
	runtimeRecord := m.readTaskRuntime(ctx, info.ID)
	startedAt := runtimeRecord.StartedAt
	if startedAt == "" {
		startedAt = taskStartedAtFromResult(info.Result)
	}
	durationMS := runtimeRecord.DurationMS
	if durationMS <= 0 && startedAt != "" && info.State == asynq.TaskStateActive {
		if parsedStartedAt, err := time.Parse(time.RFC3339, startedAt); err == nil {
			durationMS = time.Since(parsedStartedAt).Milliseconds()
		}
	}
	if durationMS <= 0 {
		durationMS = taskDurationFromResult(info.Result)
	}
	executionTrace := runtimeRecord.ExecutionTrace
	if executionTrace == nil {
		executionTrace = taskExecutionStatsFromResult(info.Result)
	}
	item := types.TaskItem{
		ID:             info.ID,
		Queue:          m.displayQueueName(info.Queue),
		TaskType:       info.Type,
		TaskName:       taskInfoDisplayName(info),
		WorkflowID:     strings.TrimSpace(info.Headers[headerWorkflowID]),
		State:          info.State.String(),
		Group:          m.displayGroupName(info.Group),
		MaxRetry:       info.MaxRetry,
		Retried:        info.Retried,
		LastErr:        info.LastErr,
		TimeoutSec:     int64(info.Timeout.Seconds()),
		StartedAt:      startedAt,
		DurationMS:     durationMS,
		Deadline:       formatTime(info.Deadline),
		NextProcessAt:  formatTime(info.NextProcessAt),
		CompletedAt:    formatTime(info.CompletedAt),
		LastFailedAt:   formatTime(info.LastFailedAt),
		IsOrphaned:     info.IsOrphaned,
		Headers:        info.Headers,
		Payload:        append([]byte(nil), info.Payload...),
		Result:         append([]byte(nil), info.Result...),
		ExecutionTrace: executionTrace,
	}
	if item.NextProcessAt == "" {
		item.NextProcessAt = periodicNextProcessAtForTaskItem(item, periodicNextRuns)
	}
	return item
}

// queueInfoTotalByState 根据任务状态返回队列中对应的统计总数。
func queueInfoTotalByState(info *asynq.QueueInfo, state string) int64 {
	switch state {
	case "pending":
		return int64(info.Pending)
	case "active":
		return int64(info.Active)
	case "scheduled":
		return int64(info.Scheduled)
	case "retry":
		return int64(info.Retry)
	case "archived":
		return int64(info.Archived)
	case "completed":
		return int64(info.Completed)
	case "aggregating":
		return int64(info.Aggregating)
	default:
		return int64(info.Size)
	}
}

// formatTime 把零值时间转换为空串，其他时间统一格式化为 RFC3339。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
