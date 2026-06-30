package taskqueue

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/jobs/archive"
	"admin/internal/jobs/taskreport"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"

	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// taskTypeDescription 返回任务类型中文说明。
func taskTypeDescription(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return "工作流触发入口任务，由总控台触发工作流时自动入队"
	case taskType == TypeWorkflowNoop:
		return "工作流空节点任务，仅用于 DAG 收尾和依赖串联"
	case taskType == TypeCacheRefreshRequest:
		return "缓存刷新请求任务，用于把短时间内多次刷新请求聚合"
	case taskType == TypeCacheRefreshBatch:
		return "缓存批量刷新任务，真正执行缓存刷新逻辑"
	case taskType == archive.TaskTypeExecute:
		return "通用归档执行任务，负责按区间批量写历史表、校验并删除热表"
	case taskType == taskreport.TaskTypeDailySummary:
		return "周期任务与工作流运行日报任务，汇总昨日 10 点到今日 10 点的执行情况并发送 Lark 通知"
	case taskType == "admin:export":
		return "管理员列表异步导出任务，生成 Excel 并回写进度"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "用户标签工作流节点任务，用于用户标签重建、补算与同步"
	default:
		return "已注册任务处理器"
	}
}

// taskTypeDisplayName 返回任务类型的人类可读名称，优先用于日志和管理后台展示。
func taskTypeDisplayName(taskType string) string {
	description := strings.TrimSpace(taskTypeDescription(strings.TrimSpace(taskType)))
	if description == "" || description == "已注册任务处理器" {
		return strings.TrimSpace(taskType)
	}
	return description
}

// taskTypeUsageHint 返回任务类型使用提示。
func taskTypeUsageHint(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return "通常不建议直接手动投递该类型；优先使用“手动触发工作流”表单。"
	case taskType == TypeWorkflowNoop:
		return "仅工作流内部使用，人工投递没有业务意义。"
	case taskType == TypeCacheRefreshRequest:
		return "适合少量缓存目标补刷；payload.targets 填缓存目标数组。"
	case taskType == TypeCacheRefreshBatch:
		return "该任务通常由聚合器自动生成，不建议人工直接投递。"
	case taskType == archive.TaskTypeExecute:
		return "建议通过归档工作流统一触发；单独投递时只适合排障或补跑。"
	case taskType == taskreport.TaskTypeDailySummary:
		return "建议由每天 10 点的 task_report.daily_summary 周期任务触发；手工投递可传 windowStart/windowEnd RFC3339 覆盖统计窗口。"
	case taskType == "admin:export":
		return "建议优先使用管理员列表页导出入口；手工投递时需提供 jobId、operatorId 和 request。"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "该类型通常由用户标签工作流节点内部调度，不建议脱离工作流单独投递。"
	default:
		return "投递前请确认后端已为该任务类型注册合法 payload 解析逻辑。"
	}
}

// taskTypePayloadExample 返回任务类型推荐负载示例。
func taskTypePayloadExample(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return fmt.Sprintf("{\n  \"workflowId\": \"wf-demo-001\",\n  \"workflowName\": \"cache.refresh\",\n  \"targets\": [\"%s\"]\n}", fmt.Sprintf(keys.AdminProfile, 1))
	case taskType == TypeWorkflowNoop:
		return "{\n  \"note\": \"finalize\"\n}"
	case taskType == TypeCacheRefreshRequest, taskType == TypeCacheRefreshBatch:
		return fmt.Sprintf("{\n  \"operation\": \"manual_refresh\",\n  \"targets\": [\"%s\", \"%s\"]\n}",
			fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1))
	case taskType == archive.TaskTypeExecute:
		return "{\n  \"targets\": [\"admin_log\"]\n}"
	case taskType == taskreport.TaskTypeDailySummary:
		return "{\n  \"windowStart\": \"2026-06-29T10:00:00+08:00\",\n  \"windowEnd\": \"2026-06-30T10:00:00+08:00\"\n}"
	case taskType == "admin:export":
		return "{\n  \"jobId\": \"job-demo-001\",\n  \"operatorId\": 1,\n  \"operatorName\": \"admin\",\n  \"request\": {\n    \"username\": \"demo\"\n  }\n}"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "{\n  \"mode\": \"targeted\",\n  \"targets\": [\"uid=10001\"],\n  \"uids\": [10001],\n  \"tagTypes\": [2, 3]\n}"
	default:
		return "{\n  \"demo\": true\n}"
	}
}

// taskTypeManualRecommended 返回当前任务类型是否适合在总控台人工直接投递。
func taskTypeManualRecommended(taskType string) bool {
	switch {
	case taskType == TypeCacheRefreshRequest:
		return true
	case taskType == "admin:export":
		return true
	case taskType == taskreport.TaskTypeDailySummary:
		return true
	default:
		return false
	}
}

