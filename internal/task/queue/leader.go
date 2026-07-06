package taskqueue

import (
	"context"
	"time"

	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// leaderReleaseTimeout 是停止时主动释放 leader 锁的最大等待时间，避免 Redis 异常拖住进程退出。
	leaderReleaseTimeout = 2 * time.Second
)

// LeaderRunner 通过 Redis 锁在多实例之间选出唯一调度器 leader。
// 这样每个实例都可以启动任务模块，但周期调度只会由 leader 负责，避免重复投递。
type LeaderRunner struct {
	client        redis.UniversalClient                 // Redis 客户端，用于竞争和续租 leader 锁
	key           string                                // leader 锁在 Redis 中的键名
	ttl           time.Duration                         // leader 锁租约时长
	renewInterval time.Duration                         // leader 锁续租间隔
	instanceID    string                                // 当前实例唯一标识
	onAcquire     func(context.Context) (func(), error) // 成为 leader 后执行的启动回调，返回释放函数
}

// NewLeaderRunner 创建分布式 leader 选举器。
func NewLeaderRunner(client redis.UniversalClient, key string, ttl, renewInterval time.Duration, onAcquire func(context.Context) (func(), error)) *LeaderRunner {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if renewInterval <= 0 || renewInterval >= ttl {
		renewInterval = ttl / 3
	}
	return &LeaderRunner{
		client:        client,
		key:           key,
		ttl:           ttl,
		renewInterval: renewInterval,
		instanceID:    uuid.NewString(),
		onAcquire:     onAcquire,
	}
}

// Start 启动 leader 选举循环，直到 ctx 结束。
func (r *LeaderRunner) Start(ctx context.Context) {
	if r == nil || r.client == nil || r.onAcquire == nil || r.key == "" {
		return
	}

	var (
		release  func()
		isLeader bool
	)
	ticker := time.NewTicker(r.renewInterval)
	defer ticker.Stop()

	for {
		if !isLeader {
			ok, err := r.client.SetNX(ctx, r.key, r.instanceID, r.ttl).Result()
			if err != nil {
				loggerx.Errorw(ctx, "任务队列 leader 竞争失败", errors.Wrap(err, "竞争 scheduler leader 锁"),
					logx.Field("key", r.key),
					logx.Field("instance", r.instanceID),
				)
			} else if ok {
				release, err = r.onAcquire(ctx)
				if err != nil {
					loggerx.Errorw(ctx, "任务队列 leader 启动调度器失败", errors.Wrap(err, "启动 scheduler leader 回调"),
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
					_ = r.release(ctx)
				} else {
					isLeader = true
					loggerx.Infow(ctx, "任务队列 leader 已获取",
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
				}
			}
		}

		select {
		case <-ctx.Done():
			if release != nil {
				release()
			}
			if isLeader {
				_ = r.releaseWithTimeout()
			}
			return
		case <-ticker.C:
			if !isLeader {
				continue
			}
			ok, err := renewLeaderScript.Run(ctx, r.client, []string{r.key}, r.instanceID, r.ttl.Milliseconds()).Int()
			if err != nil || ok == 0 {
				if err != nil {
					loggerx.Errorw(ctx, "任务队列 leader 续租失败", errors.Wrap(err, "续租 scheduler leader 锁"),
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
				} else {
					loggerx.Infow(ctx, "任务队列 leader 已丢失",
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
				}
				if release != nil {
					release()
					release = nil
				}
				isLeader = false
			}
		}
	}
}

// releaseWithTimeout 使用短超时释放当前实例的 leader 锁，避免停止流程被 Redis 阻塞。
func (r *LeaderRunner) releaseWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), leaderReleaseTimeout)
	defer cancel()
	return r.release(ctx)
}

// release 主动释放当前实例持有的 leader 锁。
func (r *LeaderRunner) release(ctx context.Context) error {
	if r == nil || r.client == nil || r.key == "" {
		return nil
	}
	_, err := releaseLeaderScript.Run(ctx, r.client, []string{r.key}, r.instanceID).Result()
	if err != nil {
		return errors.Wrap(err, "释放 leader 锁失败")
	}
	return nil
}

// IsLeader 快速校验当前实例是否仍持有 leader 租约。
// 周期任务真正入队前会调用该方法做 double-check，避免 GC 停顿或 Redis 续约延迟后继续投递旧 leader 任务。
func (r *LeaderRunner) IsLeader(ctx context.Context) (bool, error) {
	if r == nil || r.client == nil || r.key == "" || r.instanceID == "" {
		return false, nil
	}
	value, err := r.client.Get(ctx, r.key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, errors.Wrap(err, "校验 leader 租约失败")
	}
	return value == r.instanceID, nil
}
