package logic

import (
	"testing"
	"time"

	"admin/internal/model"
)

// TestBuildAdminRoleTree 验证角色模型到接口树节点的通用映射。
func TestBuildAdminRoleTree(t *testing.T) {
	now := time.Date(2026, 6, 23, 1, 2, 3, 0, time.Local)
	tree := BuildAdminRoleTree([]model.AdminRole{
		{ID: 1, Title: "父角色", Status: 1, IsDelete: 0, CreatedAt: now, UpdatedAt: now},
		{ID: 2, Title: "子角色", Pid: 1, Pids: "1", Status: 0, IsDelete: 0, CreatedAt: now, UpdatedAt: now},
	}, map[int][]int{1: {10, 11}})

	if len(tree) != 1 {
		t.Fatalf("角色树根节点数量 = %d, want 1", len(tree))
	}
	root := tree[0]
	if root.ID != 1 || root.Title != "父角色" || root.Disabled || !root.Selectable {
		t.Fatalf("根角色映射异常: %+v", root)
	}
	if len(root.Permissions) != 2 || root.Permissions[0] != 10 || root.Permissions[1] != 11 {
		t.Fatalf("根角色权限映射异常: %+v", root.Permissions)
	}
	if len(root.Children) != 1 {
		t.Fatalf("子角色数量 = %d, want 1", len(root.Children))
	}
	child := root.Children[0]
	if child.ID != 2 || !child.Disabled || !child.DisableCheckbox || child.Selectable {
		t.Fatalf("禁用子角色映射异常: %+v", child)
	}
}

// TestBuildAdminPermissionTree 验证权限模型到接口树节点的通用映射。
func TestBuildAdminPermissionTree(t *testing.T) {
	now := time.Date(2026, 6, 23, 1, 2, 3, 0, time.Local)
	checked := map[int]struct{}{2: {}}
	disabled := map[int]struct{}{2: {}}
	tree := BuildAdminPermissionTree([]model.AdminPermission{
		{ID: 1, Title: "系统管理", UUID: "100001", Status: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 2, Title: "角色新增", UUID: "100002", Pid: 1, Pids: "1", Status: 1, CreatedAt: now, UpdatedAt: now},
	}, checked, disabled)

	if len(tree) != 1 || len(tree[0].Children) != 1 {
		t.Fatalf("权限树结构异常: %+v", tree)
	}
	child := tree[0].Children[0]
	if child.ID != 2 || child.UUID != "100002" || !child.Checked || !child.Disabled || child.Selectable {
		t.Fatalf("权限子节点映射异常: %+v", child)
	}
}
