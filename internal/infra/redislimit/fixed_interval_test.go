package redislimit

import (
	"context"
	stderrors "errors"
	"net"
	"strconv"
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

// cancelAfterSetHook 在 Redis SET 返回后取消调用上下文，用于覆盖许可瞬间的取消竞态。
type cancelAfterSetHook struct {
	cancel        context.CancelFunc // cancel 取消当前 WaitFixedInterval 使用的上下文
	afterSet      atomic.Bool        // afterSet 标记 SET 命令已经返回并触发取消
	pipelineCalls atomic.Int32       // pipelineCalls 记录取消后是否仍进入 Redis 事务
}

// cancelAfterTransactionHook 在令牌快照事务返回后取消上下文，并记录后续 Redis 访问。
type cancelAfterTransactionHook struct {
	cancel           context.CancelFunc // cancel 取消当前 WaitFixedInterval 使用的上下文
	afterTransaction atomic.Bool        // afterTransaction 标记令牌快照事务已经返回
	callsAfter       atomic.Int32       // callsAfter 记录事务后的 Redis 访问次数
}

// commandSignalHook 在指定 Redis 命令完成后通知测试协程。
type commandSignalHook struct {
	command string        // command 表示需要观察的小写 Redis 命令名
	called  chan struct{} // called 在目标命令首次完成后关闭
	once    sync.Once     // once 保证并发命令只关闭一次 channel
}

// malformedGCRAHook 把成功脚本响应替换为非法结构，验证第三方解析 panic 被局部收口。
type malformedGCRAHook struct{}

// DialHook 保持 Redis 建连行为不变。
func (h *cancelAfterSetHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 在 SET 命令返回后立即取消上下文。
func (h *cancelAfterSetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "set" {
			h.afterSet.Store(true)
			h.cancel()
		}
		return err
	}
}

// ProcessPipelineHook 记录 SET 触发取消后是否仍有 Redis 事务。
func (h *cancelAfterSetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if h.afterSet.Load() {
			h.pipelineCalls.Add(1)
		}
		return next(ctx, cmds)
	}
}

// DialHook 保持 Redis 建连行为不变。
func (h *cancelAfterTransactionHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 记录令牌快照事务返回后的 Redis 单命令访问。
func (h *cancelAfterTransactionHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.afterTransaction.Load() {
			h.callsAfter.Add(1)
		}
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook 在包含 PTTL 的令牌快照事务成功返回后取消上下文。
func (h *cancelAfterTransactionHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if h.afterTransaction.Load() {
			h.callsAfter.Add(1)
		}
		err := next(ctx, cmds)
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			if cmd.Name() == "pttl" {
				h.afterTransaction.Store(true)
				h.cancel()
				break
			}
		}
		return nil
	}
}

// DialHook 保持 Redis 建连行为不变。
func (h *commandSignalHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 在目标命令完成后通知测试协程。
func (h *commandSignalHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == h.command {
			h.once.Do(func() { close(h.called) })
		}
		return err
	}
}

// ProcessPipelineHook 在 pipeline 中观察目标命令完成信号。
func (h *commandSignalHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			if cmd.Name() == h.command {
				h.once.Do(func() { close(h.called) })
				break
			}
		}
		return err
	}
}

// DialHook 保持 Redis 建连行为不变。
func (h *malformedGCRAHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 把成功的 EVAL 响应改成非法类型。
func (h *malformedGCRAHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && (cmd.Name() == "eval" || cmd.Name() == "evalsha") {
			if result, ok := cmd.(*redis.Cmd); ok {
				result.SetVal("malformed")
			}
		}
		return err
	}
}

// ProcessPipelineHook 保持 pipeline 行为不变。
func (h *malformedGCRAHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// newFixedIntervalLimiter 创建连接到指定 miniredis 的独立限流器实例。
func newFixedIntervalLimiter(t *testing.T, server *miniredis.Miniredis) (*Limiter, *redis.Client) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	return New(client), client
}

