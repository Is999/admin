package redislimit

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	// redisRatePhysicalPrefix 对齐 redis_rate 固定追加的物理 key 前缀。
	redisRatePhysicalPrefix = "rate:"
)

// TestLimiterSharesQuotaAndIsolatesKeys 验证多客户端共享额度且不同业务域、资源互不干扰。
func TestLimiterSharesQuotaAndIsolatesKeys(t *testing.T) {
	server := miniredis.RunT(t)
	firstClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = firstClient.Close()
		_ = secondClient.Close()
	})
	useLimiterAppID(t, "site-a")

	first := New(firstClient)
	second := New(secondClient)
	limit := Limit{Rate: 1, Burst: 1, Period: time.Second}
	if result, err := first.Allow(context.Background(), "audit", "log-write", limit); err != nil || !result.Allowed {
		t.Fatalf("首个客户端 Allow() result=%+v error=%v", result, err)
	}
	if result, err := second.Allow(context.Background(), "audit", "log-write", limit); err != nil || result.Allowed || result.Remaining != 0 || result.RetryAfter <= 0 || result.ResetAfter < 0 {
		t.Fatalf("第二个客户端应共享同 key 额度 result=%+v error=%v", result, err)
	}
	for _, target := range [][2]string{{"message", "log-write"}, {"audit", "main-write"}} {
		if result, err := second.Allow(context.Background(), target[0], target[1], limit); err != nil || !result.Allowed {
			t.Fatalf("独立 key Allow() scope=%s resource=%s result=%+v error=%v", target[0], target[1], result, err)
		}
	}

	auditKey := keys.RateLimitRedisKey("audit", "log-write")
	physicalKey := physicalRateLimitKey(auditKey)
	if got := server.Type(physicalKey); got != "string" {
		t.Fatalf("物理限流 key 类型=%q, want string", got)
	}
	if ttl := server.TTL(physicalKey); ttl <= 0 {
		t.Fatalf("物理限流 key TTL=%s, want positive", ttl)
	}
}

// TestLimiterAllowsExactlyBurstAcrossClients 验证并发多实例对同一 key 最多放行 Burst 次。
func TestLimiterAllowsExactlyBurstAcrossClients(t *testing.T) {
	server := miniredis.RunT(t)
	clients := []*redis.Client{
		redis.NewClient(&redis.Options{Addr: server.Addr()}),
		redis.NewClient(&redis.Options{Addr: server.Addr()}),
	}
	for _, client := range clients {
		current := client
		t.Cleanup(func() { _ = current.Close() })
	}
	useLimiterAppID(t, "site-concurrent")
	limiters := []*Limiter{New(clients[0]), New(clients[1])}
	limit := Limit{Rate: 1, Burst: 7, Period: time.Hour}
	if got := countConcurrentAllowed(t, context.Background(), limiters, "audit", "concurrent-write", limit, 64); got != int64(limit.Burst) {
		t.Fatalf("并发多客户端放行数=%d, want=%d", got, limit.Burst)
	}
}

// TestLimiterRefillsAtConfiguredRate 验证额度按 Rate 和 Period 逐步补充，并返回可用的重试时间。
func TestLimiterRefillsAtConfiguredRate(t *testing.T) {
	server := miniredis.RunT(t)
	startedAt := time.Unix(1_700_000_000, 0)
	server.SetTime(startedAt)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	useLimiterAppID(t, "site-refill")
	limiter := New(client)
	limit := Limit{Rate: 2, Burst: 2, Period: time.Second}

	for attempt := 0; attempt < limit.Burst; attempt++ {
		if result, err := limiter.Allow(context.Background(), "audit", "refill", limit); err != nil || !result.Allowed {
			t.Fatalf("初始额度申请 attempt=%d result=%+v err=%v", attempt, result, err)
		}
	}
	result, err := limiter.Allow(context.Background(), "audit", "refill", limit)
	if err != nil || result.Allowed || result.Remaining != 0 {
		t.Fatalf("初始额度耗尽后应拒绝 result=%+v err=%v", result, err)
	}
	if result.RetryAfter < 499*time.Millisecond || result.RetryAfter > 501*time.Millisecond {
		t.Fatalf("额度重试时间=%s, want about 500ms", result.RetryAfter)
	}

	server.SetTime(startedAt.Add(500 * time.Millisecond))
	if result, err = limiter.Allow(context.Background(), "audit", "refill", limit); err != nil || !result.Allowed || result.Remaining != 0 {
		t.Fatalf("单个额度补充后应放行一次 result=%+v err=%v", result, err)
	}
	if result, err = limiter.Allow(context.Background(), "audit", "refill", limit); err != nil || result.Allowed || result.RetryAfter <= 0 {
		t.Fatalf("补充额度消费后应再次拒绝 result=%+v err=%v", result, err)
	}
}

