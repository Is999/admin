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

// TestWithLockUnlocksOnPanic 校验业务 panic 时仍会释放锁，并保持 panic 向上抛出。
func TestWithLockUnlocksOnPanic(t *testing.T) {
	// server 和 client 构造可重新竞争同一锁 key 的本地 Redis 环境。
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	// panicValue 和 recovered 校验 WithLock 只负责清理资源，不吞掉业务 panic。
	const panicValue = "lock callback panic"
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_ = WithLock(context.Background(), client, "lock:panic", time.Second, func(context.Context) error {
			panic(panicValue)
		})
	}()
	if recovered != panicValue {
		t.Fatalf("recovered panic = %v, want %q", recovered, panicValue)
	}
	if err := WithLock(context.Background(), client, "lock:panic", time.Second, nil); err != nil {
		t.Fatalf("expected lock to be released after panic, got %v", err)
	}
}

// TestWithLockPreservesAcquireContextError 校验等待锁期间超时会保留 context 错误语义。
func TestWithLockPreservesAcquireContextError(t *testing.T) {
	// server 和 client 构造锁竞争环境，由较短的父 deadline 中止后续退避。
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	// holder 先持有目标锁，确保被测调用进入竞争等待。
	holder := NewLock(client, "lock:acquire-timeout")
	if err := holder.TryLock(context.Background(), time.Second); err != nil {
		t.Fatalf("expected holder lock success, got %v", err)
	}
	defer holder.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := WithLock(ctx, client, "lock:acquire-timeout", time.Second, func(context.Context) error {
		t.Fatal("timed out lock callback should not run")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected acquisition deadline error, got %v", err)
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
			return errors.Tag(ctx.Err())
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
	if !errors.Is(err, ErrLockLost) {
		t.Fatalf("expected lock lost error, got %v", err)
	}
}
