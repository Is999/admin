package rbac

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"context"
	"fmt"
	"strings"
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
	miniredisserver "github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"
)

// runRBACStandaloneRedis 模拟真实单机 Redis 的拓扑探测响应。
func runRBACStandaloneRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	server.Server().SetPreHook(func(peer *miniredisserver.Peer, command string, args ...string) bool {
		if !strings.EqualFold(command, "cluster") || len(args) != 1 || !strings.EqualFold(args[0], "info") {
			return false
		}
		peer.WriteError("ERR This instance has cluster support disabled")
		return true
	})
	return server
}

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

// TestRolePidsFromHierarchy 验证角色族谱只按真实 pid 链计算，不依赖可能滞后的 pids。
func TestRolePidsFromHierarchy(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 5, Pid: 1, Pids: "stale"},
		{ID: 6, Pid: 5, Pids: "stale"},
	}
	got, err := rolePidsFromHierarchy(roles, 6, 9)
	if err != nil {
		t.Fatalf("rolePidsFromHierarchy() error = %v", err)
	}
	if got != "1,5,6" {
		t.Fatalf("rolePidsFromHierarchy() = %q, want %q", got, "1,5,6")
	}
}

// TestRolePidsFromHierarchyRejectsDescendant 验证角色不能移动到自己的子级下面。
func TestRolePidsFromHierarchyRejectsDescendant(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0},
		{ID: 2, Pid: 1},
		{ID: 3, Pid: 2},
	}
	if _, err := rolePidsFromHierarchy(roles, 3, 2); err == nil {
		t.Fatal("rolePidsFromHierarchy() should reject descendant parent")
	}
}

// TestDisabledRolePathID 校验启用角色时会识别真实父级链上的禁用节点。
func TestDisabledRolePathID(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Status: 1},
		{ID: 2, Pid: 1, Status: 0},
		{ID: 3, Pid: 2, Status: 1},
	}
	if got, err := disabledRolePathID(roles, 3); err != nil || got != 2 {
		t.Fatalf("disabledRolePathID() = (%d, %v), want (2, nil)", got, err)
	}
	if got, err := disabledRolePathID(roles, 1); err != nil || got != 0 {
		t.Fatalf("disabledRolePathID(root) = (%d, %v), want (0, nil)", got, err)
	}
}

// TestDescendantRolePidsRebuildsMovedSubtree 验证角色换父级后全部子孙族谱同步切换到新祖先链。
func TestDescendantRolePidsRebuildsMovedSubtree(t *testing.T) {
	roles := []model.AdminRole{
		{ID: 1, Pid: 0, Pids: ""},
		{ID: 5, Pid: 1, Pids: "1"},
		{ID: 2, Pid: 5, Pids: "1,5"},
		{ID: 3, Pid: 2, Pids: "1,2"},
		{ID: 4, Pid: 3, Pids: "1,2,3"},
		{ID: 8, Pid: 1, Pids: "1"},
	}
	got, err := descendantRolePids(roles, 2)
	if err != nil {
		t.Fatalf("descendantRolePids() error = %v", err)
	}
	if got[3] != "1,5,2" || got[4] != "1,5,2,3" {
		t.Fatalf("descendantRolePids() = %v", got)
	}
	if _, ok := got[8]; ok {
		t.Fatalf("descendantRolePids() should not include sibling role: %v", got)
	}
}

// TestEffectiveRolePermissionIDs 验证超级角色自身按隐式全权限展示。
func TestEffectiveRolePermissionIDs(t *testing.T) {
	assertIntSetEqual(t, effectiveRolePermissionIDs(corelogic.AdminSuperRoleID, nil, []int{1, 2}), []int{1, 2})
	assertIntSetEqual(t, effectiveRolePermissionIDs(2, []int{2}, []int{1, 2}), []int{2})
}