// workflowUsageHint 返回工作流使用提示。
func workflowUsageHint(name string) string {
	switch name {
	case WorkflowNameCacheRefresh:
		return "适合按目标列表触发缓存刷新；targets 可填写缓存目标或页面带入的缓存键。"
	case archive.WorkflowNameRun:
		return "适合在低峰期批量执行热表归档；targets 可填写 admin_log 等归档任务名。"
	case taskreport.WorkflowNameDailySummary:
		return "通常由每天 10 点周期任务触发，发送昨日 10 点到今日 10 点的任务运行日报；手动触发可覆盖统计窗口。"
	case "user_tag.full.rebuild":
		return "每日全量重建链路，建议在低峰期执行，避免和日常重算任务叠加。"
	case "user_tag.delta.refresh":
		return "用于日常增量刷新用户标签，通常不需要填写 targets。"
	case "user_tag.targeted.calc":
		return "用于指定用户补算，必须在 targets 中填写 uid；多维筛选 hash 不能作为用户标签计算来源。"
	case "user_tag.tag.recalculate":
		return "用于按标签类型全量重算，建议在 targets 中携带 tagType 或执行标识。"
	case "user_tag.runtime.cleanup":
		return "用于机会型清理用户标签运行期辅助表，通常由周期任务触发，不需要填写 targets。"
	case "user_tag.event_outbox.retry_scan":
		return "用于周期扫描并重派用户标签事件 outbox 异常数据，通常由周期任务触发，不需要填写 targets。"
	default:
		return "触发前请确认执行目标和默认队列是否符合当前环境。"
	}
}

// workflowTargetsExample 返回工作流执行目标填写示例。
func workflowTargetsExample(name string) string {
	switch name {
	case WorkflowNameCacheRefresh:
		return strings.Join([]string{fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1)}, ", ")
	case archive.WorkflowNameRun:
		return "admin_log"
	case taskreport.WorkflowNameDailySummary:
		return ""
	case "user_tag.targeted.calc":
		return "uid:10001, uid:10002"
	case "user_tag.tag.recalculate":
		return "tagType:2, tagType:3"
	case "user_tag.runtime.cleanup":
		return ""
	case "user_tag.event_outbox.retry_scan":
		return ""
	default:
		return ""
	}
}

// workflowTriggerTaskName 返回工作流触发入口任务的展示名称。
func workflowTriggerTaskName(workflowName string, periodicName string) string {
	periodicName = strings.TrimSpace(periodicName)
	if periodicName != "" {
		return fmt.Sprintf("周期任务触发:%s", periodicName)
	}
	workflowName = strings.TrimSpace(workflowName)
	if workflowName != "" {
		return fmt.Sprintf("工作流触发:%s", workflowName)
	}
	return "工作流触发"
}

// workflowNodeTaskName 返回工作流节点任务的展示名称，便于日志直接识别当前跑的是哪个脚本节点。
func workflowNodeTaskName(workflowName string, workflowNode string, taskType string) string {
	workflowName = strings.TrimSpace(workflowName)
	workflowNode = strings.TrimSpace(workflowNode)
	if workflowName != "" && workflowNode != "" {
		return fmt.Sprintf("%s/%s", workflowName, workflowNode)
	}
	if workflowNode != "" {
		return fmt.Sprintf("%s/%s", strings.TrimSpace(taskType), workflowNode)
	}
	return taskTypeDisplayName(taskType)
}

// periodicTaskDisplayName 返回周期任务触发入口的展示名称。
func periodicTaskDisplayName(item config.TaskPeriodicConfig) string {
	return workflowTriggerTaskName(strings.TrimSpace(item.Workflow), strings.TrimSpace(item.Name))
}

// taskNameFromTask 从任务头、工作流元数据和任务类型中推导稳定的任务名称。
func taskNameFromTask(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	if taskName := strings.TrimSpace(task.Headers()[headerTaskName]); taskName != "" {
		return taskName
	}
	meta := workflowTaskMetaFromTask(task)
	if task.Type() == TypeWorkflowTrigger {
		return workflowTriggerTaskName(meta.WorkflowName, "")
	}
	if meta.WorkflowName != "" || meta.WorkflowNode != "" {
		return workflowNodeTaskName(meta.WorkflowName, meta.WorkflowNode, task.Type())
	}
	return taskTypeDisplayName(task.Type())
}

// TaskDisplayName 返回任务展示名称，供任务系统外的告警和审计链路复用。
func TaskDisplayName(task *asynq.Task) string {
	return taskNameFromTask(task)
}

// TaskSource 返回任务来源，供任务系统外的告警和审计链路复用。
func TaskSource(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Headers()[headerTaskSource])
}

