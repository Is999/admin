package security

import (
	"context"
	"fmt"
	"testing"

	keys "admin/common/rediskeys"

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
	cacheKey := keys.TableCachePrefix("site-a") + fmt.Sprintf(keys.SysConfigUUID, uuid)
	if err := client.HSet(context.Background(), cacheKey, map[string]any{
		"type":  "6",
		"value": value,
	}).Err(); err != nil {
		t.Fatalf("seedBoolSecurityConfig HSet failed: %v", err)
	}
}
