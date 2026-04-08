package logic

import (
	"context"
	"fmt"
	"testing"
	"time"

	codes "admin_cron/common/codes"
	keys "admin_cron/common/rediskeys"
	redislock "admin_cron/internal/infra/redsync"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

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

// TestWithRolePermissionWriteLockReturnsServiceBusyWhenLocked 验证角色权限写锁已被占用时，新的写请求会直接返回服务繁忙。
func TestWithRolePermissionWriteLockReturnsServiceBusyWhenLocked(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	lock := redislock.NewLock(client, rolePermissionWriteLockKey)
	if err := lock.TryLock(context.Background(), rolePermissionWriteLockTTL); err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Fatalf("Unlock() error = %v", err)
		}
	}()

	logicObj := &AdminRoleLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), &svc.ServiceContext{Rds: client}),
	}
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
		BaseLogic: NewBaseLogicWithContext(context.Background(), &svc.ServiceContext{Rds: client}),
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
		BaseLogic: NewBaseLogicWithContext(context.Background(), &svc.ServiceContext{Rds: client}),
	}
	ctx := context.Background()
	roleCacheKeys := []string{
		keys.RoleTree,
		keys.RoleStatus,
		fmt.Sprintf(keys.RolePermission, 3),
	}
	targetAdminKeys := []string{
		fmt.Sprintf(keys.AdminRoleIDs, 7),
		fmt.Sprintf(keys.AdminRolesDetail, 7),
		fmt.Sprintf(keys.AdminPermissionIDs, 7),
		fmt.Sprintf(keys.AdminPermissionUUIDs, 7),
	}
	untouchedAdminKeys := []string{
		fmt.Sprintf(keys.AdminRoleIDs, 8),
		fmt.Sprintf(keys.AdminRolesDetail, 8),
		fmt.Sprintf(keys.AdminPermissionIDs, 8),
		fmt.Sprintf(keys.AdminPermissionUUIDs, 8),
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
