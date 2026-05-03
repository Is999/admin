package task

import (
	"encoding/json"
	"strings"
	"time"

	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/types"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
)

const (
	// userTagMainWorkflowNodeTimeout 表示用户标签主 DAG 节点的显式长超时。
	userTagMainWorkflowNodeTimeout = 24 * time.Hour
	// userTagRuntimeCleanupNodeTimeout 表示运行期清理节点硬超时。
	userTagRuntimeCleanupNodeTimeout = time.Hour
	// userTagEventOutboxRetryNodeTimeout 表示事件 outbox 重派节点硬超时。
	userTagEventOutboxRetryNodeTimeout = 9 * time.Minute
)

// WorkflowDefinitions 返回用户标签四类主工作流定义。
func WorkflowDefinitions(defaults Defaults) []*taskqueue.WorkflowDefinition {
	return []*taskqueue.WorkflowDefinition{
		workflowDefinition(WorkflowNameUserTagFull, "用户标签每日全量重建", types.ModeFull, defaults),
		workflowDefinition(WorkflowNameUserTagDelta, "用户标签增量刷新", types.ModeDelta, defaults),
		workflowDefinition(WorkflowNameUserTagTargeted, "用户标签指定用户补算", types.ModeTargeted, defaults),
		workflowDefinition(WorkflowNameUserTagRecalculate, "用户标签指定标签全量重算", types.ModeRecalculate, defaults),
	}
}

// RuntimeCleanupWorkflowDefinition 返回用户标签运行期辅助表清理工作流定义。
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
				Timeout:      userTagRuntimeCleanupNodeTimeout,
				BuildPayload: buildPayload(types.ModeDelta, defaults),
			},
		},
	}
}

// EventOutboxRetryScanWorkflowDefinition 返回用户标签事件 outbox 异常扫描重派工作流定义。
func EventOutboxRetryScanWorkflowDefinition(defaults Defaults) *taskqueue.WorkflowDefinition {
	return &taskqueue.WorkflowDefinition{
		Name:          WorkflowNameUserTagEventOutboxRetryScan,
		Description:   "用户标签事件 outbox 异常扫描重派",
		DefaultQueue:  taskqueue.QueueMaintenance,
		MaxShardTotal: 1,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			types.NodeEventOutboxRetryScan: {
				Name:         types.NodeEventOutboxRetryScan,
				TaskType:     TaskTypeUserTagEventOutboxRetry,
				Timeout:      userTagEventOutboxRetryNodeTimeout,
				BuildPayload: buildPayload(types.ModeDelta, defaults),
			},
		},
	}
}

// workflowDefinition 构造用户标签主 DAG，所有模式共享同一套通用节点语义。
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
			types.NodeCollectScope: {
				Name:             types.NodeCollectScope,
				TaskType:         TaskTypeUserTagCollectScope,
				DependsOn:        []string{types.NodePrepare},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: nodeSupportsSharding(mode),
				BuildPayload:     buildPayload(mode, defaults),
			},
			types.NodeEvaluateTags: {
				Name:             types.NodeEvaluateTags,
				TaskType:         TaskTypeUserTagEvaluateTags,
				DependsOn:        []string{types.NodeCollectScope},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: nodeSupportsSharding(mode),
				BuildPayload:     buildPayload(mode, defaults),
			},
			types.NodeResolveChanges: {
				Name:             types.NodeResolveChanges,
				TaskType:         TaskTypeUserTagResolveChanges,
				DependsOn:        []string{types.NodeEvaluateTags},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: nodeSupportsSharding(mode),
				BuildPayload:     buildPayload(mode, defaults),
			},
			types.NodePersistResults: {
				Name:             types.NodePersistResults,
				TaskType:         TaskTypeUserTagPersistResults,
				DependsOn:        []string{types.NodeResolveChanges},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: nodeSupportsSharding(mode),
				BuildPayload:     buildPayload(mode, defaults),
			},
			types.NodeFinalize: {
				Name:         types.NodeFinalize,
				TaskType:     TaskTypeUserTagFinalize,
				DependsOn:    []string{types.NodePersistResults},
				MaxRetry:     finalizeMaxRetry,
				Timeout:      userTagMainWorkflowNodeTimeout,
				BuildPayload: buildPayload(mode, defaults),
			},
			types.NodeDispatchHooks: {
				Name:             types.NodeDispatchHooks,
				TaskType:         TaskTypeUserTagDispatchHooks,
				DependsOn:        []string{types.NodeFinalize},
				Timeout:          userTagMainWorkflowNodeTimeout,
				SupportsSharding: nodeSupportsSharding(mode),
				BuildPayload:     buildPayload(mode, defaults),
			},
		},
	}
}

// nodeSupportsSharding 判断当前模式下主 DAG 节点是否按 workflow shard 并发。
func nodeSupportsSharding(mode string) bool {
	return mode != types.ModeTargeted
}

// buildPayload 为工作流节点构造统一任务负载。
func buildPayload(mode string, defaults Defaults) func(taskqueue.WorkflowStartSpec, *taskqueue.WorkflowNodeDefinition, int, int) ([]byte, error) {
	return func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
		batchSize := 0
		if node.Name == types.NodeDispatchHooks || node.Name == types.NodeEventOutboxRetryScan {
			batchSize = defaults.EventBatchSize
		}
		base := types.WorkflowPayload{
			WorkflowID:  spec.WorkflowID,
			Mode:        mode,
			Node:        node.Name,
			Targets:     spec.Targets,
			ShardIndex:  shardIndex,
			ShardTotal:  shardTotal,
			BatchSize:   batchSize,
			WorkerCount: 0,
		}
		opts, err := options.ParseOptions(base, defaults)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if opts.Mode != mode {
			return nil, errors.Errorf("用户标签工作流模式不匹配 workflow_mode=%s targets_mode=%s", mode, opts.Mode)
		}
		payload := types.WorkflowPayload{
			WorkflowID:       spec.WorkflowID,
			Mode:             opts.Mode,
			Node:             node.Name,
			TagTypes:         opts.TagTypes,
			UIDs:             opts.UIDs,
			ShardIndex:       shardIndex,
			ShardTotal:       shardTotal,
			DryRun:           opts.DryRun,
			SyncSnapshotOnly: opts.SyncSnapshotOnly,
		}
		flags := userTagTargetFlags(spec.Targets)
		if node.Name == types.NodeDispatchHooks || node.Name == types.NodeEventOutboxRetryScan || flags.BatchSize {
			payload.BatchSize = opts.BatchSize
		}
		if flags.WorkerCount {
			payload.WorkerCount = opts.WorkerCount
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return data, nil
	}
}

// userTagTargetFlagSet 标记 targets 中显式传入的运行参数。
type userTagTargetFlagSet struct {
	BatchSize   bool // 是否显式指定 batch_size
	WorkerCount bool // 是否显式指定 worker_count
}

// userTagTargetFlags 提取需要固化到节点 payload 的显式运行参数。
func userTagTargetFlags(targets []string) userTagTargetFlagSet {
	flags := userTagTargetFlagSet{}
	for _, target := range targets {
		key, _, ok := strings.Cut(strings.TrimSpace(target), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "batch_size", "batchsize":
			flags.BatchSize = true
		case "worker_count", "workercount":
			flags.WorkerCount = true
		}
	}
	return flags
}