// taskInfoDisplayName 从 Asynq 任务详情中推导任务展示名称，供任务列表和详情页复用。
func taskInfoDisplayName(info *asynq.TaskInfo) string {
	if info == nil {
		return ""
	}
	if taskName := strings.TrimSpace(info.Headers[headerTaskName]); taskName != "" {
		return taskName
	}
	meta := WorkflowTaskMeta{}
	if len(info.Payload) > 0 {
		_ = json.Unmarshal(info.Payload, &meta)
	}
	if workflowName := helper.FirstNonEmptyString(strings.TrimSpace(info.Headers[headerWorkflowName]), strings.TrimSpace(meta.WorkflowName)); workflowName != "" {
		if info.Type == TypeWorkflowTrigger {
			return workflowTriggerTaskName(workflowName, "")
		}
		workflowNode := helper.FirstNonEmptyString(strings.TrimSpace(info.Headers[headerWorkflowNode]), strings.TrimSpace(meta.WorkflowNode))
		return workflowNodeTaskName(workflowName, workflowNode, info.Type)
	}
	return taskTypeDisplayName(info.Type)
}

// taskLogFields 返回任务执行链路的统一附加日志字段，便于按脚本、来源和工作流快速检索。
func taskLogFields(task *asynq.Task) []logx.LogField {
	if task == nil {
		return nil
	}
	fields := make([]logx.LogField, 0, 4)
	if taskName := taskNameFromTask(task); taskName != "" {
		fields = append(fields, logx.Field("task_name", taskName))
	}
	if taskSource := strings.TrimSpace(task.Headers()[headerTaskSource]); taskSource != "" {
		fields = append(fields, logx.Field("task_source", taskSource))
	}
	return fields
}

// taskStatsLogFields 返回任务执行统计摘要字段，字段名保持稳定便于日志平台聚合。
func taskStatsLogFields(snapshot *taskstats.Snapshot) []logx.LogField {
	if snapshot == nil || snapshot.Empty() {
		return nil
	}
	return []logx.LogField{
		logx.Field("trace_total_count", snapshot.TotalCount),
		logx.Field("trace_read_count", snapshot.ReadCount),
		logx.Field("trace_insert_count", snapshot.InsertCount),
		logx.Field("trace_update_count", snapshot.UpdateCount),
		logx.Field("trace_delete_count", snapshot.DeleteCount),
		logx.Field("trace_upsert_count", snapshot.UpsertCount),
		logx.Field("trace_skip_count", snapshot.SkipCount),
		logx.Field("trace_error_count", snapshot.ErrorCount),
		logx.Field("trace_detail_count", len(snapshot.Details)),
		logx.Field("trace_duration_ms", snapshot.DurationMS),
	}
}

// taskLogMessage 构造任务生命周期日志正文，方便只看 message 列时也能定位任务。
func taskLogMessage(action string, meta *requestctx.Meta) string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "任务 日志"
	}
	parts := []string{action}
	if meta == nil {
		return strings.Join(parts, " ")
	}
	parts = appendTextKV(parts, "task_name", meta.TaskName)
	parts = appendTextKV(parts, "task_type", meta.TaskType)
	parts = appendTextKV(parts, "task_id", meta.TaskID)
	parts = appendTextKV(parts, "queue", meta.TaskQueue)
	parts = appendTextKV(parts, "workflow", meta.WorkflowName)
	parts = appendTextKV(parts, "node", meta.WorkflowNode)
	parts = appendTextKV(parts, "workflow_id", meta.WorkflowID)
	parts = appendTextKV(parts, "mode", meta.Mode)
	if meta.ShardTotal > 0 {
		parts = append(parts, fmt.Sprintf("shard=%d/%d", meta.ShardIndex, meta.ShardTotal))
	}
	if meta.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("latency_ms=%d", meta.LatencyMS))
	}
	return strings.Join(parts, " ")
}

// appendTextKV 向日志正文追加非空键值片段。
func appendTextKV(parts []string, key string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, fmt.Sprintf("%s=%s", key, value))
}

