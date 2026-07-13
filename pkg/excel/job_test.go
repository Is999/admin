package excel

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestRedisStoreSaveWithTTL 验证调用方可以按业务完成时间控制状态剩余保留期。
func TestRedisStoreSaveWithTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store := NewRedisStore(client, "export:job:%s", 24*time.Hour)
	ctx := context.Background()

	if err := store.SaveWithTTL(ctx, "job-1", map[string]string{"status": StatusSucceeded}, 20*time.Minute); err != nil {
		t.Fatalf("SaveWithTTL() error = %v", err)
	}
	server.FastForward(19 * time.Minute)
	var status map[string]string
	if err := store.Load(ctx, "job-1", &status); err != nil {
		t.Fatalf("Load() before ttl error = %v", err)
	}
	server.FastForward(2 * time.Minute)
	if err := store.Load(ctx, "job-1", &status); err == nil {
		t.Fatal("Load() after ttl error = nil, want expired")
	}
}
