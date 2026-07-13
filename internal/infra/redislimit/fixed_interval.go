package redislimit

import (
	"context"
	"strconv"
	"sync"
	"time"

	keys "admin/common/rediskeys"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

const (
	// fixedIntervalRetryDelay 是令牌恰好过期时的最短重试等待，避免无间隔轮询 Redis。
	fixedIntervalRetryDelay = time.Millisecond
	// fixedIntervalNoExpiry 是 go-redis 对 Redis PTTL 无过期时间返回值的映射。
	fixedIntervalNoExpiry = -time.Nanosecond
	// fixedIntervalKeyNotFound 是 go-redis 对 Redis PTTL key 不存在返回值的映射。
	fixedIntervalKeyNotFound = -2 * time.Nanosecond
	// fixedIntervalTokenVersion 标识令牌值格式，便于发现同 key 不同间隔的错误复用。
	fixedIntervalTokenVersion = "fixed-v1:"
	// maxFixedIntervalLocalRequests 限制单进程同 key 的持有者和等待者总数，避免误用导致无界堆积。
	maxFixedIntervalLocalRequests = 256
)

// localGate 串行化当前进程内同一个固定间隔 Redis key 的等待请求。
type localGate struct {
	token chan struct{} // token 保证同 key 只有一个 goroutine 访问 Redis 等待令牌
	refs  int           // refs 记录持有和等待该 gate 的 goroutine 数量
}

// localGates 保存进程内固定间隔等待队列；Limiter 被误复制时仍共享同一容器。
type localGates struct {
	mu      sync.Mutex            // mu 保护 entries 的创建、引用计数和回收
	entries map[string]*localGate // entries 保存仍有持有者或等待者的 key 队列
}

// newLocalGates 创建固定间隔限流器的进程内队列容器。
func newLocalGates() *localGates {
	return &localGates{entries: make(map[string]*localGate)}
}

// WaitFixedInterval 等待指定业务资源的下一个固定间隔执行起点。
// interval 必须是 1ms 至 24h 的整毫秒；同一 scope 和 resource 在并行运行期间必须始终使用相同 interval。
// Redis 正常服务期间，本方法只保证相邻成功许可的发放时间至少间隔 interval，不等待业务完成，也不提供分布式互斥。
// 调用方必须传入带 deadline 的 context；Redis 异常、活跃令牌规则冲突、队列过载或上下文取消时失败关闭。
func (l *Limiter) WaitFixedInterval(ctx context.Context, scope string, resource string, interval time.Duration) error {
	key, err := validateWaitRequest(l, ctx, scope, resource, interval)
	if err != nil {
		return errors.Tag(err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "等待 Redis 固定间隔限流许可前上下文已取消")
	}

	release, err := l.acquireLocal(ctx, key)
	if err != nil {
		return errors.Tag(err)
	}
	defer release()

	tokenValue := fixedIntervalTokenVersion + strconv.FormatInt(interval.Milliseconds(), 10)
	for {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "等待 Redis 固定间隔限流许可失败")
		}
		granted, err := l.client.SetNX(ctx, key, tokenValue, interval).Result()
		if err != nil {
			return errors.Wrap(err, "获取 Redis 固定间隔限流许可失败")
		}
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "竞争 Redis 固定间隔限流许可后上下文已取消")
		}
		if granted {
			return nil
		}

		delay, err := l.nextFixedIntervalDelay(ctx, key, tokenValue, interval)
		if err != nil {
			return errors.Tag(err)
		}
		if err := waitFixedInterval(ctx, delay); err != nil {
			return errors.Wrap(err, "等待 Redis 固定间隔限流许可失败")
		}
	}
}

// validateWaitRequest 校验固定间隔规则，并生成按应用、业务域和资源隔离的 Redis key。
func validateWaitRequest(l *Limiter, ctx context.Context, scope string, resource string, interval time.Duration) (string, error) {
	if l == nil || isNilRedisClient(l.client) {
		return "", errors.New("Redis 分布式限流器未初始化")
	}
	if ctx == nil {
		return "", errors.New("Redis 分布式限流 context 不能为空")
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", errors.New("Redis 固定间隔限流 context 必须设置 deadline")
	}
	key := keys.RateLimitFixedIntervalRedisKey(scope, resource)
	if key == "" {
		return "", errors.New("Redis 固定间隔限流业务域、资源或 app_id 非法")
	}
	if interval < minPeriod || interval > maxResetWindow || interval%time.Millisecond != 0 {
		return "", errors.Errorf("Redis 固定间隔限流 interval 必须是 %s-%s 之间的整毫秒 interval=%s", minPeriod, maxResetWindow, interval)
	}
	return key, nil
}