// fixedIntervalTestContext 创建带 deadline 的测试上下文，满足阻塞限流 API 的有界等待契约。
func fixedIntervalTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestWaitFixedIntervalSharesKeyAcrossInstances 验证不同实例使用同 key 时共享一个分布式执行间隔。
func TestWaitFixedIntervalSharesKeyAcrossInstances(t *testing.T) {
	server := miniredis.RunT(t)
	first, _ := newFixedIntervalLimiter(t, server)
	second, _ := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-shared")
	ctx := fixedIntervalTestContext(t)

	const interval = 200 * time.Millisecond
	if err := first.WaitFixedInterval(ctx, "user_tag", "rolling_uid:write", interval); err != nil {
		t.Fatalf("首次许可失败: %v", err)
	}
	key := keys.RateLimitFixedIntervalRedisKey("user_tag", "rolling_uid:write")
	if ttl := server.TTL(key); ttl != interval {
		t.Fatalf("固定间隔 key TTL=%s, want=%s", ttl, interval)
	}

	blockedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := second.WaitFixedInterval(blockedCtx, "user_tag", "rolling_uid:write", interval); err == nil || !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("共享令牌存在时应等待到 context 超时，实际=%v", err)
	}

	server.FastForward(interval)
	if err := second.WaitFixedInterval(ctx, "user_tag", "rolling_uid:write", interval); err != nil {
		t.Fatalf("令牌到期后第二实例许可失败: %v", err)
	}
}

// TestWaitFixedIntervalRetriesWithinSameCall 验证单次调用会在现有令牌到期后自动重试并成功。
func TestWaitFixedIntervalRetriesWithinSameCall(t *testing.T) {
	server := miniredis.RunT(t)
	first, _ := newFixedIntervalLimiter(t, server)
	second, secondClient := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-retry")
	initialCtx := fixedIntervalTestContext(t)

	const interval = 20 * time.Millisecond
	if err := first.WaitFixedInterval(initialCtx, "job", "retry", interval); err != nil {
		t.Fatalf("首次许可失败: %v", err)
	}
	signal := &commandSignalHook{command: "pttl", called: make(chan struct{})}
	secondClient.AddHook(signal)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- second.WaitFixedInterval(ctx, "job", "retry", interval)
	}()

	select {
	case <-signal.called:
		server.FastForward(interval)
	case <-ctx.Done():
		t.Fatalf("第二次调用未读取现有令牌: %v", ctx.Err())
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("同一次调用未在令牌过期后成功重试: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("第二次调用未及时成功: %v", ctx.Err())
	}
}

// TestWaitFixedIntervalIsolatesResources 验证不同业务域和资源互不阻塞且状态独立。
func TestWaitFixedIntervalIsolatesResources(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, _ := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-isolation")
	ctx := fixedIntervalTestContext(t)

	targets := [][2]string{{"user_tag", "rolling_uid:write"}, {"user_tag", "profile:write"}, {"audit", "rolling_uid:write"}}
	for _, target := range targets {
		if err := limiter.WaitFixedInterval(ctx, target[0], target[1], time.Second); err != nil {
			t.Fatalf("独立 key 许可 scope=%s resource=%s error=%v", target[0], target[1], err)
		}
	}
	for _, target := range targets {
		if !server.Exists(keys.RateLimitFixedIntervalRedisKey(target[0], target[1])) {
			t.Fatalf("未生成独立固定间隔 key scope=%s resource=%s", target[0], target[1])
		}
	}
}

// TestWaitFixedIntervalIsolatesApplications 验证相同业务资源在不同 app_id 下互不共享固定间隔。
func TestWaitFixedIntervalIsolatesApplications(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, _ := newFixedIntervalLimiter(t, server)
	previous := runtimecfg.Get()
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	ctx := fixedIntervalTestContext(t)

	runtimecfg.Set(config.Config{AppID: "fixed-site-a"})
	if err := limiter.WaitFixedInterval(ctx, "job", "write", time.Second); err != nil {
		t.Fatalf("site-a 首次许可失败: %v", err)
	}
	firstKey := keys.RateLimitFixedIntervalRedisKey("job", "write")
	runtimecfg.Set(config.Config{AppID: "fixed-site-b"})
	if err := limiter.WaitFixedInterval(ctx, "job", "write", time.Second); err != nil {
		t.Fatalf("site-b 不应共享 site-a 固定间隔: %v", err)
	}
	secondKey := keys.RateLimitFixedIntervalRedisKey("job", "write")
	blockedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := limiter.WaitFixedInterval(blockedCtx, "job", "write", time.Second); err == nil || !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("site-b 第二次调用应受自身固定间隔约束: %v", err)
	}
	if firstKey == secondKey || !server.Exists(firstKey) || !server.Exists(secondKey) {
		t.Fatalf("app_id 固定间隔 key 隔离失败 first=%q second=%q", firstKey, secondKey)
	}
}

