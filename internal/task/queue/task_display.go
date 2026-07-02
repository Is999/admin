package taskqueue

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"

	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// TaskTypeDisplaySpec 描述任务类型在管理端展示和人工投递时使用的元数据。
type TaskTypeDisplaySpec struct {
	TaskType          string // 精确任务类型；与 TaskTypePrefix 二选一
	TaskTypePrefix    string // 任务类型前缀，用于同类动态任务
	DescriptionKey    string // 任务类型说明 i18n key
	UsageHintKey      string // 使用提示 i18n key
	PayloadExample    string // 推荐人工投递 payload 示例
	ManualRecommended bool   // 是否推荐人工手动投递
}

// WorkflowDisplaySpec 描述工作流在管理端展示和人工触发时使用的元数据。
type WorkflowDisplaySpec struct {
	Name           string // 工作流名称
	DescriptionKey string // 工作流说明 i18n key
	UsageHintKey   string // 使用提示 i18n key
	TargetsExample string // 目标输入示例
}

var (
	taskDisplayMu       sync.RWMutex                       // 保护任务类型和工作流展示元数据注册表
	taskTypeDisplayMap  = map[string]TaskTypeDisplaySpec{} // 精确任务类型到展示元数据的映射
	taskTypeDisplayList []TaskTypeDisplaySpec              // 前缀匹配任务类型展示元数据，按注册顺序查找
	workflowDisplayMap  = map[string]WorkflowDisplaySpec{} // 工作流名称到展示元数据的映射
)

func init() {
	RegisterTaskTypeDisplaySpecs(
		TaskTypeDisplaySpec{
			TaskType:       TypeWorkflowTrigger,
			DescriptionKey: i18n.MsgKeyTaskRegistryTypeWorkflowTriggerDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryTypeWorkflowTriggerHint,
			PayloadExample: fmt.Sprintf("{\n  \"workflowId\": \"wf-demo-001\",\n  \"workflowName\": \"cache.refresh\",\n  \"targets\": [\"%s\"]\n}", fmt.Sprintf(keys.AdminProfile, 1)),
		},
		TaskTypeDisplaySpec{
			TaskType:       TypeWorkflowNoop,
			DescriptionKey: i18n.MsgKeyTaskRegistryTypeWorkflowNoopDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryTypeWorkflowNoopHint,
			PayloadExample: "{\n  \"note\": \"finalize\"\n}",
		},
		TaskTypeDisplaySpec{
			TaskType:          TypeCacheRefreshRequest,
			DescriptionKey:    i18n.MsgKeyTaskRegistryTypeCacheRefreshRequestDesc,
			UsageHintKey:      i18n.MsgKeyTaskRegistryTypeCacheRefreshRequestHint,
			PayloadExample:    fmt.Sprintf("{\n  \"operation\": \"manual_refresh\",\n  \"targets\": [\"%s\", \"%s\"]\n}", fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1)),
			ManualRecommended: true,
		},
		TaskTypeDisplaySpec{
			TaskType:       TypeCacheRefreshBatch,
			DescriptionKey: i18n.MsgKeyTaskRegistryTypeCacheRefreshBatchDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryTypeCacheRefreshBatchHint,
			PayloadExample: fmt.Sprintf("{\n  \"operation\": \"manual_refresh\",\n  \"targets\": [\"%s\", \"%s\"]\n}", fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1)),
		},
	)
	RegisterWorkflowDisplaySpecs(WorkflowDisplaySpec{
		Name:           WorkflowNameCacheRefresh,
		DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowCacheRefreshDesc,
		UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowCacheRefreshHint,
		TargetsExample: strings.Join([]string{fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1)}, ", "),
	})
}

// RegisterTaskTypeDisplaySpecs 注册任务类型展示元数据；重复注册同一任务类型时以后者覆盖。
func RegisterTaskTypeDisplaySpecs(specs ...TaskTypeDisplaySpec) {
	taskDisplayMu.Lock()
	defer taskDisplayMu.Unlock()
	for _, spec := range specs {
		spec.TaskType = strings.TrimSpace(spec.TaskType)
		spec.TaskTypePrefix = strings.TrimSpace(spec.TaskTypePrefix)
		if spec.TaskType != "" {
			taskTypeDisplayMap[spec.TaskType] = spec
			continue
		}
		if spec.TaskTypePrefix == "" {
			continue
		}
		replaced := false
		for idx := range taskTypeDisplayList {
			if taskTypeDisplayList[idx].TaskTypePrefix == spec.TaskTypePrefix {
				taskTypeDisplayList[idx] = spec
				replaced = true
				break
			}
		}
		if !replaced {
			taskTypeDisplayList = append(taskTypeDisplayList, spec)
		}
	}
}

