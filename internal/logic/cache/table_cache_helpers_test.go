package cache

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"github.com/alicebob/miniredis/v2"
	miniredisserver "github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"
)

// runCacheStandaloneRedis 模拟真实单机 Redis 的拓扑探测响应。
func runCacheStandaloneRedis(t *testing.T) *miniredis.Miniredis {
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

// TestTableCacheKeyScope 验证 table-cache 使用独立的 app:{id}:table:{key} 命名空间。
func TestTableCacheKeyScope(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))

	tests := []struct {
		name string // name 表示测试场景名称。
		key  string // key 表示待验证 key。
		want string // want 表示期望结果。
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
			name: "rejects other app table cache key",
			key:  "app:site-b:table:role_tree",
			want: "",
		},
		{
			name: "keeps direct app key unchanged",
			key:  "app:site-a:admin:session:7",
			want: "app:site-a:admin:session:7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TableCachePhysicalKey(base, tt.key); got != tt.want {
				t.Fatalf("TableCachePhysicalKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTableCacheKeyScopeFailsClosed 确保 app_id 缺失或错配时不会生成裸缓存 key。
func TestTableCacheKeyScopeFailsClosed(t *testing.T) {
	tests := []struct {
		name         string // name 表示测试场景名称。
		runtimeAppID string // runtimeAppID 表示进程级 app_id。
		baseAppID    string // baseAppID 表示请求服务上下文 app_id。
	}{
		{name: "missing app id"},
		{name: "mismatched app id", runtimeAppID: "site-a", baseAppID: "site-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useRuntimeAppID(t, tt.runtimeAppID)
			server := runCacheStandaloneRedis(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			base := corelogic.NewBaseLogicWithContext(
				context.Background(),
				svc.NewServiceContext(config.Config{AppID: tt.baseAppID}, svc.Dependencies{Rds: client}),
			)
			if got := TableCachePhysicalKey(base, keys.RoleTree); got != "" {
				t.Fatalf("TableCachePhysicalKey() = %q, want empty", got)
			}
			if _, err := TableCacheManager(base); !errors.Is(err, ErrRedisUnavailable) {
				t.Fatalf("TableCacheManager() error = %v, want ErrRedisUnavailable", err)
			}
		})
	}
}

// TestTableCacheLogicalKey 只去掉 table-cache 前缀，不截断普通 app 级 Redis key。
func TestTableCacheLogicalKey(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))

	tests := []struct {
		name string // name 表示测试场景名称。
		key  string // key 表示待验证 key。
		want string // want 表示期望结果。
	}{
		{
			name: "trims table cache key",
			key:  "app:site-a:table:admin_role_ids:7",
			want: "admin_role_ids:7",
		},
		{
			name: "keeps direct app key",
			key:  "app:site-a:admin:session:7",
			want: "app:site-a:admin:session:7",
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
			if got := TableCacheLogicalKey(base, tt.key); got != tt.want {
				t.Fatalf("TableCacheLogicalKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIsTableCacheTargetNotFoundWithWrappedError 验证 table-cache 新版包装错误后仍能识别目标缺失。
func TestIsTableCacheTargetNotFoundWithWrappedError(t *testing.T) {
	err := errors.Wrapf(tablecache.ErrTargetNotFound, "删除缓存失败")
	if !IsTableCacheTargetNotFound(err) {
		t.Fatalf("IsTableCacheTargetNotFound() = false, want true")
	}
}

// TestInvalidateAdminSessionCacheDeletesOnlySession 验证会话失效不会误删管理员角色缓存。
func TestInvalidateAdminSessionCacheDeletesOnlySession(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))

	cacheLogic := NewCacheLogic(base.Ctx, base.Svc)
	if err := cacheLogic.SetAdminSession(7, &types.AdminSession{
		ID:       7,
		UserName: "super999",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminSession() error = %v", err)
	}
	roleKey := TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 7))
	roleDetailKey := TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, 7))
	if err := client.Set(base.Ctx, roleKey, "1,2", 0).Err(); err != nil {
		t.Fatalf("seed role key error = %v", err)
	}
	if err := client.Set(base.Ctx, roleDetailKey, "super", 0).Err(); err != nil {
		t.Fatalf("seed role detail key error = %v", err)
	}

	if err := InvalidateAdminSessionCache(base, 7); err != nil {
		t.Fatalf("InvalidateAdminSessionCache() error = %v", err)
	}

	if _, err := cacheLogic.GetAdminSession(7); err == nil {
		t.Fatal("GetAdminSession() error = nil, want session deleted")
	}
	if !server.Exists(roleKey) {
		t.Fatalf("role key %s should be kept", roleKey)
	}
	if !server.Exists(roleDetailKey) {
		t.Fatalf("role detail key %s should be kept", roleDetailKey)
	}
}

// TestInvalidateAdminSecurityCacheDeletesSessionAndMFA 验证账号安全属性变化会同时撤销会话和 MFA 运行态。
func TestInvalidateAdminSecurityCacheDeletesSessionAndMFA(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	cacheLogic := NewCacheLogic(base.Ctx, base.Svc)
	if err := cacheLogic.SetAdminSession(7, &types.AdminSession{ID: 7, UserName: "admin-7", Token: "token-7"}); err != nil {
		t.Fatalf("SetAdminSession() error = %v", err)
	}
	if err := client.Set(base.Ctx, keys.LoginCheckMFAFlagRedisKey(7), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed login MFA flag error = %v", err)
	}
	if err := client.HSet(base.Ctx, keys.AdminMFATwoStepRedisKey(7), "ticket-1", "payload").Err(); err != nil {
		t.Fatalf("seed MFA ticket hash error = %v", err)
	}

	if err := InvalidateAdminSecurityCache(base, 7); err != nil {
		t.Fatalf("InvalidateAdminSecurityCache() error = %v", err)
	}
	for _, key := range []string{
		keys.AdminSessionRedisKey(7),
		keys.LoginCheckMFAFlagRedisKey(7),
		keys.AdminMFATwoStepRedisKey(7),
	} {
		if server.Exists(key) {
			t.Fatalf("security cache key %s should be deleted", key)
		}
	}
}

