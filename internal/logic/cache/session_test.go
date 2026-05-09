package cache

import (
	"context"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestAdminSessionCacheUsesAppPrefix 验证管理员登录态只写入 app_id 命名空间下的 Redis key。
func TestAdminSessionCacheUsesAppPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: int64((4 * time.Hour) / time.Second),
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "admin",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	if server.Exists(keys.AdminInfoLogicalKey(7)) {
		t.Fatal("不应写入裸管理员登录态 key")
	}
	if !server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("期望写入 app_id 命名空间下的管理员登录态 key")
	}
}

// TestAdminLogoutTokenUsesAppPrefix 验证登出标记不会写入裸 Redis key。
func TestAdminLogoutTokenUsesAppPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.MarkAdminLogoutToken(7, "token-7", time.Hour); err != nil {
		t.Fatalf("MarkAdminLogoutToken() error = %v", err)
	}
	if server.Exists(keys.AdminLogoutTokenLogicalKey(7)) {
		t.Fatal("不应写入裸管理员登出标记 key")
	}
	if !server.Exists(keys.AdminLogoutTokenRedisKey(7)) {
		t.Fatal("期望写入 app_id 命名空间下的管理员登出标记 key")
	}
}
