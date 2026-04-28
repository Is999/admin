package cache

import (
	"context"
	"fmt"
	"testing"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// deleteCommandCaptureHook 捕获测试中的 DEL 命令参数数量，验证 Redis Cluster 下不会发出跨 slot 多 key DEL。
type deleteCommandCaptureHook struct {
	directArgCounts   *[]int // directArgCounts 记录普通命令链路中的 DEL 参数数量。
	pipelineArgCounts *[]int // pipelineArgCounts 记录管道命令链路中的 DEL 参数数量。
}

// TestTableCacheKeyScope 验证 table-cache 使用独立的 app:{id}:table:{key} 命名空间。
func TestTableCacheKeyScope(t *testing.T) {
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))

	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "scopes logical key",
			key:  keys.RoleTree,
			want: "app:site-a:table:role_tree",
		},
		{
			name: "keeps table cache key unchanged",
			key:  "app:site-a:table:role_tree",
			want: "app:site-a:table:role_tree",
		},
		{
			name: "keeps other app table cache key unchanged",
			key:  "app:site-b:table:role_tree",
			want: "app:site-b:table:role_tree",
		},
		{
			name: "keeps direct app key unchanged",
			key:  "app:site-a:admin:info:7",
			want: "app:site-a:admin:info:7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tableCachePhysicalKey(base, tt.key); got != tt.want {
				t.Fatalf("tableCachePhysicalKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTableCacheLogicalKey 只去掉 table-cache 前缀，不截断普通 app 级 Redis key。
func TestTableCacheLogicalKey(t *testing.T) {
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))

	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "trims table cache key",
			key:  "app:site-a:table:admin_role_ids:7",
			want: "admin_role_ids:7",
		},
		{
			name: "keeps direct app key",
			key:  "app:site-a:admin:info:7",
			want: "app:site-a:admin:info:7",
		},
		{
			name: "keeps other app table cache key",
			key:  "app:site-b:table:admin_role_ids:7",
			want: "app:site-b:table:admin_role_ids:7",
		},
		{
			name: "keeps logical key",
			key:  "admin_role_ids:7",
			want: "admin_role_ids:7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tableCacheLogicalKey(base, tt.key); got != tt.want {
				t.Fatalf("tableCacheLogicalKey() = %q, want %q", got, tt.want)
			}
		})
	}
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
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))

	cacheLogic := NewCacheLogic(base.Ctx, base.Svc)
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "super999",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	roleKey := tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 7))
	permissionKey := tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionUUIDs, 7))
	if err := client.Set(base.Ctx, roleKey, "1,2", 0).Err(); err != nil {
		t.Fatalf("seed role key error = %v", err)
	}
	if err := client.Set(base.Ctx, permissionKey, "uuid-1", 0).Err(); err != nil {
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
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	ctx := context.Background()

	targetKeys := []string{
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 7)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, 7)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionIDs, 7)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionUUIDs, 7)),
	}
	untouchedKeys := []string{
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 8)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, 8)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionIDs, 8)),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionUUIDs, 8)),
		keys.AdminInfoRedisKey(base.AppID(), 7),
		tableCachePhysicalKey(base, fmt.Sprintf(keys.AdminProfile, 7)),
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

	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
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