// RegisterWorkflowDisplaySpecs 注册工作流展示元数据；重复注册同一工作流时以后者覆盖。
func RegisterWorkflowDisplaySpecs(specs ...WorkflowDisplaySpec) {
	taskDisplayMu.Lock()
	defer taskDisplayMu.Unlock()
	for _, spec := range specs {
		spec.Name = strings.TrimSpace(spec.Name)
		if spec.Name == "" {
			continue
		}
		workflowDisplayMap[spec.Name] = spec
	}
}

// taskTypeDisplaySpec 按精确任务类型优先、前缀兜底读取展示元数据。
func taskTypeDisplaySpec(taskType string) (TaskTypeDisplaySpec, bool) {
	taskType = strings.TrimSpace(taskType)
	taskDisplayMu.RLock()
	defer taskDisplayMu.RUnlock()
	if spec, ok := taskTypeDisplayMap[taskType]; ok {
		return spec, true
	}
	for _, spec := range taskTypeDisplayList {
		if spec.TaskTypePrefix != "" && strings.HasPrefix(taskType, spec.TaskTypePrefix) {
			return spec, true
		}
	}
	return TaskTypeDisplaySpec{}, false
}

// workflowDisplaySpec 按工作流名称读取展示元数据。
func workflowDisplaySpec(name string) (WorkflowDisplaySpec, bool) {
	name = strings.TrimSpace(name)
	taskDisplayMu.RLock()
	defer taskDisplayMu.RUnlock()
	spec, ok := workflowDisplayMap[name]
	return spec, ok
}

// taskTypeDescription 返回默认语言任务类型说明。
func taskTypeDescription(taskType string) string {
	return taskTypeDescriptionForLocale(taskType, i18n.LocaleZHCN)
}

// taskTypeDescriptionForLocale 返回指定语言任务类型说明。
func taskTypeDescriptionForLocale(taskType string, locale string) string {
	if spec, ok := taskTypeDisplaySpec(taskType); ok {
		if description := taskDisplayMessage(spec.DescriptionKey, locale); description != "" {
			return description
		}
	}
	return i18n.MessageByKey(i18n.MsgKeyTaskRegistryTypeRegisteredDesc, locale)
}

// taskTypeDisplayName 返回任务类型的人类可读名称，优先用于日志和管理后台展示。
func taskTypeDisplayName(taskType string) string {
	description := strings.TrimSpace(taskTypeDescription(strings.TrimSpace(taskType)))
	defaultDescription := i18n.MessageByKey(i18n.MsgKeyTaskRegistryTypeRegisteredDesc, i18n.LocaleZHCN)
	if description == "" || description == defaultDescription {
		return strings.TrimSpace(taskType)
	}
	return description
}

// taskTypeUsageHint 返回指定语言任务类型使用提示。
func taskTypeUsageHint(taskType string, locale string) string {
	if spec, ok := taskTypeDisplaySpec(taskType); ok {
		if usageHint := taskDisplayMessage(spec.UsageHintKey, locale); usageHint != "" {
			return usageHint
		}
	}
	return i18n.MessageByKey(i18n.MsgKeyTaskRegistryTypeDefaultHint, locale)
}

// taskTypePayloadExample 返回任务类型推荐负载示例。
func taskTypePayloadExample(taskType string) string {
	if spec, ok := taskTypeDisplaySpec(taskType); ok && strings.TrimSpace(spec.PayloadExample) != "" {
		return strings.TrimSpace(spec.PayloadExample)
	}
	return "{\n  \"demo\": true\n}"
}

// taskTypeManualRecommended 返回当前任务类型是否适合在总控台人工直接投递。
func taskTypeManualRecommended(taskType string) bool {
	if spec, ok := taskTypeDisplaySpec(taskType); ok {
		return spec.ManualRecommended
	}
	return false
}

// workflowDescription 返回指定语言工作流说明。
func workflowDescription(name string, fallback string, locale string) string {
	if spec, ok := workflowDisplaySpec(name); ok {
		if description := taskDisplayMessage(spec.DescriptionKey, locale); description != "" {
			return description
		}
	}
	return strings.TrimSpace(fallback)
}

// workflowUsageHint 返回指定语言工作流使用提示。
func workflowUsageHint(name string, locale string) string {
	if spec, ok := workflowDisplaySpec(name); ok {
		if usageHint := taskDisplayMessage(spec.UsageHintKey, locale); usageHint != "" {
			return usageHint
		}
	}
	return i18n.MessageByKey(i18n.MsgKeyTaskRegistryWorkflowDefaultHint, locale)
}

// taskDisplayMessage 通过统一语言包解析前端可见的任务注册表文案。
func taskDisplayMessage(key string, locale string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	text := strings.TrimSpace(i18n.MessageByKey(key, locale))
	if text == "" || text == key {
		return ""
	}
	return text
}

// workflowTargetsExample 返回工作流执行目标填写示例。
func workflowTargetsExample(name string) string {
	if spec, ok := workflowDisplaySpec(name); ok {
		return strings.TrimSpace(spec.TargetsExample)
	}
	return ""
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