// TestInvalidateAdminSessionCacheFailsWithoutRedis 验证会话失效不会把 Redis 故障伪报成功。
func TestInvalidateAdminSessionCacheFailsWithoutRedis(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	if err := InvalidateAdminSessionCache(base, 7); err == nil {
		t.Fatal("InvalidateAdminSessionCache() error = nil, want Redis unavailable failure")
	}
}

// TestInvalidateAdminSessionCacheFailsClosedWithoutNamespace 验证命名空间异常不会吞掉失效请求。
func TestInvalidateAdminSessionCacheFailsClosedWithoutNamespace(t *testing.T) {
	useRuntimeAppID(t, "site-b")
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)

	if err := InvalidateAdminSessionCache(base, 7); err == nil {
		t.Fatal("InvalidateAdminSessionCache() error = nil, want namespace failure")
	}
	if !svcCtx.SecurityCacheSyncPending() {
		t.Fatal("命名空间异常后应立即关闭当前进程缓存鉴权")
	}
}

// TestInvalidateAdminRoleCacheByAdminIDsDeletesOnlyTargetAdmins 验证只清理受影响管理员的角色缓存。
func TestInvalidateAdminRoleCacheByAdminIDsDeletesOnlyTargetAdmins(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	ctx := context.Background()

	targetKeys := []string{
		TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 7)),
		TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, 7)),
	}
	untouchedKeys := []string{
		TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, 8)),
		TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, 8)),
		keys.AdminSessionRedisKey(7),
	}
	for _, key := range append(targetKeys, untouchedKeys...) {
		if err := client.SAdd(ctx, key, "value").Err(); err != nil {
			t.Fatalf("SAdd(%s) error = %v", key, err)
		}
	}

	if err := InvalidateAdminRoleCacheByAdminIDs(base, 7); err != nil {
		t.Fatalf("InvalidateAdminRoleCacheByAdminIDs() error = %v", err)
	}

	for _, key := range targetKeys {
		if server.Exists(key) {
			t.Fatalf("InvalidateAdminRoleCacheByAdminIDs() target key %s should be deleted", key)
		}
	}
	for _, key := range untouchedKeys {
		if !server.Exists(key) {
			t.Fatalf("InvalidateAdminRoleCacheByAdminIDs() unrelated key %s should be kept", key)
		}
	}
}

// TestStringHashFieldsWithCacheReadsSelectedFields 验证 Hash 点查不拉取无关字段。
func TestStringHashFieldsWithCacheReadsSelectedFields(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	ctx := context.Background()
	cacheKey := TableCachePhysicalKey(base, keys.RoleStatus)
	if err := client.HSet(ctx, cacheKey, "1", "1", "2", "0").Err(); err != nil {
		t.Fatalf("HSet(%s) error = %v", cacheKey, err)
	}

	values, err := StringHashFieldsWithCache(base, keys.RoleStatus, []string{"1", "3"})
	if err != nil {
		t.Fatalf("StringHashFieldsWithCache() error=%v", err)
	}
	if len(values) != 1 || values["1"] != "1" {
		t.Fatalf("StringHashFieldsWithCache() values=%v, want map[1:1]", values)
	}
}

// TestDeleteTableCacheKeysExactDeletesRegisteredTargets 验证精确失效会删除全部已注册表缓存。
func TestDeleteTableCacheKeysExactDeletesRegisteredTargets(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	ctx := context.Background()
	cacheKeys := []string{
		TableCachePhysicalKey(base, keys.PermissionTree),
		TableCachePhysicalKey(base, keys.RoutePermissionIDs),
		TableCachePhysicalKey(base, keys.PermissionUUID),
	}
	for _, key := range cacheKeys {
		if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}

	if err := DeleteTableCacheKeysExact(base, "test delete", cacheKeys); err != nil {
		t.Fatalf("DeleteTableCacheKeysExact() error = %v", err)
	}

	for _, key := range cacheKeys {
		if server.Exists(key) {
			t.Fatalf("DeleteTableCacheKeysExact() key %s should be deleted", key)
		}
	}
}

// TestDeleteTableCacheKeysExactRejectsUnknownTarget 验证失效必须经过 table-cache 目标解析，不能退化成原始 DEL。
func TestDeleteTableCacheKeysExactRejectsUnknownTarget(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := runCacheStandaloneRedis(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	cacheKey := TableCachePhysicalKey(base, "unknown_cache:1")
	if err := client.Set(base.Ctx, cacheKey, "value", 0).Err(); err != nil {
		t.Fatalf("Set(%s) error = %v", cacheKey, err)
	}

	err := DeleteTableCacheKeysExact(base, "test unknown", []string{cacheKey})
	if !IsTableCacheTargetNotFound(err) {
		t.Fatalf("DeleteTableCacheKeysExact() error = %v, want ErrTargetNotFound", err)
	}
	if !server.Exists(cacheKey) {
		t.Fatalf("DeleteTableCacheKeysExact() unknown key %s should be kept", cacheKey)
	}
}
