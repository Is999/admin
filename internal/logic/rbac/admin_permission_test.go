package rbac

import (
	"testing"

	"admin/internal/types"
)

// TestMarkPermissionTreeManageScope 验证权限树会按可管理范围标记节点状态。
func TestMarkPermissionTreeManageScope(t *testing.T) {
	items := []types.AdminPermissionItem{
		{
			ID:         1,
			Title:      "目录A",
			Status:     1,
			Selectable: true,
			Children: []types.AdminPermissionItem{
				{
					ID:         2,
					Title:      "按钮A-1",
					Status:     1,
					Selectable: true,
				},
				{
					ID:         3,
					Title:      "按钮A-2",
					Status:     0,
					Selectable: true,
				},
			},
		},
		{
			ID:         4,
			Title:      "目录B",
			Status:     1,
			Selectable: true,
		},
	}

	got := markPermissionTreeManageScope(items, map[int]struct{}{
		1: {},
		2: {},
	})
	if len(got) != 2 {
		t.Fatalf("markPermissionTreeManageScope() len = %d, want 2", len(got))
	}
	if got[0].Disabled || got[0].DisableCheckbox || !got[0].Selectable {
		t.Fatalf("permission 1 state mismatch: %+v", got[0])
	}
	if got[0].Children[0].Disabled || got[0].Children[0].DisableCheckbox || !got[0].Children[0].Selectable {
		t.Fatalf("permission 2 state mismatch: %+v", got[0].Children[0])
	}
	if !got[0].Children[1].Disabled || !got[0].Children[1].DisableCheckbox || got[0].Children[1].Selectable {
		t.Fatalf("permission 3 state mismatch: %+v", got[0].Children[1])
	}
	if !got[1].Disabled || !got[1].DisableCheckbox || got[1].Selectable {
		t.Fatalf("permission 4 state mismatch: %+v", got[1])
	}
}

// TestMarkPermissionTreeCheckedDisablesOutOfScopeCheckedNode 验证历史越权但已勾选的权限节点会被锁定，避免继续误操作。
func TestMarkPermissionTreeCheckedDisablesOutOfScopeCheckedNode(t *testing.T) {
	items := []types.AdminPermissionItem{
		{
			ID:         1,
			Title:      "目录A",
			Status:     1,
			Selectable: true,
			Children: []types.AdminPermissionItem{
				{
					ID:         2,
					Title:      "按钮A-1",
					Status:     1,
					Selectable: true,
				},
			},
		},
	}

	got := markPermissionTreeChecked(
		items,
		map[int]struct{}{2: {}},
		map[int]struct{}{1: {}},
		false,
	)

	if got[0].Disabled || got[0].DisableCheckbox || !got[0].Selectable {
		t.Fatalf("permission 1 should remain usable: %+v", got[0])
	}
	child := got[0].Children[0]
	if !child.Checked {
		t.Fatalf("permission 2 should keep checked state for display: %+v", child)
	}
	if !child.Disabled || !child.DisableCheckbox || child.Selectable {
		t.Fatalf("permission 2 should be locked when out of assignable scope: %+v", child)
	}
}

// TestResolvePermissionUpdateParentID 验证权限编辑时的父级 ID 父级保留语义。
func TestResolvePermissionUpdateParentID(t *testing.T) {
	tests := []struct {
		name       string
		currentPid int
		requestPid int
		want       int
	}{
		{
			name:       "existing root remains root when request pid is zero",
			currentPid: 0,
			requestPid: 0,
			want:       0,
		},
		{
			name:       "non root keeps current parent when request pid is omitted as zero",
			currentPid: 65,
			requestPid: 0,
			want:       65,
		},
		{
			name:       "non root moves to explicit positive parent",
			currentPid: 65,
			requestPid: 99,
			want:       99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePermissionUpdateParentID(tt.currentPid, tt.requestPid); got != tt.want {
				t.Fatalf("resolvePermissionUpdateParentID(%d, %d) = %d, want %d", tt.currentPid, tt.requestPid, got, tt.want)
			}
		})
	}
}

// TestPermissionParentChanged 验证只有真实迁移父级时才触发父级可管理范围校验。
func TestPermissionParentChanged(t *testing.T) {
	tests := []struct {
		name       string
		currentPid int
		nextPid    int
		want       bool
	}{
		{
			name:       "root edit does not count as moving to root",
			currentPid: 0,
			nextPid:    0,
			want:       false,
		},
		{
			name:       "same parent edit does not recheck parent scope",
			currentPid: 65,
			nextPid:    65,
			want:       false,
		},
		{
			name:       "positive parent change needs scope check",
			currentPid: 65,
			nextPid:    99,
			want:       true,
		},
		{
			name:       "root moves to positive parent needs scope check",
			currentPid: 0,
			nextPid:    99,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permissionParentChanged(tt.currentPid, tt.nextPid); got != tt.want {
				t.Fatalf("permissionParentChanged(%d, %d) = %t, want %t", tt.currentPid, tt.nextPid, got, tt.want)
			}
		})
	}
}