// buildTaskExecutionResult 组装任务成功后的统一结果摘要，供任务详情页直接展示。
func buildTaskExecutionResult(task *asynq.Task, meta *requestctx.Meta, startedAt time.Time, statsSnapshot *taskstats.Snapshot) TaskExecutionResult {
	result := TaskExecutionResult{
		Status:         taskExecutionStatusSuccess,
		FinishedAt:     time.Now().Format(time.RFC3339),
		ExecutionTrace: statsSnapshot,
	}
	if !startedAt.IsZero() {
		result.StartedAt = startedAt.Format(time.RFC3339)
	}
	if task != nil {
		result.TaskSource = strings.TrimSpace(task.Headers()[headerTaskSource])
		fillTaskExecutionResultFromPayload(task, &result)
	}
	if meta == nil {
		if task != nil {
			result.TaskType = strings.TrimSpace(task.Type())
			result.TaskName = taskNameFromTask(task)
		}
		return result
	}
	result.TraceID = strings.TrimSpace(meta.TraceID)
	result.TaskID = strings.TrimSpace(meta.TaskID)
	result.TaskType = strings.TrimSpace(meta.TaskType)
	result.TaskName = strings.TrimSpace(meta.TaskName)
	result.TaskQueue = strings.TrimSpace(meta.TaskQueue)
	result.LatencyMS = meta.LatencyMS
	result.BizCode = meta.BizCode
	result.BizMessage = strings.TrimSpace(meta.BizMessage)
	result.WorkflowID = strings.TrimSpace(meta.WorkflowID)
	result.WorkflowName = strings.TrimSpace(meta.WorkflowName)
	result.WorkflowNode = strings.TrimSpace(meta.WorkflowNode)
	result.ShardIndex = meta.ShardIndex
	result.ShardTotal = meta.ShardTotal
	if result.TaskType == "" && task != nil {
		result.TaskType = strings.TrimSpace(task.Type())
	}
	if result.TaskName == "" && task != nil {
		result.TaskName = taskNameFromTask(task)
	}
	return result
}

// fillTaskExecutionResultFromPayload 从任务 payload 中提炼结果摘要，减少排障时反复展开原始 JSON 的成本。
func fillTaskExecutionResultFromPayload(task *asynq.Task, result *TaskExecutionResult) {
	if task == nil || result == nil || len(task.Payload()) == 0 {
		return
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(task.Payload(), &payloadMap); err != nil || len(payloadMap) == 0 {
		return
	}
	result.PayloadKeys = payloadSummaryKeys(payloadMap)
	result.Queue = helper.FirstNonEmptyString(result.Queue, strings.TrimSpace(anyToString(payloadMap["queue"])))
	result.WorkflowID = helper.FirstNonEmptyString(result.WorkflowID, strings.TrimSpace(anyToString(payloadMap["workflowId"])))
	result.WorkflowName = helper.FirstNonEmptyString(result.WorkflowName, strings.TrimSpace(anyToString(payloadMap["workflowName"])))
	result.WorkflowNode = helper.FirstNonEmptyString(result.WorkflowNode, strings.TrimSpace(anyToString(payloadMap["workflowNode"])))
	if result.ShardTotal == 0 {
		result.ShardTotal = anyToInt(payloadMap["shardTotal"])
	}
	if result.ShardIndex == 0 {
		result.ShardIndex = anyToInt(payloadMap["shardIndex"])
	}
	if result.GrayPercent == 0 {
		result.GrayPercent = anyToInt(payloadMap["grayPercent"])
	}
	result.Operation = helper.FirstNonEmptyString(result.Operation, strings.TrimSpace(anyToString(payloadMap["operation"])))
	result.Mode = helper.FirstNonEmptyString(result.Mode, strings.TrimSpace(anyToString(payloadMap["mode"])))
	if targets := anyToStringSlice(payloadMap["targets"]); len(targets) > 0 {
		result.TargetCount = len(targets)
		result.Targets = limitPreviewStrings(targets, 5)
	}
	if uids := anyToStringSlice(payloadMap["uids"]); len(uids) > 0 {
		result.UIDCount = len(uids)
	}
	if tagTypes := anyToStringSlice(payloadMap["tagTypes"]); len(tagTypes) > 0 {
		result.TagTypeCount = len(tagTypes)
	}
}

// payloadSummaryKeys 返回 payload 顶层字段摘要，固定排序后截断，避免结果展示过长。
func payloadSummaryKeys(payloadMap map[string]any) []string {
	keys := make([]string, 0, len(payloadMap))
	for key := range payloadMap {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return limitPreviewStrings(keys, 12)
}

// anyToString 把任意 JSON 值转成字符串，优先用于结果摘要提取。
func anyToString(value any) string {
	switch current := value.(type) {
	case string:
		return current
	case float64:
		return strconv.FormatInt(int64(current), 10)
	case int:
		return strconv.Itoa(current)
	case int64:
		return strconv.FormatInt(current, 10)
	case json.Number:
		return current.String()
	default:
		return ""
	}
}

// anyToInt 把任意 JSON 值转成整数，常用于提取 shard/gray 等配置字段。
func anyToInt(value any) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	case int64:
		return int(current)
	case json.Number:
		parsed, _ := current.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(current))
		return parsed
	default:
		return 0
	}
}

// anyToStringSlice 把 JSON 数组统一转成字符串切片，便于统计 targets/uids/tagTypes 数量。
func anyToStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(anyToString(item))
		if text == "" {
			continue
		}
		result = append(result, text)
	}
	return result
}

// limitPreviewStrings 截断预览字段条数，避免任务结果因目标列表过长而失控。
func limitPreviewStrings(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}
