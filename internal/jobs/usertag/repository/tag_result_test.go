package repository

import (
	"testing"

	"admin/internal/jobs/usertag/route"
	"admin/internal/jobs/usertag/types"
)

// TestWorkflowShardUIDsFiltersCurrentShard 验证骨架仓储仍按工作流分片过滤 UID。
func TestWorkflowShardUIDsFiltersCurrentShard(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10, 10)))
	uids := repo.WorkflowShardUIDs(types.RuntimeOptions{ShardIndex: 1, ShardTotal: 2}, []int64{1, 2, 3, 4, 3, 0})
	if len(uids) != 2 || uids[0] != 1 || uids[1] != 3 {
		t.Fatalf("unexpected shard uids: %#v", uids)
	}
}
