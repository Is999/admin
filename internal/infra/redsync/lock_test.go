package redsync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Is999/go-utils/errors"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestTryLockRejectsNilRedisClient 校验 nil Redis 客户端会返回明确错误，而不是在 redsync 内部 panic。
func TestTryLockRejectsNilRedisClient(t *testing.T) {
	// lock 模拟调用方传入 nil Redis 客户端时创建出来的锁实例。
	lock := NewLock(nil, "lock:nil-client")

	err := lock.TryLock(context.Background(), time.Second)
	if err == nil {
		t.Fatal("expected nil redis client lock error, got nil")
	}
	if !strings.Contains(err.Error(), "Redis 锁未初始化") {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

// TestIsLockTakenDetectsContention 校验锁竞争错误能被识别为可跳过的互斥冲突。
func TestIsLockTakenDetectsContention(t *testing.T) {
	// server 和 client 使用 miniredis 构造同一把锁的竞争场景，避免依赖真实 Redis。
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	lock := NewLock(client, "lock:busy")
	if err := lock.TryLock(context.Background(), time.Second); err != nil {
		t.Fatalf("expected first lock success, got %v", err)
	}
	defer lock.Unlock()

	err := WithLock(context.Background(), client, "lock:busy", time.Second, func(context.Context) error {
		t.Fatal("second lock holder should not run")
		return nil
	})
	if err == nil {
		t.Fatal("expected lock contention error, got nil")
	}
	if !IsLockTaken(err) {
		t.Fatalf("expected lock taken error, got %v", err)
	}
}

// TestWithLockReturnsUnlockError 校验释放锁失败时 WithLock 会把错误返回给调用方。
func TestWithLockReturnsUnlockError(t *testing.T) {
	// server 和 client 使用 miniredis 构造可控的 Redis 环境，便于模拟释放阶段断连。
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	err := WithLock(context.Background(), client, "lock:unlock-error", time.Second, func(context.Context) error {
		server.Close()
		return nil
	})
	if err == nil {
		t.Fatal("expected unlock error, got nil")
	}
	if !strings.Contains(err.Error(), "释放 Redis 锁失败") {
		t.Fatalf("expected release failure, got %v", err)
	}
}

// TestWithLockCancelsContextOnRenewalFailure 校验续期失败后业务 context 会被主动取消。
func TestWithLockCancelsContextOnRenewalFailure(t *testing.T) {
	// server 和 client 在业务函数内主动关闭，用来触发后台续期失败。
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	err := WithLock(context.Background(), client, "lock:renewal-failure", 50*time.Millisecond, func(ctx context.Context) error {
		server.Close()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("lock context was not canceled")
		}
	})
	if err == nil {
		t.Fatal("expected renewal failure error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected protected context cancellation, got %v", err)
	}
}
