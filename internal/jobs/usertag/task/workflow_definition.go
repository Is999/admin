package task

import (
	"encoding/json"
	"time"

	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/types"
	"admin/internal/task/queue"
)

const (
	// userTagMainWorkflowNodeTimeout 表示用户标签主计算节点不设置固定 Asynq 超时。
	// 生产大数据量下由分片、批次和工作流互斥边界控制执行范围，避免节点因固定时长中断后反复重跑。
	userTagMainWorkflowNodeTimeout time.Duration = 0
)

// WorkflowDefinitions 返回用户标签工作流定义。
func WorkflowDefinitions(defaults Defaults) []*taskqueue.WorkflowDefinition {
	return []*taskqueue.WorkflowDefinition{
		workflowDefinition(WorkflowNameUserTagFull, "用户标签每日全量重建", types.ModeFull, defaults),
		workflowDefinition(WorkflowNameUserTagDelta, "用户标签每日增量刷新", types.ModeDelta, defaults),
		workflowDefinition(WorkflowNameUserTagTargeted, "用户标签指定用户补算", types.ModeTargeted, defaults),
		workflowDefinition(WorkflowNameUserTagRecalculate, "用户标签指定标签全量重算", types.ModeRecalculate, defaults),
	}
}

// RuntimeCleanupWorkflowDefinition 返回用户标签运行期辅助表清理工作流定义。
// 清理工作流只有一个节点，独立于 full/delta/targeted/recalculate 主 DAG，避免容量治理任务影响标签计算成败。
func RuntimeCleanupWorkflowDefinition(defaults Defaults) *taskqueue.WorkflowDefinition {
	return &taskqueue.WorkflowDefinition{
		Name:          WorkflowNameUserTagRuntimeCleanup,
		Description:   "用户标签运行期辅助表清理",
		DefaultQueue:  taskqueue.QueueMaintenance,
		MaxShardTotal: 1,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			types.NodeRuntimeCleanup: {
				Name:         types.NodeRuntimeCleanup,
				TaskType:     TaskTypeUserTagRuntimeCleanup,
				Timeout:      10 * time.Minute,
				BuildPayload: buildPayload(types.ModeDelta, defaults),
			},
		},
	}
}

// KafkaOutboxRetryScanWorkflowDefinition 返回用户标签 Kafka outbox 异常扫描重推工作流定义。
// 该工作流只扫描已落库 outbox 异常行，不重新计算用户标签，不参与用户标签主 DAG 成败。
func KafkaOutboxRetryScanWorkflowDefinition(defaults Defaults) *taskqueue.WorkflowDefinition {
	return &taskqueue.WorkflowDefinition{
		Name:          WorkflowNameUserTagKafkaOutboxRetryScan,
		Description:   "用户标签 Kafka outbox 异常扫描重推",
		DefaultQueue:  taskqueue.QueueMaintenance,
		MaxShardTotal: 1,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			types.NodeKafkaOutboxRetryScan: {
				Name:         types.NodeKafkaOutboxRetryScan,
				TaskType:     TaskTypeUserTagKafkaOutboxRetry,
				Timeout:      9 * time.Minute,
				BuildPayload: buildPayload(types.ModeDelta, defaults),
			},
		},
	}
}

// workflowDefinition 构造单个用户标签工作流定义，统一节点拓扑；主计算节点默认不设置固定超时。
func workflowDefinition(name, description, mode string, defaults Defaults) *taskqueue.WorkflowDefinition {
	finalizeMaxRetry := 0
	if mode == types.ModeFull {
		// full finalize 会执行 MySQL RENAME TABLE 非幂等切表，禁止被工作流全局重试覆盖。
		finalizeMaxRetry = -1
	}
	return &taskqueue.WorkflowDefinition{
		Name:          name,
		Description:   description,
		DefaultQueue:  taskqueue.QueueMaintenance,
		MaxShardTotal: options.MaxShardTotal,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			types.NodePrepare: {
				Name:         types.NodePrepare,
				TaskType:     TaskTypeUserTagPrepare,
				Timeout:      userTagMainWorkflowNodeTimeout,
				BuildPayload: buildPayload(mode, defaults),
			},
			types.NodeBusinessHook: {
				Name:             types.NodeBusinessHook,
				TaskType:         TaskTypeUserTagBusinessHook,
				DependsOn:        []string{types.NodePrepare},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: true,
				BuildPayload:     buildPayload(mode, defaults),
			},
			types.NodeFinalize: {
				Name:         types.NodeFinalize,
				TaskType:     TaskTypeUserTagFinalize,
				DependsOn:    []string{types.NodeBusinessHook},
				MaxRetry:     finalizeMaxRetry,
				Timeout:      userTagMainWorkflowNodeTimeout,
				BuildPayload: buildPayload(mode, defaults),
			},
			types.NodeSyncKafka: {
				Name:             types.NodeSyncKafka,
				TaskType:         TaskTypeUserTagSyncKafka,
				DependsOn:        []string{types.NodeFinalize},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: true,
				BuildPayload:     buildPayload(mode, defaults),
			},
		},
	}
}

// buildPayload 为工作流节点构造统一任务负载，延迟到运行期补齐默认批次参数。
func buildPayload(mode string, defaults Defaults) func(taskqueue.WorkflowStartSpec, *taskqueue.WorkflowNodeDefinition, int, int) ([]byte, error) {
	_ = defaults
	return func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
		payload := types.WorkflowPayload{
			WorkflowID:  spec.WorkflowID,
			Mode:        mode,
			Node:        node.Name,
			Targets:     spec.Targets,
			ShardIndex:  shardIndex,
			ShardTotal:  shardTotal,
			BatchSize:   0,
			WorkerCount: 0,
		}
		return json.Marshal(payload)
	}
}
