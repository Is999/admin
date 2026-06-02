package route

import "testing"

// TestShardPlanRoutesUID 验证同一 UID 的分片路由稳定可复现。
func TestShardPlanRoutesUID(t *testing.T) {
	plan := NewShardPlan(10, 10)
	if shard := plan.UIDShard(12345, 10); shard != 5 {
		t.Fatalf("unexpected uid shard: %d", shard)
	}
}

// TestPhysicalShardsForWorkflowLowerWorkflowTotal 验证工作流分片少于物理分表时不会漏扫同余分表。
func TestPhysicalShardsForWorkflowLowerWorkflowTotal(t *testing.T) {
	plan := NewShardPlan(5, 10)
	shards := plan.physicalShardsForWorkflow(2, 5, plan.RuntimeShardTotal)
	if len(shards) != 2 || shards[0] != 2 || shards[1] != 7 {
		t.Fatalf("unexpected runtime shards: %#v", shards)
	}
}

// TestPhysicalShardsForWorkflowHigherWorkflowTotal 验证工作流分片多于物理分表时仍能定位唯一物理分表。
func TestPhysicalShardsForWorkflowHigherWorkflowTotal(t *testing.T) {
	plan := NewShardPlan(20, 10)
	shards := plan.physicalShardsForWorkflow(13, 20, plan.RuntimeShardTotal)
	if len(shards) != 1 || shards[0] != 3 {
		t.Fatalf("unexpected runtime shards: %#v", shards)
	}
}

// TestPhysicalShardsForWorkflowCoprime 验证互质分片配置下每个工作流分片需要覆盖全部物理分表。
func TestPhysicalShardsForWorkflowCoprime(t *testing.T) {
	plan := NewShardPlan(3, 10)
	shards := plan.physicalShardsForWorkflow(1, 3, plan.RuntimeShardTotal)
	if len(shards) != 10 {
		t.Fatalf("unexpected coprime runtime shards: %#v", shards)
	}
}

// TestTagShardsForWorkflowCoprime 验证标签结果分片也使用同一套物理分表覆盖规则。
func TestTagShardsForWorkflowCoprime(t *testing.T) {
	plan := NewShardPlan(10, 10)
	shards := plan.TagShardsForWorkflow(1, 3)
	if len(shards) != 1000 {
		t.Fatalf("unexpected coprime tag shards: %#v", shards)
	}
}

// TestIndexedUIDConditionPrefersShardNo 验证中转索引表在分片一致时优先走 shard_no。
func TestIndexedUIDConditionPrefersShardNo(t *testing.T) {
	plan := NewShardPlan(10, 10)
	condition, err := plan.IndexedUIDCondition("uid", "shard_no", Shard{Index: 3, Total: 10})
	if err != nil {
		t.Fatalf("indexed condition failed: %v", err)
	}
	if condition.Expr != "shard_no = ?" || len(condition.Args) != 1 || condition.Args[0] != 3 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

// TestIndexedUIDConditionUsesShardNoSet 验证工作流分片可映射到 1000 分片索引集合。
func TestIndexedUIDConditionUsesShardNoSet(t *testing.T) {
	plan := NewShardPlanWithResult(10, 1000, 1000)
	condition, err := plan.IndexedUIDCondition("uid", "shard_no", Shard{Index: 3, Total: 10})
	if err != nil {
		t.Fatalf("indexed condition failed: %v", err)
	}
	if condition.Expr != "shard_no IN ?" || len(condition.Args) != 1 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
	values, ok := condition.Args[0].([]int)
	if !ok || len(values) != 100 || values[0] != 3 || values[99] != 993 {
		t.Fatalf("unexpected shard values: %#v", condition.Args)
	}
}

// TestIndexedUIDConditionFallbackModulo 验证分片不一致时回退 UID 取模兼容。
func TestIndexedUIDConditionFallbackModulo(t *testing.T) {
	plan := NewShardPlan(20, 10)
	condition, err := plan.IndexedUIDCondition("uid", "shard_no", Shard{Index: 13, Total: 20})
	if err != nil {
		t.Fatalf("indexed condition failed: %v", err)
	}
	if condition.Expr != "MOD(uid, ?) = ?" || len(condition.Args) != 2 || condition.Args[0] != 20 || condition.Args[1] != 13 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

// TestShardConditionRejectsUnsafeColumn 验证分片条件拒绝危险列名。
func TestShardConditionRejectsUnsafeColumn(t *testing.T) {
	plan := NewShardPlan(10, 10)
	if _, err := plan.UIDModuloCondition("uid;drop table", Shard{Index: 0, Total: 10}); err == nil {
		t.Fatal("expected unsafe column error")
	}
}
