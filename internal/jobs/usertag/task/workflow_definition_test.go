package task

import (
	"testing"
	"time"

	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/types"
)

// TestWorkflowDefinitionsFinalizeDisablesRetryOverride 验证 full 切表节点禁止被全局重试覆盖。
func TestWorkflowDefinitionsFinalizeDisablesRetryOverride(t *testing.T) {
	defs := WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	foundFull := false
	for _, def := range defs {
		node := def.Nodes[types.NodeFinalize]
		if node == nil {
			t.Fatalf("用户标签工作流 %s 缺少 finalize 节点", def.Name)
		}
		if def.Name == WorkflowNameUserTagFull && node.MaxRetry >= 0 {
			t.Fatalf("期望 finalize 禁止自动重试，实际 MaxRetry=%d", node.MaxRetry)
		}
		if def.Name != WorkflowNameUserTagFull && node.MaxRetry < 0 {
			t.Fatalf("只有 full finalize 才应禁止自动重试，workflow=%s MaxRetry=%d", def.Name, node.MaxRetry)
		}
		if def.Name == WorkflowNameUserTagFull {
			foundFull = true
		}
	}
	if !foundFull {
		t.Fatal("未找到用户标签 full 工作流定义")
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

// TestWorkflowDefinitionsDoNotSetMainNodeTimeout 验证用户标签主计算节点不带固定超时，避免生产大数据量反复超时重跑。
func TestWorkflowDefinitionsDoNotSetMainNodeTimeout(t *testing.T) {
	for _, def := range WorkflowDefinitions(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2}) {
		for name, node := range def.Nodes {
			if node.Timeout != 0 {
				t.Fatalf("workflow=%s node=%s Timeout=%s want no timeout", def.Name, name, node.Timeout)
			}
		}
	}
}

// TestRuntimeCleanupWorkflowDefinitionSingleNode 验证运行期清理是独立单节点工作流，避免误接入用户标签主 DAG。
func TestRuntimeCleanupWorkflowDefinitionSingleNode(t *testing.T) {
	def := RuntimeCleanupWorkflowDefinition(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if def.Name != WorkflowNameUserTagRuntimeCleanup {
		t.Fatalf("cleanup workflow name=%s want=%s", def.Name, WorkflowNameUserTagRuntimeCleanup)
	}
	if def.MaxShardTotal != 1 {
		t.Fatalf("cleanup workflow should be single shard, MaxShardTotal=%d", def.MaxShardTotal)
	}
	if len(def.Nodes) != 1 {
		t.Fatalf("cleanup workflow should only have one node, got=%d", len(def.Nodes))
	}
	node := def.Nodes[types.NodeRuntimeCleanup]
	if node == nil {
		t.Fatal("cleanup workflow missing runtime cleanup node")
	}
	if node.TaskType != TaskTypeUserTagRuntimeCleanup {
		t.Fatalf("cleanup task type=%s want=%s", node.TaskType, TaskTypeUserTagRuntimeCleanup)
	}
	if node.Timeout != 10*time.Minute {
		t.Fatalf("cleanup timeout=%s want=10m", node.Timeout)
	}
	if node.SupportsSharding {
		t.Fatal("cleanup workflow should not enable sharding")
	}
}

// TestKafkaOutboxRetryScanWorkflowDefinitionSingleNode 验证 Kafka outbox 异常扫描是独立单节点工作流。
func TestKafkaOutboxRetryScanWorkflowDefinitionSingleNode(t *testing.T) {
	def := KafkaOutboxRetryScanWorkflowDefinition(Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if def.Name != WorkflowNameUserTagKafkaOutboxRetryScan {
		t.Fatalf("retry scan workflow name=%s want=%s", def.Name, WorkflowNameUserTagKafkaOutboxRetryScan)
	}
	if def.MaxShardTotal != 1 {
		t.Fatalf("retry scan workflow should be single shard, MaxShardTotal=%d", def.MaxShardTotal)
	}
	if len(def.Nodes) != 1 {
		t.Fatalf("retry scan workflow should only have one node, got=%d", len(def.Nodes))
	}
	node := def.Nodes[types.NodeKafkaOutboxRetryScan]
	if node == nil {
		t.Fatal("retry scan workflow missing retry scan node")
	}
	if node.TaskType != TaskTypeUserTagKafkaOutboxRetry {
		t.Fatalf("retry scan task type=%s want=%s", node.TaskType, TaskTypeUserTagKafkaOutboxRetry)
	}
	if node.Timeout != 9*time.Minute {
		t.Fatalf("retry scan timeout=%s want=9m", node.Timeout)
	}
	if node.SupportsSharding {
		t.Fatal("retry scan workflow should not enable sharding")
	}
}