// TestWaitFixedIntervalAllowsOneConcurrentStarter 验证同进程同 key 的并发请求只放行一个起点。
func TestWaitFixedIntervalAllowsOneConcurrentStarter(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, _ := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-local-concurrent")

	assertOneConcurrentFixedIntervalStarter(t, []*Limiter{limiter}, "local")
}

// TestWaitFixedIntervalAllowsOneConcurrentStarterAcrossInstances 验证多实例同 key 并发只放行一个起点。
func TestWaitFixedIntervalAllowsOneConcurrentStarterAcrossInstances(t *testing.T) {
	server := miniredis.RunT(t)
	limiters := make([]*Limiter, 0, 12)
	for range 12 {
		limiter, client := newFixedIntervalLimiter(t, server)
		if err := client.Ping(context.Background()).Err(); err != nil {
			t.Fatalf("预热 Redis 客户端失败: %v", err)
		}
		limiters = append(limiters, limiter)
	}
	useLimiterAppID(t, "fixed-distributed-concurrent")

	assertOneConcurrentFixedIntervalStarter(t, limiters, "distributed")
}

// assertOneConcurrentFixedIntervalStarter 并发竞争固定间隔许可并断言仅一个成功。
func assertOneConcurrentFixedIntervalStarter(t *testing.T, limiters []*Limiter, resource string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	var successes atomic.Int32
	var waiters sync.WaitGroup
	errorsCh := make(chan error, 24)
	for index := 0; index < 24; index++ {
		waiters.Add(1)
		go func(current int) {
			defer waiters.Done()
			<-start
			err := limiters[current%len(limiters)].WaitFixedInterval(ctx, "job", resource, time.Second)
			if err == nil {
				if successes.Add(1) == 1 {
					cancel()
				}
				return
			}
			if !stderrors.Is(err, context.Canceled) {
				errorsCh <- err
			}
		}(index)
	}
	close(start)
	waiters.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("并发固定间隔调用返回意外错误: %v", err)
	}
	if got := successes.Load(); got != 1 {
		t.Fatalf("并发固定间隔许可数=%d, want=1", got)
	}
}

