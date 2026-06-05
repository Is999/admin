package rbac

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/routealias"
	"context"
	"fmt"
	"testing"
	"time"

	codes "admin/common/codes"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	redislock "admin/internal/infra/redsync"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestRetainAssignablePermissionIDs 验证权限收敛时只保留父级允许范围内的权限。
func TestRetainAssignablePermissionIDs(t *testing.T) {
	got := retainAssignablePermissionIDs(
		[]int{5, 3, 3, 2, 1, 9},
		[]int{1, 3, 7},
	)
	want := []int{1, 3}
	if len(got) != len(want) {
		t.Fatalf("retainAssignablePermissionIDs() len = %d, want %d, got=%v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("retainAssignablePermissionIDs()[%d] = %d, want %d, got=%v", index, got[index], want[index], got)
		}
	}
}

// TestParentRoleUsesFullPermissionScope 验证超级管理员父级不依赖角色权限关系表。
func TestParentRoleUsesFullPermissionScope(t *testing.T) {
	tests := []struct {
		name         string // name 表示测试场景名称。
		parentRoleID int    // parentRoleID 表示测试字段。
		isSuperRole  bool   // isSuperRole 表示当前操作人是否超级管理员。
		want         bool   // want 表示期望结果。
	}{
		{name: "super edits super parent", parentRoleID: corelogic.AdminSuperRoleID, isSuperRole: true, want: true},
		{name: "super edits root parent", parentRoleID: 0, isSuperRole: true, want: true},
		{name: "normal sees super parent", parentRoleID: corelogic.AdminSuperRoleID, isSuperRole: false, want: false},
		{name: "normal sees root parent", parentRoleID: 0, isSuperRole: false, want: false},
		{name: "normal role", parentRoleID: 2, isSuperRole: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parentRoleUsesFullPermissionScope(tt.parentRoleID, tt.isSuperRole); got != tt.want {
				t.Fatalf("parentRoleUsesFullPermissionScope(%d, %t) = %v, want %v", tt.parentRoleID, tt.isSuperRole, got, tt.want)
			}
		})
	}
}

// TestManageableRoleSetFromExcludesOperatorRoles 验证普通管理员不能管理自己拥有的角色。
func TestManageableRoleSetFromExcludesOperatorRoles(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 3, Pids: "1,2,3"},
		{ID: 5, Pid: 1, Pids: "1"},
	}
	got := manageableRoleSetFrom(roles, []int{2}, false)
	for _, roleID := range []int{3, 4} {
		if _, ok := got[roleID]; !ok {
			t.Fatalf("普通管理员应可管理后代角色 %d，got=%v", roleID, got)
		}
	}
	for _, roleID := range []int{1, 2, 5} {
		if _, ok := got[roleID]; ok {
			t.Fatalf("普通管理员不应管理角色 %d，got=%v", roleID, got)
		}
	}
}

// TestAssignableRoleScopeOnlyAllowsDescendantRoles 验证给用户分配角色时只能选择当前管理员下级角色。
func TestAssignableRoleScopeOnlyAllowsDescendantRoles(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 3, Pids: "1,2,3"},
		{ID: 5, Pid: 1, Pids: "1"},
		{ID: 6, Pid: 5, Pids: "1,5"},
	}
	assignableRoleSet := manageableRoleSetFrom(roles, []int{2}, false)

	for _, roleID := range []int{3, 4} {
		if _, ok := assignableRoleSet[roleID]; !ok {
			t.Fatalf("用户赋权应允许下级角色 %d，got=%v", roleID, assignableRoleSet)
		}
	}
	for _, roleID := range []int{1, 2, 5, 6} {
		if _, ok := assignableRoleSet[roleID]; ok {
			t.Fatalf("用户赋权不应允许自身、上级、同级或其它分支角色 %d，got=%v", roleID, assignableRoleSet)
		}
	}
}

