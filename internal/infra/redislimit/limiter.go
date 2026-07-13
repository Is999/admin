// Package redislimit 提供按应用、业务域和资源隔离的 Redis 分布式限流能力。
package redislimit

import (
	"context"
	"reflect"
	"time"

	keys "admin/common/rediskeys"

	"github.com/Is999/go-utils/errors"
	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

const (
	// maxQuota 限制单条规则的速率和突发额度，避免异常参数放大状态窗口。
	maxQuota = 1_000_000
	// minPeriod 限制规则周期下界，避免无意义的亚毫秒规则。
	minPeriod = time.Millisecond
	// maxResetWindow 限制限流状态的最长自动过期时间。
	maxResetWindow = 24 * time.Hour
)

// Limit 定义一个稳定的分布式限流规则；同一个 key 必须始终使用同一规则。
type Limit struct {
	Rate   int           // Period 内持续补充的额度数量
	Burst  int           // 可立即消费的最大突发额度
	Period time.Duration // Rate 对应的统计周期
}

// Result 描述一次额度申请结果。
type Result struct {
	Allowed    bool          // 是否获得一个额度
	Remaining  int           // 当前可立即消费的剩余额度
	RetryAfter time.Duration // 未获批时距离下一次可重试的时间
	ResetAfter time.Duration // 当前状态恢复到满额度所需时间
}

// Limiter 提供 GCRA 配额与固定间隔两种 Redis 分布式限流能力。
// 同一个 Limiter 可被并发复用，调用方应在进程生命周期内复用 New 返回的指针。
type Limiter struct {
	client redis.UniversalClient // client 兼容 Redis 单机、哨兵和集群客户端
	inner  *redisrate.Limiter    // inner 执行成熟的单 key GCRA Lua 脚本
	gates  *localGates           // gates 收敛当前进程内同 key 的固定间隔 Redis 竞争
}

// New 创建支持项目 Redis 单机和 Cluster 客户端的分布式限流器。
func New(client redis.UniversalClient) *Limiter {
	limiter := &Limiter{
		client: client,
		gates:  newLocalGates(),
	}
	if isNilRedisClient(client) {
		return limiter
	}
	limiter.inner = redisrate.NewLimiter(client)
	return limiter
}

// Allow 按业务域和资源立即申请一个额度；Redis 异常时调用方不得继续受保护的操作。
func (l *Limiter) Allow(ctx context.Context, scope string, resource string, limit Limit) (Result, error) {
	scopedKey, err := validateAllowRequest(l, ctx, scope, resource, limit)
	if err != nil {
		return Result{}, errors.Tag(err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, errors.Wrap(err, "申请 Redis 分布式限流额度前上下文已取消")
	}

	result, err := allowGCRA(l.inner, ctx, scopedKey, redisrate.Limit{
		Rate:   limit.Rate,
		Burst:  limit.Burst,
		Period: limit.Period,
	})
	if err != nil {
		return Result{}, errors.Wrap(err, "执行 Redis 分布式限流失败")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, errors.Wrap(err, "申请 Redis 分布式限流额度后上下文已取消")
	}
	if result == nil {
		return Result{}, errors.New("Redis 分布式限流返回空结果")
	}
	if result.Allowed != 0 && result.Allowed != 1 {
		return Result{}, errors.Errorf("Redis 分布式限流返回非法放行数量 allowed=%d", result.Allowed)
	}
	if result.Remaining < 0 || result.Remaining > limit.Burst {
		return Result{}, errors.Errorf("Redis 分布式限流返回非法剩余额度 remaining=%d", result.Remaining)
	}
	if result.ResetAfter < 0 || result.ResetAfter > maxResetWindow+time.Second {
		return Result{}, errors.Errorf("Redis 分布式限流返回非法恢复时间 reset_after=%s", result.ResetAfter)
	}

	allowed := result.Allowed == 1
	if allowed {
		if result.RetryAfter >= 0 {
			return Result{}, errors.Errorf("Redis 分布式限流放行结果包含非法重试时间 retry_after=%s", result.RetryAfter)
		}
		return Result{
			Allowed:    true,
			Remaining:  result.Remaining,
			ResetAfter: result.ResetAfter,
		}, nil
	}
	if result.RetryAfter <= 0 {
		return Result{}, errors.Errorf("Redis 分布式限流返回非法重试时间 retry_after=%s", result.RetryAfter)
	}
	if result.Remaining != 0 {
		return Result{}, errors.Errorf("Redis 分布式限流拒绝结果包含非法剩余额度 remaining=%d", result.Remaining)
	}
	return Result{
		Allowed:    false,
		Remaining:  result.Remaining,
		RetryAfter: result.RetryAfter,
		ResetAfter: result.ResetAfter,
	}, nil
}

// allowGCRA 限定第三方解析边界，异常返回结构导致 panic 时按失败关闭返回错误。
func allowGCRA(inner *redisrate.Limiter, ctx context.Context, key string, limit redisrate.Limit) (result *redisrate.Result, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = errors.New("Redis GCRA 限流返回格式异常")
		}
	}()
	return inner.Allow(ctx, key, limit)
}

// validateAllowRequest 校验调用上下文和 GCRA 规则，并生成按应用、业务域、资源隔离的 Redis key。
func validateAllowRequest(l *Limiter, ctx context.Context, scope string, resource string, limit Limit) (string, error) {
	if l == nil || l.inner == nil {
		return "", errors.New("Redis 分布式限流器未初始化")
	}
	if ctx == nil {
		return "", errors.New("Redis 分布式限流 context 不能为空")
	}
	scopedKey := keys.RateLimitRedisKey(scope, resource)
	if scopedKey == "" {
		return "", errors.New("Redis 分布式限流业务域、资源或 app_id 非法")
	}
	if limit.Rate <= 0 || limit.Rate > maxQuota {
		return "", errors.Errorf("Redis 分布式限流 rate 必须在 1-%d 之间 rate=%d", maxQuota, limit.Rate)
	}
	if limit.Burst <= 0 || limit.Burst > maxQuota {
		return "", errors.Errorf("Redis 分布式限流 burst 必须在 1-%d 之间 burst=%d", maxQuota, limit.Burst)
	}
	if limit.Period < minPeriod || limit.Period > maxResetWindow {
		return "", errors.Errorf("Redis 分布式限流 period 必须在 %s-%s 之间 period=%s", minPeriod, maxResetWindow, limit.Period)
	}
	emissionInterval := limit.Period.Seconds() / float64(limit.Rate)
	if emissionInterval < float64(time.Microsecond)/float64(time.Second) {
		return "", errors.Errorf("Redis 分布式限流额度发放间隔不能小于 %s", time.Microsecond)
	}
	resetSeconds := emissionInterval * float64(limit.Burst)
	if resetSeconds > maxResetWindow.Seconds() {
		return "", errors.Errorf("Redis 分布式限流状态恢复时间不能超过 %s", maxResetWindow)
	}
	return scopedKey, nil
}

// isNilRedisClient 同时识别 nil 接口和接口中的 typed nil。
func isNilRedisClient(client redis.UniversalClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