// TestLocalGatesRespectContextReclaimAndSurviveLimiterCopy 验证本地队列取消、回收和误复制共享语义。
func TestLocalGatesRespectContextReclaimAndSurviveLimiterCopy(t *testing.T) {
	limiter := New(nil)
	copied := *limiter
	if copied.gates != limiter.gates {
		t.Fatal("Limiter 值复制后必须共享本地队列容器")
	}
	const key = "app:test:rate_limit:fixed_interval:job:local"
	release, err := limiter.acquireLocal(context.Background(), key)
	if err != nil {
		t.Fatalf("获取首个本地队列失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := copied.acquireLocal(ctx, key); err == nil || !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("复制实例未共享本地队列或未响应 context: %v", err)
	}
	release()

	limiter.gates.mu.Lock()
	defer limiter.gates.mu.Unlock()
	if len(limiter.gates.entries) != 0 {
		t.Fatalf("本地队列未回收，剩余=%d", len(limiter.gates.entries))
	}
}

// TestLocalGatesDoNotBlockDifferentKeys 验证一个 key 占用本地队列时不阻塞另一个 key。
func TestLocalGatesDoNotBlockDifferentKeys(t *testing.T) {
	limiter := New(nil)
	releaseFirst, err := limiter.acquireLocal(context.Background(), "first")
	if err != nil {
		t.Fatalf("获取首个 key 失败: %v", err)
	}
	defer releaseFirst()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	releaseSecond, err := limiter.acquireLocal(ctx, "second")
	if err != nil {
		t.Fatalf("第二个 key 不应被首个 key 阻塞: %v", err)
	}
	releaseSecond()
}

// TestLocalGatesRejectOverload 验证同 key 本地请求达到上限后立即失败关闭且不增加引用。
func TestLocalGatesRejectOverload(t *testing.T) {
	limiter := New(nil)
	const key = "overload"
	gate := &localGate{token: make(chan struct{}, 1), refs: maxFixedIntervalLocalRequests}
	limiter.gates.mu.Lock()
	limiter.gates.entries[key] = gate
	limiter.gates.mu.Unlock()

	release, err := limiter.acquireLocal(context.Background(), key)
	if err == nil || release != nil || !strings.Contains(err.Error(), "本地队列已满") {
		t.Fatalf("本地队列过载应立即失败关闭 release=%v error=%v", release != nil, err)
	}
	limiter.gates.mu.Lock()
	defer limiter.gates.mu.Unlock()
	if gate.refs != maxFixedIntervalLocalRequests {
		t.Fatalf("过载请求不应增加 gate 引用 refs=%d", gate.refs)
	}
}

// TestWaitFixedIntervalRejectsConflictingRule 验证同 key 混用不同间隔时失败关闭。
func TestWaitFixedIntervalRejectsConflictingRule(t *testing.T) {
	server := miniredis.RunT(t)
	first, _ := newFixedIntervalLimiter(t, server)
	second, _ := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-conflict")
	ctx := fixedIntervalTestContext(t)

	if err := first.WaitFixedInterval(ctx, "job", "conflict", 200*time.Millisecond); err != nil {
		t.Fatalf("首次规则许可失败: %v", err)
	}
	err := second.WaitFixedInterval(ctx, "job", "conflict", 500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "不同间隔或规则复用") {
		t.Fatalf("不同间隔应失败关闭，实际=%v", err)
	}
	key := keys.RateLimitFixedIntervalRedisKey("job", "conflict")
	if strings.Contains(err.Error(), key) {
		t.Fatalf("规则冲突错误泄漏 Redis key: %v", err)
	}
}

// TestWaitFixedIntervalAllowsRuleChangeAfterDrain 验证旧令牌过期后可由已协调的新规则接管。
func TestWaitFixedIntervalAllowsRuleChangeAfterDrain(t *testing.T) {
	server := miniredis.RunT(t)
	oldLimiter, _ := newFixedIntervalLimiter(t, server)
	newLimiter, _ := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-rule-change")
	ctx := fixedIntervalTestContext(t)

	if err := oldLimiter.WaitFixedInterval(ctx, "job", "rule-change", 200*time.Millisecond); err != nil {
		t.Fatalf("旧规则首次许可失败: %v", err)
	}
	server.FastForward(200 * time.Millisecond)
	if err := newLimiter.WaitFixedInterval(ctx, "job", "rule-change", 500*time.Millisecond); err != nil {
		t.Fatalf("旧调用方排空且令牌过期后新规则应可接管: %v", err)
	}
	if err := oldLimiter.WaitFixedInterval(ctx, "job", "rule-change", 200*time.Millisecond); err == nil {
		t.Fatal("新规则令牌存活期间旧规则调用应失败关闭")
	}
}

// TestWaitFixedIntervalDoesNotExposeForeignValue 验证 key 碰撞错误不会泄露实际 Redis 值。
func TestWaitFixedIntervalDoesNotExposeForeignValue(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, client := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-foreign")
	ctx := fixedIntervalTestContext(t)

	const interval = 200 * time.Millisecond
	const expectedToken = fixedIntervalTokenVersion + "200"
	values := []string{"sensitive-session-token-should-not-appear", expectedToken + "x", fixedIntervalTokenVersion}
	for index, value := range values {
		resource := "foreign:" + strconv.Itoa(index)
		key := keys.RateLimitFixedIntervalRedisKey("job", resource)
		if err := client.Set(context.Background(), key, value, interval).Err(); err != nil {
			t.Fatalf("预置外来值失败: %v", err)
		}
		err := limiter.WaitFixedInterval(ctx, "job", resource, interval)
		if err == nil || !strings.Contains(err.Error(), "不同间隔或规则复用") {
			t.Fatalf("外来值碰撞应失败关闭，实际=%v", err)
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), value) {
			t.Fatalf("碰撞错误泄漏 Redis key 或 value: %v", err)
		}
	}
}

// TestWaitFixedIntervalRejectsInvalidTokenState 验证错误类型、永久令牌和超长 TTL 均失败关闭。
func TestWaitFixedIntervalRejectsInvalidTokenState(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, client := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-state")
	ctx := fixedIntervalTestContext(t)

	const interval = 200 * time.Millisecond
	tests := []struct {
		name string             // name 表示异常状态名称
		seed func(string) error // seed 写入异常 Redis 状态
		want string             // want 表示期望错误片段
	}{
		{name: "missing ttl", want: "缺少 TTL", seed: func(key string) error {
			return client.Set(context.Background(), key, fixedIntervalTokenVersion+"200", 0).Err()
		}},
		{name: "oversized ttl", want: "TTL 超出间隔", seed: func(key string) error {
			return client.Set(context.Background(), key, fixedIntervalTokenVersion+"200", time.Minute).Err()
		}},
		{name: "wrong type", want: "原子读取", seed: func(key string) error { return client.LPush(context.Background(), key, "value").Err() }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resource := "state:" + strings.ReplaceAll(testCase.name, " ", "-")
			key := keys.RateLimitFixedIntervalRedisKey("job", resource)
			if err := testCase.seed(key); err != nil {
				t.Fatalf("预置异常 Redis 状态失败: %v", err)
			}
			err := limiter.WaitFixedInterval(ctx, "job", resource, interval)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("异常状态应包含 %q，实际=%v", testCase.want, err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("异常状态错误泄漏 Redis key: %v", err)
			}
		})
	}
}