// TestManageableRoleSetFromOnlyAllowsDescendants 验证只能上级角色编辑下级角色权限。
func TestManageableRoleSetFromOnlyAllowsDescendants(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 3, Pids: "1,2,3"},
		{ID: 5, Pid: 2, Pids: "1,2"},
		{ID: 6, Pid: 1, Pids: "1"},
	}
	got := manageableRoleSetFrom(roles, []int{3}, false)
	if _, ok := got[4]; !ok {
		t.Fatalf("上级角色应可管理下级角色 4，got=%v", got)
	}
	for _, roleID := range []int{1, 2, 3, 5, 6} {
		if _, ok := got[roleID]; ok {
			t.Fatalf("不应允许编辑非下级角色 %d，got=%v", roleID, got)
		}
	}
}

// TestManageableRoleSetFromSuperIncludesAll 验证超级管理员可管理全部角色。
func TestManageableRoleSetFromSuperIncludesAll(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
	}
	got := manageableRoleSetFrom(roles, []int{1}, true)
	for _, roleID := range []int{1, 2, 3} {
		if _, ok := got[roleID]; !ok {
			t.Fatalf("超级管理员应可管理角色 %d，got=%v", roleID, got)
		}
	}
}

// TestParentRoleSetFromIncludesOperatorRoles 验证普通管理员可把自身角色作为下级角色父级。
func TestParentRoleSetFromIncludesOperatorRoles(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 1, Pids: "1"},
	}
	got := parentRoleSetFrom(roles, []int{2}, false)
	for _, roleID := range []int{2, 3} {
		if _, ok := got[roleID]; !ok {
			t.Fatalf("普通管理员应可选择父级角色 %d，got=%v", roleID, got)
		}
	}
	for _, roleID := range []int{1, 4} {
		if _, ok := got[roleID]; ok {
			t.Fatalf("普通管理员不应选择父级角色 %d，got=%v", roleID, got)
		}
	}
}

// TestDocumentPermissionEntryNormalization 验证文档子权限保存时会补齐入口权限，并清理缺少入口的半截授权。
func TestDocumentPermissionEntryNormalization(t *testing.T) {
	idAlias := map[int]routealias.Alias{
		99:  routealias.DocsIndex,
		160: routealias.DocsAPIIndex,
		164: routealias.DocsAPIServiceIndex,
		165: routealias.DocsAPIServiceFront,
		210: routealias.Alias("docs.file.角色文档/后端开发/AI开发提示词.md"),
		222: routealias.Alias("docs.file.api/接口文档/前台系统/系统接口.md"),
	}
	aliasID := map[routealias.Alias]int{
		routealias.DocsIndex:           99,
		routealias.DocsAPIIndex:        160,
		routealias.DocsAPIServiceIndex: 164,
		routealias.DocsAPIServiceFront: 165,
		routealias.Alias("docs.file.角色文档/后端开发/AI开发提示词.md"):  210,
		routealias.Alias("docs.file.api/接口文档/前台系统/系统接口.md"): 222,
	}

	expanded := expandDocumentEntryPermissionIDs([]int{160, 165, 210, 222}, idAlias, aliasID)
	assertIntSetEqual(t, expanded, []int{99, 160, 164, 165, 210, 222})

	retained := retainAssignablePermissionIDs(expanded, []int{99, 160, 165, 210, 222})
	complete := retainCompleteDocumentPermissionIDs(retained, idAlias, aliasID)
	assertIntSetEqual(t, complete, []int{99, 160, 210})
}

// TestPermissionAncestorNormalization 验证角色授权保存时补齐菜单祖先，避免子菜单有权限但父菜单不可见。
func TestPermissionAncestorNormalization(t *testing.T) {
	rows := []permissionPathRow{
		{ID: 1, Pids: ""},
		{ID: 52, Pids: "1"},
		{ID: 100, Pids: "1,52"},
	}

	expanded := expandPermissionAncestorIDsFromRows([]int{52}, rows)
	assertIntSetEqual(t, expanded, []int{1, 52})

	expanded = expandPermissionAncestorIDsFromRows([]int{100}, rows)
	assertIntSetEqual(t, expanded, []int{1, 52, 100})

	complete := retainCompletePermissionPathIDsFromRows([]int{52}, rows)
	assertIntSetEqual(t, complete, []int{})

	complete = retainCompletePermissionPathIDsFromRows([]int{1, 52}, rows)
	assertIntSetEqual(t, complete, []int{1, 52})
}

