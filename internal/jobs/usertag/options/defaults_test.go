package options

import (
	"testing"

	"admin/internal/config"
)

// TestNewDefaultsStartsFromSinglePhysicalResultShard 验证用户标签默认从单任务分片、单物理结果表起步。
func TestNewDefaultsStartsFromSinglePhysicalResultShard(t *testing.T) {
	got := NewDefaults(config.UserTagConfig{})
	if got.ShardTotal != 1 {
		t.Fatalf("ShardTotal=%d want=1", got.ShardTotal)
	}
	if got.RuntimeShardTotal != 1000 {
		t.Fatalf("RuntimeShardTotal=%d want=1000", got.RuntimeShardTotal)
	}
	if got.ResultShardTotal != 1 {
		t.Fatalf("ResultShardTotal=%d want=1", got.ResultShardTotal)
	}
}

// TestNewDefaultsKeepsConfiguredPhysicalResultShard 验证显式物理分表配置仍可平滑扩容。
func TestNewDefaultsKeepsConfiguredPhysicalResultShard(t *testing.T) {
	got := NewDefaults(config.UserTagConfig{
		DefaultShardTotal: 8,
		RuntimeShardTotal: 1000,
		ResultShardTotal:  100,
	})
	if got.ShardTotal != 8 || got.RuntimeShardTotal != 1000 || got.ResultShardTotal != 100 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}
