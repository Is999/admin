package task

import (
	"encoding/json"
	"testing"
	"time"

	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/types"
	"admin/internal/task/queue"
)

// TestWorkflowDefinitionsRegisterCompleteSkeletonDAG 验证主工作流注册完整用户标签通用骨架节点。
func TestWorkflowDefinitionsRegisterCompleteSkeletonDAG(t *testing.T) {
	defs := workflowDefinitionsByName(WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2, EventBatchSize: 50}))
	for _, workflowName := range []string{WorkflowNameUserTagFull, WorkflowNameUserTagDelta, WorkflowNameUserTagTargeted, WorkflowNameUserTagRecalculate} {
		def := defs[workflowName]
		if def == nil {
			t.Fatalf("工作流未注册: %s", workflowName)
		}
		for _, nodeName := range mainSkeletonNodes() {
			if def.Nodes[nodeName] == nil {
				t.Fatalf("workflow=%s 缺少骨架节点 %s", workflowName, nodeName)
			}
		}
	}
}

// TestWorkflowDefinitionsDAGDependencies 验证主 DAG 依赖顺序不遗漏。
func TestWorkflowDefinitionsDAGDependencies(t *testing.T) {
	defs := workflowDefinitionsByName(WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2}))
	fullDef := defs[WorkflowNameUserTagFull]
	assertDependsOn(t, fullDef, types.NodeCollectScope, types.NodePrepare)
	assertDependsOn(t, fullDef, types.NodeEvaluateTags, types.NodeCollectScope)
	assertDependsOn(t, fullDef, types.NodeResolveChanges, types.NodeEvaluateTags)
	assertDependsOn(t, fullDef, types.NodePersistResults, types.NodeResolveChanges)
	assertDependsOn(t, fullDef, types.NodeFinalize, types.NodePersistResults)
	assertDependsOn(t, fullDef, types.NodeDispatchHooks, types.NodeFinalize)
}

// TestWorkflowDefinitionsRetryAndShardingRules 验证执行模式的重试和分片规则。
func TestWorkflowDefinitionsRetryAndShardingRules(t *testing.T) {
	defs := workflowDefinitionsByName(WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2}))
	fullDef := defs[WorkflowNameUserTagFull]
	if fullDef.Nodes[types.NodeFinalize].MaxRetry >= 0 {
		t.Fatalf("full finalize 应禁止全局自动重试，实际 MaxRetry=%d", fullDef.Nodes[types.NodeFinalize].MaxRetry)
	}
	for _, workflowName := range []string{WorkflowNameUserTagDelta, WorkflowNameUserTagTargeted, WorkflowNameUserTagRecalculate} {
		def := defs[workflowName]
		if def.Nodes[types.NodeFinalize].MaxRetry < 0 {
			t.Fatalf("非 full 工作流不应禁用 finalize 重试 workflow=%s", workflowName)
		}
	}
	targetedDef := defs[WorkflowNameUserTagTargeted]
	for _, nodeName := range []string{types.NodeCollectScope, types.NodeEvaluateTags, types.NodeResolveChanges, types.NodePersistResults, types.NodeDispatchHooks} {
		if targetedDef.Nodes[nodeName].SupportsSharding {
			t.Fatalf("targeted 节点不应启用分片 workflow=%s node=%s", targetedDef.Name, nodeName)
		}
	}
}

// TestWorkflowDefinitionsUseExplicitMainNodeTimeout 验证主计算节点有显式长超时，避免被默认超时截断。
func TestWorkflowDefinitionsUseExplicitMainNodeTimeout(t *testing.T) {
	for _, def := range WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2}) {
		for name, node := range def.Nodes {
			if node.Timeout != userTagMainWorkflowNodeTimeout {
				t.Fatalf("workflow=%s node=%s Timeout=%s want=%s", def.Name, name, node.Timeout, userTagMainWorkflowNodeTimeout)
			}
		}
	}
}

// TestWorkflowDefinitionsLimitShardTotal 验证用户标签工作流定义自身带分片上限，防止绕过 API 直接放大任务数。
func TestWorkflowDefinitionsLimitShardTotal(t *testing.T) {
	for _, def := range WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2}) {
		if def.MaxShardTotal != options.MaxShardTotal {
			t.Fatalf("workflow=%s MaxShardTotal=%d want=%d", def.Name, def.MaxShardTotal, options.MaxShardTotal)
		}
	}
}