// assertIntSetEqual 校验整数集合一致，不要求顺序。
func assertIntSetEqual(t *testing.T, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("int set len = %d, want %d, got=%v", len(got), len(want), got)
	}
	gotSet := make(map[int]struct{}, len(got))
	for _, item := range got {
		gotSet[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := gotSet[item]; !ok {
			t.Fatalf("int set missing %d, got=%v want=%v", item, got, want)
		}
	}
}

// TestWithRolePermissionWriteLockReturnsServiceBusyWhenLocked 验证角色权限写锁已被占用时，新的写请求会直接返回服务繁忙。
func TestWithRolePermissionWriteLockReturnsServiceBusyWhenLocked(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}
	lock := redislock.NewLock(client, logicObj.AppRedisKey(keys.RolePermissionWriteLock))
	if err := lock.TryLock(context.Background(), rolePermissionWriteLockTTL); err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Fatalf("Unlock() error = %v", err)
		}
	}()

	result := logicObj.withRolePermissionWriteLock("AdminRoleLogic.TestLock", func() *types.BizResult {
		return types.NewBizResult(codes.Success)
	})
	if result == nil {
		t.Fatalf("withRolePermissionWriteLock() result is nil")
	}
	if result.Code != codes.ServiceBusy {
		t.Fatalf("withRolePermissionWriteLock() code = %d, want %d", result.Code, codes.ServiceBusy)
	}
}

// TestWithRolePermissionWriteLockExecutesWhenUnlocked 验证空闲时角色权限写锁允许单个写请求进入临界区。
func TestWithRolePermissionWriteLockExecutesWhenUnlocked(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}

	called := false
	result := logicObj.withRolePermissionWriteLock("AdminRoleLogic.TestLock", func() *types.BizResult {
		called = true
		time.Sleep(10 * time.Millisecond)
		return types.NewBizResult(codes.Success)
	})
	if !called {
		t.Fatalf("withRolePermissionWriteLock() did not execute critical section")
	}
	if result == nil {
		t.Fatalf("withRolePermissionWriteLock() result is nil")
	}
	if result.Code != codes.Success {
		t.Fatalf("withRolePermissionWriteLock() code = %d, want %d", result.Code, codes.Success)
	}
}

// TestRefreshRoleRelatedCacheByScopeDeletesExactAdminCaches 验证角色缓存刷新只精确删除受影响管理员的高基数缓存。
func TestRefreshRoleRelatedCacheByScopeDeletesExactAdminCaches(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}
	ctx := context.Background()
	roleCacheKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.RoleTree),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.RoleStatus),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RolePermission, 3)),
	}
	targetAdminKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRoleIDs, 7)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRolesDetail, 7)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminPermissionIDs, 7)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminPermissionUUIDs, 7)),
	}
	untouchedAdminKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRoleIDs, 8)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRolesDetail, 8)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminPermissionIDs, 8)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminPermissionUUIDs, 8)),
	}
	for _, key := range append(append(roleCacheKeys, targetAdminKeys...), untouchedAdminKeys...) {
		if err := client.SAdd(ctx, key, "value").Err(); err != nil {
			t.Fatalf("SAdd(%s) error = %v", key, err)
		}
	}

	logicObj.refreshRoleRelatedCacheByScope([]int{3}, []int{7})

	for _, key := range append(roleCacheKeys, targetAdminKeys...) {
		if server.Exists(key) {
			t.Fatalf("refreshRoleRelatedCacheByScope() key %s should be deleted", key)
		}
	}
	for _, key := range untouchedAdminKeys {
		if !server.Exists(key) {
			t.Fatalf("refreshRoleRelatedCacheByScope() unrelated key %s should be kept", key)
		}
	}
}

// TestDiffPermissionIDs 验证角色权限增量保存时能正确拆分新增和删除集合。
func TestDiffPermissionIDs(t *testing.T) {
	added, removed := diffPermissionIDs(
		[]int{1, 2, 3, 3, 5},
		[]int{2, 4, 5, 5},
	)
	wantAdded := []int{4}
	wantRemoved := []int{1, 3}
	if len(added) != len(wantAdded) {
		t.Fatalf("diffPermissionIDs() added len = %d, want %d, got=%v", len(added), len(wantAdded), added)
	}
	for index := range wantAdded {
		if added[index] != wantAdded[index] {
			t.Fatalf("diffPermissionIDs() added[%d] = %d, want %d, got=%v", index, added[index], wantAdded[index], added)
		}
	}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("diffPermissionIDs() removed len = %d, want %d, got=%v", len(removed), len(wantRemoved), removed)
	}
	for index := range wantRemoved {
		if removed[index] != wantRemoved[index] {
			t.Fatalf("diffPermissionIDs() removed[%d] = %d, want %d, got=%v", index, removed[index], wantRemoved[index], removed)
		}
	}
}