// TestWaitFixedIntervalRejectsInvalidRequests 验证非法输入、取消和 Redis 故障均不会放行。
func TestWaitFixedIntervalRejectsInvalidRequests(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, client := newFixedIntervalLimiter(t, server)
	useLimiterAppID(t, "fixed-validation")
	validCtx := fixedIntervalTestContext(t)

	tests := []struct {
		name     string          // name 表示用例名称
		limiter  *Limiter        // limiter 表示待测实例
		ctx      context.Context // ctx 表示调用上下文
		scope    string          // scope 表示业务域
		resource string          // resource 表示资源
		interval time.Duration   // interval 表示固定间隔
	}{
		{name: "nil limiter", ctx: validCtx, scope: "job", resource: "write", interval: time.Second},
		{name: "nil client", limiter: New(nil), ctx: validCtx, scope: "job", resource: "write", interval: time.Second},
		{name: "nil context", limiter: limiter, scope: "job", resource: "write", interval: time.Second},
		{name: "missing deadline", limiter: limiter, ctx: context.Background(), scope: "job", resource: "write", interval: time.Second},
		{name: "empty scope", limiter: limiter, ctx: validCtx, resource: "write", interval: time.Second},
		{name: "invalid resource", limiter: limiter, ctx: validCtx, scope: "job", resource: "write{hot}", interval: time.Second},
		{name: "zero interval", limiter: limiter, ctx: validCtx, scope: "job", resource: "write", interval: 0},
		{name: "sub millisecond", limiter: limiter, ctx: validCtx, scope: "job", resource: "write", interval: 500 * time.Microsecond},
		{name: "fractional millisecond", limiter: limiter, ctx: validCtx, scope: "job", resource: "write", interval: 1500 * time.Microsecond},
		{name: "over max", limiter: limiter, ctx: validCtx, scope: "job", resource: "write", interval: maxResetWindow + time.Millisecond},
	}
	commandsBefore := server.CommandCount()
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.limiter.WaitFixedInterval(testCase.ctx, testCase.scope, testCase.resource, testCase.interval); err == nil {
				t.Fatal("WaitFixedInterval() error=nil")
			}
		})
	}
	if commandsAfter := server.CommandCount(); commandsAfter != commandsBefore {
		t.Fatalf("非法参数不应访问 Redis: before=%d after=%d", commandsBefore, commandsAfter)
	}

	var typedNil *redis.Client
	if err := New(typedNil).WaitFixedInterval(validCtx, "job", "typed-nil", time.Second); err == nil {
		t.Fatal("typed nil Redis client 应失败")
	}
	canceledCtx, cancel := context.WithCancel(validCtx)
	cancel()
	if err := limiter.WaitFixedInterval(canceledCtx, "job", "canceled", time.Second); err == nil || !stderrors.Is(err, context.Canceled) {
		t.Fatalf("已取消 context 应失败，实际=%v", err)
	}
	if server.Exists(keys.RateLimitFixedIntervalRedisKey("job", "canceled")) {
		t.Fatal("已取消请求不应写入 Redis 令牌")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("关闭 Redis 客户端失败: %v", err)
	}
	if err := limiter.WaitFixedInterval(validCtx, "job", "closed", time.Second); err == nil {
		t.Fatal("关闭的 Redis 客户端应失败")
	}
}

// TestWaitFixedIntervalRequiresAppID 验证缺少应用命名空间时失败且不访问 Redis。
func TestWaitFixedIntervalRequiresAppID(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, _ := newFixedIntervalLimiter(t, server)
	previous := runtimecfg.Get()
	runtimecfg.Set(config.Config{})
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	ctx := fixedIntervalTestContext(t)

	before := server.CommandCount()
	if err := limiter.WaitFixedInterval(ctx, "job", "write", time.Second); err == nil {
		t.Fatal("缺少 app_id 应失败")
	}
	if after := server.CommandCount(); after != before {
		t.Fatalf("缺少 app_id 不应访问 Redis: before=%d after=%d", before, after)
	}
}