// TestWorkflowDefinitionPayloadNormalizesTargets 验证工作流节点 payload 会按运行参数解析和固化。
func TestWorkflowDefinitionPayloadNormalizesTargets(t *testing.T) {
	defs := workflowDefinitionsByName(WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2, EventBatchSize: 50}))
	node := defs[WorkflowNameUserTagTargeted].Nodes[types.NodeCollectScope]
	raw, err := node.BuildPayload(taskqueue.WorkflowStartSpec{
		WorkflowID: "wf-targeted",
		Targets: []string{
			"uid=101",
			"uid=102",
			"tag_type=7",
			"tag_type=8",
			"batch_size=30",
			"worker_count=3",
		},
	}, node, 0, 1)
	if err != nil {
		t.Fatalf("BuildPayload() error = %v", err)
	}
	var payload types.WorkflowPayload
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload unmarshal failed: %v", err)
	}
	if payload.Mode != types.ModeTargeted || payload.Node != types.NodeCollectScope || payload.BatchSize != 30 || payload.WorkerCount != 3 {
		t.Fatalf("payload 未按 targets 固化: %+v", payload)
	}
	if len(payload.UIDs) != 2 || len(payload.TagTypes) != 2 {
		t.Fatalf("payload targeted 参数缺失: %+v", payload)
	}
}

// TestRuntimeCleanupWorkflowDefinitionSingleNode 验证运行期清理是独立单节点工作流，避免误接入用户标签主 DAG。
func TestRuntimeCleanupWorkflowDefinitionSingleNode(t *testing.T) {
	def := RuntimeCleanupWorkflowDefinition(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if def.Name != WorkflowNameUserTagRuntimeCleanup {
		t.Fatalf("cleanup workflow name=%s want=%s", def.Name, WorkflowNameUserTagRuntimeCleanup)
	}
	assertSingleNodeWorkflow(t, def, types.NodeRuntimeCleanup, TaskTypeUserTagRuntimeCleanup, userTagRuntimeCleanupNodeTimeout)
}

// TestEventOutboxRetryScanWorkflowDefinitionSingleNode 验证事件 outbox 异常扫描是独立单节点工作流。
func TestEventOutboxRetryScanWorkflowDefinitionSingleNode(t *testing.T) {
	def := EventOutboxRetryScanWorkflowDefinition(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if def.Name != WorkflowNameUserTagEventOutboxRetryScan {
		t.Fatalf("retry scan workflow name=%s want=%s", def.Name, WorkflowNameUserTagEventOutboxRetryScan)
	}
	assertSingleNodeWorkflow(t, def, types.NodeEventOutboxRetryScan, TaskTypeUserTagEventOutboxRetry, userTagEventOutboxRetryNodeTimeout)
}

// workflowDefinitionsByName 按工作流名称索引定义，便于断言具体 DAG。
func workflowDefinitionsByName(defs []*taskqueue.WorkflowDefinition) map[string]*taskqueue.WorkflowDefinition {
	out := make(map[string]*taskqueue.WorkflowDefinition, len(defs))
	for _, def := range defs {
		out[def.Name] = def
	}
	return out
}

// mainSkeletonNodes 返回主 DAG 期望注册的骨架节点。
func mainSkeletonNodes() []string {
	return []string{
		types.NodePrepare,
		types.NodeCollectScope,
		types.NodeEvaluateTags,
		types.NodeResolveChanges,
		types.NodePersistResults,
		types.NodeFinalize,
		types.NodeDispatchHooks,
	}
}

// assertDependsOn 校验节点依赖和期望完全一致。
func assertDependsOn(t *testing.T, def *taskqueue.WorkflowDefinition, nodeName string, want ...string) {
	t.Helper()
	if def == nil || def.Nodes[nodeName] == nil {
		t.Fatalf("workflow 缺少节点 node=%s", nodeName)
	}
	got := def.Nodes[nodeName].DependsOn
	if len(got) != len(want) {
		t.Fatalf("node=%s depends=%v want=%v", nodeName, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("node=%s depends=%v want=%v", nodeName, got, want)
		}
	}
}

// assertSingleNodeWorkflow 校验独立维护工作流只有一个指定节点。
func assertSingleNodeWorkflow(t *testing.T, def *taskqueue.WorkflowDefinition, nodeName string, taskType string, timeout time.Duration) {
	t.Helper()
	if def.MaxShardTotal != 1 {
		t.Fatalf("workflow=%s should be single shard, MaxShardTotal=%d", def.Name, def.MaxShardTotal)
	}
	if len(def.Nodes) != 1 {
		t.Fatalf("workflow=%s should only have one node, got=%d", def.Name, len(def.Nodes))
	}
	node := def.Nodes[nodeName]
	if node == nil {
		t.Fatalf("workflow=%s missing node=%s", def.Name, nodeName)
	}
	if node.TaskType != taskType {
		t.Fatalf("node=%s task type=%s want=%s", nodeName, node.TaskType, taskType)
	}
	if node.Timeout != timeout {
		t.Fatalf("node=%s timeout=%s want=%s", nodeName, node.Timeout, timeout)
	}
	if node.SupportsSharding {
		t.Fatalf("workflow=%s node=%s should not enable sharding", def.Name, nodeName)
	}
}