// nextFixedIntervalDelay 在同 key 的 Redis 事务中读取令牌值和 TTL，校验规则后返回下一次竞争前的等待时间。
func (l *Limiter) nextFixedIntervalDelay(ctx context.Context, key string, tokenValue string, interval time.Duration) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, errors.Wrap(err, "读取 Redis 固定间隔限流令牌前上下文已取消")
	}

	var valueCmd *redis.StringCmd
	var delayCmd *redis.DurationCmd
	_, err := l.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// 只读取期望令牌长度再多一个字节，识别尾随内容且避免碰撞 key 的大 value 放大资源开销。
		valueCmd = pipe.GetRange(ctx, key, 0, int64(len(tokenValue)))
		delayCmd = pipe.PTTL(ctx, key)
		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "原子读取 Redis 固定间隔限流令牌失败")
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Wrap(err, "读取 Redis 固定间隔限流令牌后上下文已取消")
	}
	value, err := valueCmd.Result()
	if err != nil {
		return 0, errors.Wrap(err, "解析 Redis 固定间隔限流令牌失败")
	}
	delay, err := delayCmd.Result()
	if err != nil {
		return 0, errors.Wrap(err, "解析 Redis 固定间隔限流令牌 TTL 失败")
	}
	switch {
	case delay == fixedIntervalNoExpiry:
		return 0, errors.New("Redis 固定间隔限流令牌缺少 TTL")
	case delay == fixedIntervalKeyNotFound:
		return fixedIntervalRetryDelay, nil
	case delay < 0:
		return 0, errors.Errorf("Redis 固定间隔限流令牌 TTL 非法 ttl=%s", delay)
	case delay > interval:
		return 0, errors.Errorf("Redis 固定间隔限流令牌 TTL 超出间隔 ttl=%s interval=%s", delay, interval)
	case delay == 0:
		return fixedIntervalRetryDelay, nil
	}
	if value != tokenValue {
		return 0, errors.Errorf("Redis 固定间隔限流 key 被不同间隔或规则复用 expected_interval=%s", interval)
	}
	return delay, nil
}

// acquireLocal 获取当前进程内指定 key 的串行槽，并返回释放函数。
func (l *Limiter) acquireLocal(ctx context.Context, key string) (func(), error) {
	gates := l.gates
	if gates == nil {
		return nil, errors.New("Redis 固定间隔限流本地队列未初始化")
	}
	gates.mu.Lock()
	gate := gates.entries[key]
	if gate == nil {
		gate = &localGate{token: make(chan struct{}, 1)}
		gates.entries[key] = gate
	}
	if gate.refs >= maxFixedIntervalLocalRequests {
		gates.mu.Unlock()
		return nil, errors.Errorf("Redis 固定间隔限流本地队列已满 max=%d", maxFixedIntervalLocalRequests)
	}
	gate.refs++
	gates.mu.Unlock()

	select {
	case gate.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.token
			gates.releaseRef(key, gate)
			return nil, errors.Wrap(err, "等待 Redis 固定间隔限流本地队列失败")
		}
		return func() {
			<-gate.token
			gates.releaseRef(key, gate)
		}, nil
	case <-ctx.Done():
		gates.releaseRef(key, gate)
		return nil, errors.Wrap(ctx.Err(), "等待 Redis 固定间隔限流本地队列失败")
	}
}

// releaseRef 释放 gate 引用，并在最后一个使用者退出时回收 key，避免动态 key 常驻内存。
func (g *localGates) releaseRef(key string, gate *localGate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	gate.refs--
	if gate.refs == 0 && g.entries[key] == gate {
		delete(g.entries, key)
	}
}

// waitFixedInterval 按上下文等待指定时长，确保任务取消后不会继续竞争 Redis 许可。
func waitFixedInterval(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return errors.Tag(ctx.Err())
	}
}
