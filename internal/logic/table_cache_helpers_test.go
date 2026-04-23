package logic

import (
	"context"
	"fmt"
	"testing"

	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// deleteCommandCaptureHook 捕获测试中的 DEL 命令参数数量，验证 Redis Cluster 下不会发出跨 slot 多 key DEL。
type deleteCommandCaptureHook struct {
	directArgCounts   *[]int // directArgCounts 记录普通命令链路中的 DEL 参数数量。
	pipelineArgCounts *[]int // pipelineArgCounts 记录管道命令链路中的 DEL 参数数量。
}

// DialHook 不修改测试 Redis 连接行为，仅满足 go-redis Hook 接口。
func (h deleteCommandCaptureHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 捕获非管道 DEL 命令参数数量。
func (h deleteCommandCaptureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.FullName() == "del" && h.directArgCounts != nil {
			*h.directArgCounts = append(*h.directArgCounts, len(cmd.Args()))
		}
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook 捕获管道中的 DEL 命令参数数量。
func (h deleteCommandCaptureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.FullName() == "del" && h.pipelineArgCounts != nil {
				*h.pipelineArgCounts = append(*h.pipelineArgCounts, len(cmd.Args()))
			}
		}
		return next(ctx, cmds)
	}
}

// TestInvalidateAdminRelationCachePreserveSession 验证个人中心自助更新时不会删除当前登录态缓存。
func TestInvalidateAdminRelationCachePreserveSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := NewBaseLogicWithContext(context.Background(), newTestSecurityLogic().svc)
	base.svc.Rds = client

	cacheLogic := NewCacheLogic(base.Context(), base.svc)
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "super999",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	roleKey := fmt.Sprintf(keys.AdminRoleIDs, 7)
	permissionKey := fmt.Sprintf(keys.AdminPermissionUUIDs, 7)
	if err := client.Set(base.Context(), roleKey, "1,2", 0).Err(); err != nil {
		t.Fatalf("seed role key error = %v", err)
	}
	if err := client.Set(base.Context(), permissionKey, "uuid-1", 0).Err(); err != nil {
		t.Fatalf("seed permission key error = %v", err)
	}

	invalidateAdminRelationCachePreserveSession(base, 7)

	if _, err := cacheLogic.GetAdminInfo(7); err != nil {
		t.Fatalf("GetAdminInfo() error = %v, want session kept", err)
	}
	if server.Exists(roleKey) {
		t.Fatalf("role key %s should be deleted", roleKey)
	}
	if server.Exists(permissionKey) {
		t.Fatalf("permission key %s should be deleted", permissionKey)
	}
}

// TestInvalidateAdminRoleAndPermissionCacheByAdminIDsDeletesOnlyTargetAdmins 验证只清理受影响管理员。
func TestInvalidateAdminRoleAndPermissionCacheByAdminIDsDeletesOnlyTargetAdmins(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := NewBaseLogicWithContext(context.Background(), newTestSecurityLogic().svc)
	base.svc.Rds = client
	ctx := context.Background()

	targetKeys := []string{
		fmt.Sprintf(keys.AdminRoleIDs, 7),
		fmt.Sprintf(keys.AdminRolesDetail, 7),
		fmt.Sprintf(keys.AdminPermissionIDs, 7),
		fmt.Sprintf(keys.AdminPermissionUUIDs, 7),
	}
	untouchedKeys := []string{
		fmt.Sprintf(keys.AdminRoleIDs, 8),
		fmt.Sprintf(keys.AdminRolesDetail, 8),
		fmt.Sprintf(keys.AdminPermissionIDs, 8),
		fmt.Sprintf(keys.AdminPermissionUUIDs, 8),
		base.AppRedisKey(fmt.Sprintf(keys.AdminInfo, 7)),
		fmt.Sprintf(keys.AdminProfile, 7),
	}
	for _, key := range append(targetKeys, untouchedKeys...) {
		if err := client.SAdd(ctx, key, "value").Err(); err != nil {
			t.Fatalf("SAdd(%s) error = %v", key, err)
		}
	}

	invalidateAdminRoleAndPermissionCacheByAdminIDs(base, 7)

	for _, key := range targetKeys {
		if server.Exists(key) {
			t.Fatalf("invalidateAdminRoleAndPermissionCacheByAdminIDs() target key %s should be deleted", key)
		}
	}
	for _, key := range untouchedKeys {
		if !server.Exists(key) {
			t.Fatalf("invalidateAdminRoleAndPermissionCacheByAdminIDs() unrelated key %s should be kept", key)
		}
	}
}

// TestDeleteRedisKeysExactBatchesUsesSingleKeyDeleteCommands 验证精确删除缓存时不会发出跨 slot 多 key DEL。
func TestDeleteRedisKeysExactBatchesUsesSingleKeyDeleteCommands(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	var directArgCounts []int
	var pipelineArgCounts []int
	client.AddHook(deleteCommandCaptureHook{
		directArgCounts:   &directArgCounts,
		pipelineArgCounts: &pipelineArgCounts,
	})

	base := NewBaseLogicWithContext(context.Background(), newTestSecurityLogic().svc)
	base.svc.Rds = client
	ctx := context.Background()
	cacheKeys := []string{
		keys.PermissionTree,
		keys.PermissionModule,
		keys.PermissionUUID,
	}
	for _, key := range cacheKeys {
		if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}

	deleteRedisKeysExactBatches(base, "test delete", cacheKeys)

	for _, key := range cacheKeys {
		if server.Exists(key) {
			t.Fatalf("deleteRedisKeysExactBatches() key %s should be deleted", key)
		}
	}
	allArgCounts := append(directArgCounts, pipelineArgCounts...)
	if len(allArgCounts) != len(cacheKeys) {
		t.Fatalf("DEL command count = %d, want %d, direct=%v pipeline=%v", len(allArgCounts), len(cacheKeys), directArgCounts, pipelineArgCounts)
	}
	for _, argCount := range allArgCounts {
		if argCount != 2 {
			t.Fatalf("DEL args length = %d, want 2(command + one key), direct=%v pipeline=%v", argCount, directArgCounts, pipelineArgCounts)
		}
	}
}