// TestMarkRoleTreeScopeDisablesOutOfManageNodes 验证角色树会按当前管理员可管理范围锁定越权分支。
func TestMarkRoleTreeScopeDisablesOutOfManageNodes(t *testing.T) {
	items := []types.AdminRoleItem{
		{
			ID:         1,
			Title:      "父角色",
			Status:     1,
			IsDelete:   0,
			Selectable: true,
			Children: []types.AdminRoleItem{
				{
					ID:         2,
					Title:      "可管理子角色",
					Status:     1,
					IsDelete:   0,
					Selectable: true,
				},
				{
					ID:         3,
					Title:      "越权子角色",
					Status:     1,
					IsDelete:   0,
					Selectable: true,
				},
			},
		},
		{
			ID:         4,
			Title:      "禁用角色",
			Status:     0,
			IsDelete:   0,
			Selectable: true,
		},
	}

	got := markRoleTreeScope(items, map[int]struct{}{
		1: {},
		2: {},
	})
	if len(got) != 2 {
		t.Fatalf("markRoleTreeScope() len = %d, want 2", len(got))
	}
	if got[0].Disabled || got[0].DisableCheckbox || !got[0].Selectable {
		t.Fatalf("父角色状态不符合预期: %+v", got[0])
	}
	if got[0].Children[0].Disabled || got[0].Children[0].DisableCheckbox || !got[0].Children[0].Selectable {
		t.Fatalf("可管理子角色状态不符合预期: %+v", got[0].Children[0])
	}
	if !got[0].Children[1].Disabled || !got[0].Children[1].DisableCheckbox || got[0].Children[1].Selectable {
		t.Fatalf("越权子角色应被锁定: %+v", got[0].Children[1])
	}
	if !got[1].Disabled || !got[1].DisableCheckbox || got[1].Selectable {
		t.Fatalf("禁用角色应保持不可选: %+v", got[1])
	}
}

// TestMarkRoleTreeParentScopeAllowsOperatorRole 验证父级下拉允许普通管理员选择自身角色创建下级。
func TestMarkRoleTreeParentScopeAllowsOperatorRole(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 2, Pid: 1, Pids: "1"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 1, Pids: "1"},
	}
	items := []types.AdminRoleItem{
		{
			ID:         1,
			Title:      "超级管理员",
			Status:     1,
			IsDelete:   0,
			Selectable: true,
			Children: []types.AdminRoleItem{
				{
					ID:         2,
					Title:      "管理员",
					Status:     1,
					IsDelete:   0,
					Selectable: true,
					Children: []types.AdminRoleItem{
						{
							ID:         3,
							Title:      "管理员下级",
							Status:     1,
							IsDelete:   0,
							Selectable: true,
						},
					},
				},
				{
					ID:         4,
					Title:      "同级角色",
					Status:     1,
					IsDelete:   0,
					Selectable: true,
				},
			},
		},
	}

	got := markRoleTreeScope(items, parentRoleSetFrom(roles, []int{2}, false))

	adminNode := got[0].Children[0]
	if adminNode.Disabled || adminNode.DisableCheckbox || !adminNode.Selectable {
		t.Fatalf("自身角色应允许作为新增下级父级: %+v", adminNode)
	}
	childNode := adminNode.Children[0]
	if childNode.Disabled || childNode.DisableCheckbox || !childNode.Selectable {
		t.Fatalf("自身后代角色应允许继续作为父级: %+v", childNode)
	}
	siblingNode := got[0].Children[1]
	if !siblingNode.Disabled || !siblingNode.DisableCheckbox || siblingNode.Selectable {
		t.Fatalf("同级角色不应允许作为父级: %+v", siblingNode)
	}
}
