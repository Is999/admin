package security

import (
	"context"
	"fmt"
	"testing"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"

	"github.com/redis/go-redis/v9"
)

// seedBoolSecurityConfig 在 Redis 中写入布尔型安全配置缓存。
func seedBoolSecurityConfig(t *testing.T, client *redis.Client, uuid string, enabled bool) {
	t.Helper()
	if client == nil {
		t.Fatalf("seedBoolSecurityConfig client is nil")
	}
	value := "false"
	if enabled {
		value = "true"
	}
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	cacheKey := keys.TableCachePrefix() + fmt.Sprintf(keys.SysConfigUUID, uuid)
	if err := client.HSet(context.Background(), cacheKey, map[string]any{
		"type":  "6",
		"value": value,
	}).Err(); err != nil {
		t.Fatalf("seedBoolSecurityConfig HSet failed: %v", err)
	}
}

// seedStringSliceSecurityConfig 在 Redis 中写入字符串数组型安全配置缓存。
func seedStringSliceSecurityConfig(t *testing.T, client *redis.Client, uuid string, value string) {
	t.Helper()
	if client == nil {
		t.Fatalf("seedStringSliceSecurityConfig client is nil")
	}
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	cacheKey := keys.TableCachePrefix() + fmt.Sprintf(keys.SysConfigUUID, uuid)
	if err := client.HSet(context.Background(), cacheKey, map[string]any{
		"type":  "2",
		"value": value,
	}).Err(); err != nil {
		t.Fatalf("seedStringSliceSecurityConfig HSet failed: %v", err)
	}
}