// TestRoleParentIDForUpdate 验证普通角色采用明确父级，超级角色始终固定在根节点。
func TestRoleParentIDForUpdate(t *testing.T) {
	tests := []struct {
		name         string // 测试场景
		roleID       int    // 目标角色 ID
		requestedPid int    // 请求父角色 ID
		want         int    // 期望父角色 ID
		wantErr      bool   // 是否期望错误
	}{
		{name: "normal moves to root", roleID: 2, requestedPid: 0, want: 0},
		{name: "normal changes parent", roleID: 2, requestedPid: 3, want: 3},
		{name: "super stays root", roleID: corelogic.AdminSuperRoleID, requestedPid: 0, want: 0},
		{name: "super rejects parent", roleID: corelogic.AdminSuperRoleID, requestedPid: 3, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roleParentIDForUpdate(tt.roleID, tt.requestedPid)
			if (err != nil) != tt.wantErr {
				t.Fatalf("roleParentIDForUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("roleParentIDForUpdate() = %d, want %d", got, tt.want)
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

// TestRoleItemScopeSetFromUsesCachedTree 验证角色树展示范围可直接基于缓存节点计算。
func TestRoleItemScopeSetFromUsesCachedTree(t *testing.T) {
	items := []types.AdminRoleItem{
		{
			ID:   1,
			Pids: "",
			Children: []types.AdminRoleItem{
				{
					ID:   2,
					Pids: "1",
					Children: []types.AdminRoleItem{
						{ID: 3, Pids: "1,2"},
					},
				},
				{ID: 4, Pids: "1"},
			},
		},
	}
	manageable := roleItemScopeSetFrom(items, []int{2}, false, false)
	if _, ok := manageable[3]; !ok || len(manageable) != 1 {
		t.Fatalf("roleItemScopeSetFrom(manageable) = %v, want only role 3", manageable)
	}
	parentOptions := roleItemScopeSetFrom(items, []int{2}, false, true)
	for _, roleID := range []int{2, 3} {
		if _, ok := parentOptions[roleID]; !ok {
			t.Fatalf("roleItemScopeSetFrom(parent) missing role %d: %v", roleID, parentOptions)
		}
	}
}

// TestRoleDocPermissionIDsUsesCache 验证角色文档权限命中 Redis 时不依赖数据库。
func TestRoleDocPermissionIDsUsesCache(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}
	cacheKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RoleDocPermission, 3))
	if err := client.SAdd(context.Background(), cacheKey, "10", "11").Err(); err != nil {
		t.Fatalf("写入角色文档权限缓存失败: %v", err)
	}
	permissionIDs, err := logicObj.roleDocPermissionRelationIDsWithCache(3)
	if err != nil {
		t.Fatalf("roleDocPermissionRelationIDsWithCache(3) error=%v", err)
	}
	assertIntSetEqual(t, permissionIDs, []int{10, 11})
}

// TestDocumentPermissionEntryNormalization 验证文档权限必须同时拥有对应站点入口路由。
func TestDocumentPermissionEntryNormalization(t *testing.T) {
	docSites := map[int]string{
		210: "admin",
		222: "api",
	}
	entryPermissionIDs := map[string]int{
		"admin": 99,
		"api":   164,
	}
	retained := retainDocPermissionsWithEntries([]int{210, 222}, docSites, entryPermissionIDs, []int{99})
	assertIntSetEqual(t, retained, []int{210})
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

	complete = retainCompletePermissionPathIDsFromRows([]int{1, 52}, []permissionPathRow{{ID: 52, Pids: "1"}})
	assertIntSetEqual(t, complete, []int{})
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
	lock := redislock.NewLock(client, logicObj.AppRedisKey(keys.RBACWriteLock))
	if err := lock.TryLock(context.Background(), rbacWriteLockTTL); err != nil {
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
	server := runRBACStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}
	ctx := context.Background()
	roleCacheKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.RoleTree),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.RoleStatus),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RolePermission, 3)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RoleDocPermission, 3)),
	}
	targetAdminKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRolesDetail, 7)),
	}
	untouchedAdminKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRoleIDs, 7)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRoleIDs, 8)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRolesDetail, 8)),
	}
	for _, key := range append(append(roleCacheKeys, targetAdminKeys...), untouchedAdminKeys...) {
		if err := client.SAdd(ctx, key, "value").Err(); err != nil {
			t.Fatalf("SAdd(%s) error = %v", key, err)
		}
	}

	if err := logicObj.refreshRoleRelatedCacheByScope([]int{3}, []int{7}); err != nil {
		t.Fatalf("refreshRoleRelatedCacheByScope() error=%v", err)
	}

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

