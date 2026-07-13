package route

import (
	"testing"

	"admin/common/idgen"
)

// TestShardPlanRoutesUID 验证同一 UID 的分片路由稳定可复现。
func TestShardPlanRoutesUID(t *testing.T) {
	plan := NewShardPlan(10)
	if shard := plan.UIDShard(12345, 10); shard != idgen.ShardNo(12345)*10/idgen.ShardMod {
		t.Fatalf("unexpected uid shard: %d", shard)
	}
}

// TestResultTableUsesIndependentPhysicalCount 验证标签结果表不跟随 user 物理分片数。
func TestResultTableUsesIndependentPhysicalCount(t *testing.T) {
	plan := NewShardPlanWithResult(8, 4)
	var uid int64
	for candidate := int64(1); candidate < 100000; candidate++ {
		if idgen.ShardNo(candidate) >= 768 {
			uid = candidate
			break
		}
	}
	table, err := plan.ResultTable(uid)
	if err != nil {
		t.Fatalf("ResultTable() error = %v", err)
	}
	if table != "user_tag_b0768" {
		t.Fatalf("ResultTable() = %q, want user_tag_b0768", table)
	}
}

// TestIndexedUIDConditionPrefersShardNo 验证中转索引表在分片一致时优先走 shard_no。
func TestIndexedUIDConditionPrefersShardNo(t *testing.T) {
	plan := NewShardPlan(1024)
	condition := plan.IndexedUIDCondition(Shard{Index: 3, Total: 1024})
	if condition.Expr != "shard_no = ?" || len(condition.Args) != 1 || condition.Args[0] != 3 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

// TestIndexedUIDConditionUsesShardNoRange 验证工作流分片映射为连续固定桶范围。
func TestIndexedUIDConditionUsesShardNoRange(t *testing.T) {
	plan := NewShardPlanWithResult(8, 1024)
	condition := plan.IndexedUIDCondition(Shard{Index: 3, Total: 8})
	if condition.Expr != "shard_no BETWEEN ? AND ?" || len(condition.Args) != 2 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
	if condition.Args[0] != 384 || condition.Args[1] != 511 {
		t.Fatalf("unexpected shard values: %#v", condition.Args)
	}
}

// TestIndexedUIDConditionSplitsNonDivisibleBuckets 验证分片不整除时仍连续且完整覆盖固定桶。
func TestIndexedUIDConditionSplitsNonDivisibleBuckets(t *testing.T) {
	plan := NewShardPlan(20)
	condition := plan.IndexedUIDCondition(Shard{Index: 13, Total: 20})
	if condition.Expr != "shard_no BETWEEN ? AND ?" || len(condition.Args) != 2 || condition.Args[0] != 666 || condition.Args[1] != 716 {
		t.Fatalf("unexpected condition: %#v", condition)
	}
}

// TestShardBucketRangesCoverAllBuckets 验证任意允许的工作分片数都连续且无重叠覆盖固定桶。
func TestShardBucketRangesCoverAllBuckets(t *testing.T) {
	plan := NewShardPlan(1024)
	for total := 1; total <= idgen.ShardMod; total++ {
		next := 0
		for index := 0; index < total; index++ {
			start, end := plan.shardBucketRange(Shard{Index: index, Total: total})
			if start != next || end < start || end >= idgen.ShardMod {
				t.Fatalf("total=%d index=%d range=%d-%d next=%d", total, index, start, end, next)
			}
			next = end + 1
		}
		if next != idgen.ShardMod {
			t.Fatalf("total=%d covered=%d buckets, want=%d", total, next, idgen.ShardMod)
		}
		for bucket := 0; bucket < idgen.ShardMod; bucket++ {
			index := bucket * total / idgen.ShardMod
			start, end := plan.shardBucketRange(Shard{Index: index, Total: total})
			if bucket < start || bucket > end {
				t.Fatalf("total=%d bucket=%d routed=%d range=%d-%d", total, bucket, index, start, end)
			}
		}
	}
}
