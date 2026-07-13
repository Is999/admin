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
	if got.ResultShardTotal != 1 {
		t.Fatalf("ResultShardTotal=%d want=1", got.ResultShardTotal)
	}
}

// TestNewDefaultsKeepsLogicalResultTable 验证工作分片数不会改变 Proxy 逻辑结果表。
func TestNewDefaultsKeepsLogicalResultTable(t *testing.T) {
	got := NewDefaults(config.UserTagConfig{
		DefaultShardTotal: 8,
	})
	if got.ShardTotal != 8 || got.ResultShardTotal != 1 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}