// TestRefreshRolePermissionCacheKeepsAdminRoleCache 验证权限关系变更不再扇出清理管理员角色缓存。
func TestRefreshRolePermissionCacheKeepsAdminRoleCache(t *testing.T) {
	server := runRBACStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})),
	}
	ctx := context.Background()
	rolePermissionKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RolePermission, 3))
	roleDocPermissionKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RoleDocPermission, 3))
	adminRoleKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.AdminRoleIDs, 7))
	for _, key := range []string{rolePermissionKey, roleDocPermissionKey, adminRoleKey} {
		if err := client.SAdd(ctx, key, "1").Err(); err != nil {
			t.Fatalf("SAdd(%s) error=%v", key, err)
		}
	}

	if err := logicObj.refreshRolePermissionCache(3); err != nil {
		t.Fatalf("refreshRolePermissionCache() error=%v", err)
	}
	for _, key := range []string{rolePermissionKey, roleDocPermissionKey} {
		if server.Exists(key) {
			t.Fatalf("refreshRolePermissionCache() key %s should be deleted", key)
		}
	}
	if !server.Exists(adminRoleKey) {
		t.Fatalf("refreshRolePermissionCache() admin role key %s should be kept", adminRoleKey)
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
	if got[0].Disabled || got[0].DisableCheckbox || !got[0].Selectable || !got[0].Manageable {
		t.Fatalf("父角色状态不符合预期: %+v", got[0])
	}
	if got[0].Children[0].Disabled || got[0].Children[0].DisableCheckbox ||
		!got[0].Children[0].Selectable || !got[0].Children[0].Manageable {
		t.Fatalf("可管理子角色状态不符合预期: %+v", got[0].Children[0])
	}
	if !got[0].Children[1].Disabled || !got[0].Children[1].DisableCheckbox ||
		got[0].Children[1].Selectable || got[0].Children[1].Manageable {
		t.Fatalf("越权子角色应被锁定: %+v", got[0].Children[1])
	}
	if !got[1].Disabled || !got[1].DisableCheckbox || got[1].Selectable || got[1].Manageable {
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

	parentRoleSet := parentRoleSetFrom(roles, []int{2}, false)
	got := markRoleTreeParentScope(items, parentRoleSet, true)

	adminNode := got[0].Children[0]
	if adminNode.Disabled || adminNode.DisableCheckbox || !adminNode.Selectable || !adminNode.CanCreateChild {
		t.Fatalf("自身角色应允许作为新增下级父级: %+v", adminNode)
	}
	childNode := adminNode.Children[0]
	if childNode.Disabled || childNode.DisableCheckbox || !childNode.Selectable || !childNode.CanCreateChild {
		t.Fatalf("自身后代角色应允许继续作为父级: %+v", childNode)
	}
	siblingNode := got[0].Children[1]
	if !siblingNode.Disabled || !siblingNode.DisableCheckbox || siblingNode.Selectable || siblingNode.CanCreateChild {
		t.Fatalf("同级角色不应允许作为父级: %+v", siblingNode)
	}
}

// TestMarkRoleTreeCreateScopeDisablesCreateBelowDisabledAncestor 验证禁用角色路径下不能新增子角色。
func TestMarkRoleTreeCreateScopeDisablesCreateBelowDisabledAncestor(t *testing.T) {
	items := []types.AdminRoleItem{{
		ID:       2,
		Status:   0,
		IsDelete: 0,
		Children: []types.AdminRoleItem{{
			ID:       3,
			Status:   1,
			IsDelete: 0,
		}},
	}}
	got := markRoleTreeCreateScope(items, map[int]struct{}{2: {}, 3: {}}, true)
	if got[0].CanCreateChild || got[0].Children[0].CanCreateChild {
		t.Fatalf("disabled role path must not allow child creation: %+v", got)
	}
}

// TestMarkRoleTreeParentScopeDisablesSelectionBelowDisabledAncestor 验证父级下拉不会暴露后端必然拒绝的禁用路径。
func TestMarkRoleTreeParentScopeDisablesSelectionBelowDisabledAncestor(t *testing.T) {
	items := []types.AdminRoleItem{{
		ID:       2,
		Status:   0,
		IsDelete: 0,
		Children: []types.AdminRoleItem{{
			ID:       3,
			Status:   1,
			IsDelete: 0,
		}},
	}}
	got := markRoleTreeParentScope(items, map[int]struct{}{2: {}, 3: {}}, true)
	if !got[0].Disabled || got[0].Selectable || !got[0].Children[0].Disabled || got[0].Children[0].Selectable {
		t.Fatalf("禁用角色路径不应出现在可选父级中: %+v", got)
	}
}
