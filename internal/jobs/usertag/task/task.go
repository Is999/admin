package task

import (
	"encoding/json"

	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// WorkflowNameUserTagFull 表示用户标签每日全量重建工作流。
	WorkflowNameUserTagFull = "user_tag.full.rebuild"
	// WorkflowNameUserTagDelta 表示用户标签每日增量刷新工作流。
	WorkflowNameUserTagDelta = "user_tag.delta.refresh"
	// WorkflowNameUserTagTargeted 表示指定用户补算工作流。
	WorkflowNameUserTagTargeted = "user_tag.targeted.calc"
	// WorkflowNameUserTagRecalculate 表示指定标签全量重算工作流。
	WorkflowNameUserTagRecalculate = "user_tag.tag.recalculate"
	// WorkflowNameUserTagRuntimeCleanup 表示用户标签运行期辅助表清理工作流。
	WorkflowNameUserTagRuntimeCleanup = "user_tag.runtime.cleanup"
	// WorkflowNameUserTagEventOutboxRetryScan 表示用户标签事件 outbox 异常扫描重派工作流。
	WorkflowNameUserTagEventOutboxRetryScan = "user_tag.event_outbox.retry_scan"
)

const (
	// TaskTypeUserTagPrepare 准备用户标签运行环境。
	TaskTypeUserTagPrepare = "user_tag:prepare"
	// TaskTypeUserTagCollectScope 收集本次工作流候选范围。
	TaskTypeUserTagCollectScope = "user_tag:collect_scope"
	// TaskTypeUserTagEvaluateTags 执行通用标签规则评估。
	TaskTypeUserTagEvaluateTags = "user_tag:evaluate_tags"
	// TaskTypeUserTagResolveChanges 解析标签得到和失去差异。
	TaskTypeUserTagResolveChanges = "user_tag:resolve_changes"
	// TaskTypeUserTagPersistResults 写入标签结果和事件 outbox。
	TaskTypeUserTagPersistResults = "user_tag:persist_results"
	// TaskTypeUserTagFinalize 原子切换全量重建结果表。
	TaskTypeUserTagFinalize = "user_tag:finalize"
	// TaskTypeUserTagDispatchHooks 派发标签得失事件 hook。
	TaskTypeUserTagDispatchHooks = "user_tag:dispatch_hooks"
	// TaskTypeUserTagEventOutboxRetry 独立重试用户标签事件 outbox，不参与用户标签计算工作流成败。
	TaskTypeUserTagEventOutboxRetry = types.TaskTypeUserTagEventOutboxRetry
	// TaskTypeUserTagRuntimeCleanup 独立清理用户标签运行期辅助表，不参与用户标签计算工作流成败。
	TaskTypeUserTagRuntimeCleanup = types.TaskTypeUserTagRuntimeCleanup
)

const (
	// ModeFull 表示全量重建模式。
	ModeFull = types.ModeFull
	// ModeDelta 表示增量刷新模式。
	ModeDelta = types.ModeDelta
	// ModeTargeted 表示显式指定 UID 补算模式。
	ModeTargeted = types.ModeTargeted
	// ModeRecalculate 表示指定标签全量重算模式。
	ModeRecalculate = types.ModeRecalculate
)

type (
	// WorkflowPayload 是用户标签 DAG 节点之间传递的统一负载。
	WorkflowPayload = types.WorkflowPayload
	// Defaults 是用户标签工作流默认运行参数。
	Defaults = options.Defaults
)

// DecodePayload 解析 Asynq 任务负载。
func DecodePayload(task *asynq.Task) (WorkflowPayload, error) {
	var payload WorkflowPayload
	if task == nil {
		return payload, errors.Errorf("任务不能为空")
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return payload, errors.Tag(err)
	}
	return payload, nil
}
