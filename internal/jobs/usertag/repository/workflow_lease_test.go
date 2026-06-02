package repository

import (
	"context"
	"strings"
	"testing"

	"admin/internal/jobs/usertag/types"
)

// TestWorkflowLeaseRequiresRepositoryAndRedis 验证租约依赖缺失时返回错误而不是静默跳过。
func TestWorkflowLeaseRequiresRepositoryAndRedis(t *testing.T) {
	cases := []struct {
		name string         // name 表示测试场景名称。
		repo *TagRepository // repo 表示测试字段。
		want string         // want 表示期望结果。
	}{
		{name: "nil-repository", repo: nil, want: "仓储未初始化"},
		{name: "missing-redis", repo: NewTagRepository(RuntimeDeps{}), want: "Redis 未初始化"},
	}
	for _, tc := range cases {
		err := tc.repo.AcquireWorkflowLease(context.Background(), types.RuntimeOptions{
			WorkflowID: "workflow-1",
			Mode:       types.ModeFull,
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error contains %q, got %v", tc.name, tc.want, err)
		}
	}
}