// TestLimiterRecoversAfterStateExpiry 验证限流状态 TTL 到期后额度自动恢复。
func TestLimiterRecoversAfterStateExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	useLimiterAppID(t, "site-expiry")
	limiter := New(client)
	limit := Limit{Rate: 1, Burst: 1, Period: time.Second}
	if result, err := limiter.Allow(context.Background(), "audit", "expiry", limit); err != nil || !result.Allowed {
		t.Fatalf("首次限流调用 result=%+v err=%v", result, err)
	}
	if result, err := limiter.Allow(context.Background(), "audit", "expiry", limit); err != nil || result.Allowed {
		t.Fatalf("额度耗尽后应拒绝 result=%+v err=%v", result, err)
	}
	server.FastForward(2 * time.Second)
	if result, err := limiter.Allow(context.Background(), "audit", "expiry", limit); err != nil || !result.Allowed {
		t.Fatalf("限流状态过期后应恢复 result=%+v err=%v", result, err)
	}
}

// TestLimiterHonorsInFlightContextDeadline 验证 Redis 已连接但不响应时仍受调用方 deadline 约束。
func TestLimiterHonorsInFlightContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建无响应 Redis 测试监听失败: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	client := redis.NewClient(&redis.Options{
		Addr:                  listener.Addr().String(),
		ContextTimeoutEnabled: true,
		MaxRetries:            -1,
		DialTimeout:           time.Second,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          5 * time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	useLimiterAppID(t, "site-deadline")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	result, err := New(client).Allow(ctx, "audit", "deadline", Limit{Rate: 1, Burst: 1, Period: time.Second})
	if err == nil || result.Allowed {
		t.Fatalf("无响应 Redis 不应放行 result=%+v err=%v", result, err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("Redis 限流未遵守 context deadline，耗时=%s", elapsed)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("限流测试未连接到无响应 Redis")
	}
}

// TestLimiterIsolatesApplications 验证相同业务资源在不同 app_id 下不共享额度。
func TestLimiterIsolatesApplications(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	limiter := New(client)
	limit := Limit{Rate: 1, Burst: 1, Period: time.Second}
	previous := runtimecfg.Get()
	t.Cleanup(func() { runtimecfg.Restore(previous) })

	runtimecfg.Set(config.Config{AppID: "site-a"})
	if result, err := limiter.Allow(context.Background(), "audit", "write", limit); err != nil || !result.Allowed {
		t.Fatalf("site-a Allow() result=%+v error=%v", result, err)
	}
	runtimecfg.Set(config.Config{AppID: "site-b"})
	if result, err := limiter.Allow(context.Background(), "audit", "write", limit); err != nil || !result.Allowed {
		t.Fatalf("site-b Allow() result=%+v error=%v", result, err)
	}
	if result, err := limiter.Allow(context.Background(), "audit", "write", limit); err != nil || result.Allowed {
		t.Fatalf("site-b 第二次调用应共享自身额度 result=%+v error=%v", result, err)
	}
}

// TestLimiterFailsClosedWhenRedisUnavailable 验证 Redis 异常不会被误判为放行。
func TestLimiterFailsClosedWhenRedisUnavailable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr:         server.Addr(),
		MaxRetries:   -1,
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
	})
	t.Cleanup(func() { _ = client.Close() })
	useLimiterAppID(t, "site-error")
	server.Close()

	result, err := New(client).Allow(context.Background(), "audit", "error", Limit{Rate: 1, Burst: 1, Period: time.Second})
	if err == nil || result.Allowed {
		t.Fatalf("Redis 异常 result=%+v error=%v", result, err)
	}
}