// TestWaitFixedIntervalHonorsInFlightDeadline 验证 Redis 无响应时受调用方 deadline 约束。
func TestWaitFixedIntervalHonorsInFlightDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建无响应 Redis 监听失败: %v", err)
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
	useLimiterAppID(t, "fixed-deadline")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	if err := New(client).WaitFixedInterval(ctx, "job", "deadline", time.Second); err == nil {
		t.Fatal("无响应 Redis 不应放行")
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("固定间隔限流未遵守 context deadline，耗时=%s", elapsed)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("测试未连接到无响应 Redis")
	}
}

// TestWaitFixedIntervalStopsAfterDeniedSetCancellation 验证 SET 未获许可时取消后不再读取令牌快照。
func TestWaitFixedIntervalStopsAfterDeniedSetCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	useLimiterAppID(t, "fixed-cancel-set")
	seedClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = seedClient.Close() })
	key := keys.RateLimitFixedIntervalRedisKey("job", "denied-set")
	const interval = 200 * time.Millisecond
	if err := seedClient.Set(context.Background(), key, fixedIntervalTokenVersion+"200", interval).Err(); err != nil {
		t.Fatalf("预置令牌失败: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	hook := &cancelAfterSetHook{cancel: cancel}
	client.AddHook(hook)
	err := New(client).WaitFixedInterval(ctx, "job", "denied-set", interval)
	if err == nil || !stderrors.Is(err, context.Canceled) {
		t.Fatalf("SET 后取消应失败，实际=%v", err)
	}
	if got := hook.pipelineCalls.Load(); got != 0 {
		t.Fatalf("取消后不应进入令牌快照事务，实际=%d", got)
	}
}

// TestWaitFixedIntervalStopsAfterTransactionCancellation 验证快照事务后取消不会继续竞争 Redis。
func TestWaitFixedIntervalStopsAfterTransactionCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	useLimiterAppID(t, "fixed-cancel-transaction")
	seedClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = seedClient.Close() })
	key := keys.RateLimitFixedIntervalRedisKey("job", "transaction")
	const interval = 200 * time.Millisecond
	if err := seedClient.Set(context.Background(), key, fixedIntervalTokenVersion+"200", interval).Err(); err != nil {
		t.Fatalf("预置令牌失败: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	hook := &cancelAfterTransactionHook{cancel: cancel}
	client.AddHook(hook)
	err := New(client).WaitFixedInterval(ctx, "job", "transaction", interval)
	if err == nil || !stderrors.Is(err, context.Canceled) {
		t.Fatalf("事务后取消应失败，实际=%v", err)
	}
	if got := hook.callsAfter.Load(); got != 0 {
		t.Fatalf("取消后不应继续访问 Redis，实际=%d", got)
	}
}

// TestWaitFixedIntervalRejectsCancellationAfterGrant 验证 Redis 刚写入许可时取消也不向业务返回成功。
func TestWaitFixedIntervalRejectsCancellationAfterGrant(t *testing.T) {
	server := miniredis.RunT(t)
	useLimiterAppID(t, "fixed-cancel-grant")
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	client.AddHook(&cancelAfterSetHook{cancel: cancel})

	err := New(client).WaitFixedInterval(ctx, "job", "grant", 200*time.Millisecond)
	if err == nil || !stderrors.Is(err, context.Canceled) {
		t.Fatalf("Redis 许可后取消应失败，实际=%v", err)
	}
	key := keys.RateLimitFixedIntervalRedisKey("job", "grant")
	if !server.Exists(key) || server.TTL(key) <= 0 {
		t.Fatal("取消后应保留短 TTL 令牌以保持失败关闭")
	}
}

// TestAllowFailsClosedForMalformedScriptResult 验证第三方 GCRA 解析异常不会 panic 击穿进程。
func TestAllowFailsClosedForMalformedScriptResult(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(&malformedGCRAHook{})
	useLimiterAppID(t, "gcra-malformed")

	result, err := New(client).Allow(context.Background(), "audit", "malformed", Limit{Rate: 1, Burst: 1, Period: time.Second})
	if err == nil || result.Allowed || !strings.Contains(err.Error(), "返回格式异常") {
		t.Fatalf("非法 GCRA 响应应失败关闭 result=%+v error=%v", result, err)
	}
}
