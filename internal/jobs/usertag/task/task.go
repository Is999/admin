package task

import (
	"encoding/json"

	"admin_cron/internal/config"
	"admin_cron/internal/jobs/usertag/options"
	"admin_cron/internal/jobs/usertag/types"

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
	// WorkflowNameUserTagKafkaOutboxRetryScan 表示用户标签 Kafka outbox 异常扫描重推工作流。
	WorkflowNameUserTagKafkaOutboxRetryScan = "user_tag.kafka_outbox.retry_scan"
)

const (
	// TaskTypeUserTagPrepare 准备用户标签运行环境。
	TaskTypeUserTagPrepare = "user_tag:prepare"
	// TaskTypeUserTagBusinessHook 预留扩展入口，默认不做具体处理。
	TaskTypeUserTagBusinessHook = "user_tag:business_hook"
	// TaskTypeUserTagFinalize 原子切换全量重建结果表。
	TaskTypeUserTagFinalize = "user_tag:finalize"
	// TaskTypeUserTagSyncKafka 重建 full 同步快照或兼容历史 Kafka 同步节点。
	TaskTypeUserTagSyncKafka = "user_tag:sync_kafka"
	// TaskTypeUserTagKafkaOutboxRetry 独立重试用户标签 outbox，不参与用户标签计算工作流成败。
	TaskTypeUserTagKafkaOutboxRetry = types.TaskTypeUserTagKafkaOutboxRetry
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

// NewDefaults 从全局配置构造用户标签默认运行参数。
func NewDefaults(cfg config.UserTagConfig) Defaults {
	return options.NewDefaults(cfg)
}

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