// TestLimiterRejectsInvalidRequests 验证未初始化、非法规则和未治理 key 在访问 Redis 前被拒绝。
func TestLimiterRejectsInvalidRequests(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	useLimiterAppID(t, "site-validate")
	limiter := New(client)
	validLimit := Limit{Rate: 1, Burst: 1, Period: time.Second}

	tests := []struct {
		name     string          // 用例名称
		limiter  *Limiter        // 待测限流器
		ctx      context.Context // 调用上下文
		scope    string          // 业务域
		resource string          // 资源标识
		limit    Limit           // 限流规则
	}{
		{name: "nil limiter", ctx: context.Background(), scope: "audit", resource: "write", limit: validLimit},
		{name: "empty client", limiter: New(nil), ctx: context.Background(), scope: "audit", resource: "write", limit: validLimit},
		{name: "nil context", limiter: limiter, scope: "audit", resource: "write", limit: validLimit},
		{name: "empty scope", limiter: limiter, ctx: context.Background(), resource: "write", limit: validLimit},
		{name: "uppercase scope", limiter: limiter, ctx: context.Background(), scope: "Audit", resource: "write", limit: validLimit},
		{name: "resource hash tag", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write{hot}", limit: validLimit},
		{name: "resource whitespace", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write log", limit: validLimit},
		{name: "control character", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write\n", limit: validLimit},
		{name: "long resource", limiter: limiter, ctx: context.Background(), scope: "audit", resource: strings.Repeat("a", 600), limit: validLimit},
		{name: "zero rate", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write", limit: Limit{Burst: 1, Period: time.Second}},
		{name: "zero burst", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write", limit: Limit{Rate: 1, Period: time.Second}},
		{name: "short period", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write", limit: Limit{Rate: 1, Burst: 1, Period: time.Microsecond}},
		{name: "fast emission", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write", limit: Limit{Rate: maxQuota, Burst: 1, Period: time.Millisecond}},
		{name: "long reset", limiter: limiter, ctx: context.Background(), scope: "audit", resource: "write", limit: Limit{Rate: 1, Burst: 2, Period: maxResetWindow}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := testCase.limiter.Allow(testCase.ctx, testCase.scope, testCase.resource, testCase.limit); err == nil {
				t.Fatal("Allow() error=nil")
			}
		})
	}

	var typedNil *redis.Client
	if _, err := New(typedNil).Allow(context.Background(), "audit", "write", validLimit); err == nil {
		t.Fatal("typed nil Redis client should fail")
	}
}

// useLimiterAppID 为单测设置应用级 Redis 命名空间。
func useLimiterAppID(t *testing.T, appID string) {
	t.Helper()
	previous := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() { runtimecfg.Restore(previous) })
}

// countConcurrentAllowed 并发申请同一额度，并返回成功放行次数。
func countConcurrentAllowed(t *testing.T, ctx context.Context, limiters []*Limiter, scope string, resource string, limit Limit, attempts int) int64 {
	t.Helper()
	var allowed atomic.Int64
	errCh := make(chan error, attempts)
	var wg sync.WaitGroup
	for index := 0; index < attempts; index++ {
		wg.Add(1)
		go func(current int) {
			defer wg.Done()
			result, err := limiters[current%len(limiters)].Allow(ctx, scope, resource, limit)
			if err != nil {
				errCh <- err
				return
			}
			if result.Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发限流调用失败: %v", err)
	}
	return allowed.Load()
}

// physicalRateLimitKey 返回 redis_rate 最终写入 Redis 的物理 key。
func physicalRateLimitKey(logicalKey string) string {
	return redisRatePhysicalPrefix + logicalKey
}
