package rbac

import (
	"context"
	"testing"

	"admin/common/codes"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestMaxUUIDFailsWithoutDatabase 验证数据库未初始化时不会伪造下一个权限 UUID。
func TestMaxUUIDFailsWithoutDatabase(t *testing.T) {
	logicObj := &AdminPermissionLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(
			context.Background(),
			svc.NewServiceContext(config.Config{}, svc.Dependencies{}),
		),
	}
	result := logicObj.MaxUUID()
	if result == nil || result.Code != codes.DBError {
		t.Fatalf("MaxUUID() = %+v, want database error", result)
	}
	if result.Error == nil {
		t.Fatal("MaxUUID() must retain the database initialization error")
	}
}

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
	if got[0].Disabled || got[0].DisableCheckbox || !got[0].Selectable || !got[0].Manageable || !got[0].CanCreateChild {
		t.Fatalf("permission 1 state mismatch: %+v", got[0])
	}
	if got[0].Children[0].Disabled || got[0].Children[0].DisableCheckbox || !got[0].Children[0].Selectable ||
		!got[0].Children[0].Manageable || !got[0].Children[0].CanCreateChild {
		t.Fatalf("permission 2 state mismatch: %+v", got[0].Children[0])
	}
	if !got[0].Children[1].Disabled || !got[0].Children[1].DisableCheckbox || got[0].Children[1].Selectable ||
		got[0].Children[1].Manageable || got[0].Children[1].CanCreateChild {
		t.Fatalf("permission 3 state mismatch: %+v", got[0].Children[1])
	}
	if !got[1].Disabled || !got[1].DisableCheckbox || got[1].Selectable || got[1].Manageable || got[1].CanCreateChild {
		t.Fatalf("permission 4 state mismatch: %+v", got[1])
	}
}

// TestMarkPermissionTreeManageScopeDisablesCreateBelowDisabledAncestor 验证禁用父级下不能新增子权限。
func TestMarkPermissionTreeManageScopeDisablesCreateBelowDisabledAncestor(t *testing.T) {
	items := []types.AdminPermissionItem{{
		ID:     1,
		Status: 0,
		Children: []types.AdminPermissionItem{{
			ID:     2,
			Status: 1,
		}},
	}}
	got := markPermissionTreeManageScope(items, map[int]struct{}{1: {}, 2: {}})
	if got[0].CanCreateChild || got[0].Children[0].CanCreateChild {
		t.Fatalf("disabled path must not allow child creation: %+v", got)
	}
	if !got[0].Children[0].Manageable {
		t.Fatalf("enabled child should keep management permission: %+v", got[0].Children[0])
	}
}

// TestPermissionItemWithinScope 验证权限自身和祖先权限均可建立管理范围。
func TestPermissionItemWithinScope(t *testing.T) {
	item := types.AdminPermissionItem{ID: 3, Pids: "1,2"}
	if !permissionItemWithinScope(item, map[int]struct{}{2: {}}) {
		t.Fatal("ancestor permission should include descendant in scope")
	}
	if !permissionItemWithinScope(item, map[int]struct{}{3: {}}) {
		t.Fatal("permission should include itself in scope")
	}
	if permissionItemWithinScope(item, map[int]struct{}{4: {}}) {
		t.Fatal("unrelated permission must stay out of scope")
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

// TestPermissionPidsFromHierarchy 验证权限族谱按真实 pid 链生成，不信任旧 pids。
func TestPermissionPidsFromHierarchy(t *testing.T) {
	permissions := []model.AdminPermission{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 5, Pid: 1, Pids: "stale"},
		{ID: 6, Pid: 5, Pids: "stale"},
	}
	got, err := permissionPidsFromHierarchy(permissions, 6, 9)
	if err != nil {
		t.Fatalf("permissionPidsFromHierarchy() error = %v", err)
	}
	if got != "1,5,6" {
		t.Fatalf("permissionPidsFromHierarchy() = %q, want %q", got, "1,5,6")
	}
}

// TestDisabledPermissionPathID 校验启用权限时会识别真实父级链上的禁用节点。
func TestDisabledPermissionPathID(t *testing.T) {
	permissions := []model.AdminPermission{
		{ID: 1, Pid: 0, Status: 1},
		{ID: 2, Pid: 1, Status: 0},
		{ID: 3, Pid: 2, Status: 1},
	}
	if got, err := disabledPermissionPathID(permissions, 3); err != nil || got != 2 {
		t.Fatalf("disabledPermissionPathID() = (%d, %v), want (2, nil)", got, err)
	}
	if got, err := disabledPermissionPathID(permissions, 1); err != nil || got != 0 {
		t.Fatalf("disabledPermissionPathID(root) = (%d, %v), want (0, nil)", got, err)
	}
}

// TestAllEnabledPermissionIDsUsesCache 验证全量启用权限 ID 命中 Redis 时不查询数据库。
func TestAllEnabledPermissionIDsUsesCache(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminPermissionLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(
			context.Background(),
			svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}),
		),
	}
	cacheKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.PermissionUUID)
	if err := client.HSet(context.Background(), cacheKey, "7", "100007", "3", "100003").Err(); err != nil {
		t.Fatalf("写入权限 UUID 缓存失败: %v", err)
	}
	permissionIDs, err := logicObj.AllEnabledPermissionIDsWithCache()
	if err != nil {
		t.Fatalf("AllEnabledPermissionIDsWithCache() error=%v", err)
	}
	assertIntSetEqual(t, permissionIDs, []int{3, 7})
}

// TestDescendantPermissionPidsRebuildsMovedSubtree 验证权限换父级后全部子孙族谱同步切换。
func TestDescendantPermissionPidsRebuildsMovedSubtree(t *testing.T) {
	permissions := []model.AdminPermission{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 5, Pid: 1, Pids: "1"},
		{ID: 2, Pid: 5, Pids: "1,5"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 3, Pids: "1,2,3"},
		{ID: 8, Pid: 1, Pids: "1"},
	}
	got, err := descendantPermissionPids(permissions, 2)
	if err != nil {
		t.Fatalf("descendantPermissionPids() error = %v", err)
	}
	if got[3] != "1,5,2" || got[4] != "1,5,2,3" {
		t.Fatalf("descendantPermissionPids() = %v", got)
	}
	if _, ok := got[8]; ok {
		t.Fatalf("descendantPermissionPids() should not include sibling permission: %v", got)
	}
}

// TestPermissionParentChanged 验证只有真实迁移父级时才触发父级可管理范围校验。
func TestPermissionParentChanged(t *testing.T) {
	tests := []struct {
		name       string // name 表示测试场景名称。
		currentPid int    // currentPid 表示测试字段。
		nextPid    int    // nextPid 表示测试字段。
		want       bool   // want 表示期望结果。
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
