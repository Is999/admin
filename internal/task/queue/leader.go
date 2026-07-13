package taskqueue

import (
	"context"
	"sync"
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
	healthMu      sync.RWMutex                          // 保护选举循环心跳状态
	health        leaderHealth                          // 选举循环运行和最近心跳状态
	started       chan struct{}                         // 选举循环已实际进入运行态信号
	startOnce     sync.Once                             // 保证启动信号只关闭一次
}

// leaderHealth 描述调度器选举循环的本地运行状态。
type leaderHealth struct {
	running       bool      // 选举循环是否仍在运行
	lastHeartbeat time.Time // 最近一次完成 Redis 选举或续租检查的时间
	lastError     string    // 最近一次选举或续租错误
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
		started:       make(chan struct{}),
	}
}

// Start 启动 leader 选举循环，直到 ctx 结束。
func (r *LeaderRunner) Start(ctx context.Context) {
	if r == nil || r.client == nil || r.onAcquire == nil || r.key == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.markRunning()
	defer r.markStopped()

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
				r.markHeartbeat(err)
				loggerx.Errorw(ctx, "任务队列 leader 竞争失败", errors.Wrap(err, "竞争 scheduler leader 锁"),
					logx.Field("key", r.key),
					logx.Field("instance", r.instanceID),
				)
			} else if ok {
				release, err = r.onAcquire(ctx)
				if err != nil {
					r.markHeartbeat(err)
					loggerx.Errorw(ctx, "任务队列 leader 启动调度器失败", errors.Wrap(err, "启动 scheduler leader 回调"),
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
					_ = r.release(ctx)
				} else {
					isLeader = true
					r.markHeartbeat(nil)
					loggerx.Infow(ctx, "任务队列 leader 已获取",
						logx.Field("key", r.key),
						logx.Field("instance", r.instanceID),
					)
				}
			} else {
				r.markHeartbeat(nil)
			}
		}
		r.signalStarted()

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
			r.markHeartbeat(err)
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

// signalStarted 在首次 Redis 选举检查完成后确认循环已实际启动。
func (r *LeaderRunner) signalStarted() {
	r.startOnce.Do(func() {
		close(r.started)
	})
}

// Ready 校验选举循环真实运行且心跳未停滞。
func (r *LeaderRunner) Ready(now time.Time) error {
	if r == nil {
		return errors.New("任务 Scheduler 选举循环未初始化")
	}
	r.healthMu.RLock()
	health := r.health
	r.healthMu.RUnlock()
	if !health.running {
		return errors.New("任务 Scheduler 选举循环未运行")
	}
	if health.lastError != "" {
		return errors.Errorf("任务 Scheduler 选举心跳失败: %s", health.lastError)
	}
	maxAge := 3 * r.renewInterval
	if maxAge <= 0 {
		maxAge = time.Minute
	}
	if health.lastHeartbeat.IsZero() || now.Sub(health.lastHeartbeat) > maxAge {
		return errors.Errorf("任务 Scheduler 选举心跳已超时: max_age=%s", maxAge)
	}
	return nil
}

// waitStarted 等待选举 goroutine 实际进入运行态。
func (r *LeaderRunner) waitStarted() {
	if r == nil || r.started == nil {
		return
	}
	<-r.started
}

// markRunning 标记选举循环已启动并写入首次心跳。
func (r *LeaderRunner) markRunning() {
	r.healthMu.Lock()
	r.health = leaderHealth{running: true, lastHeartbeat: time.Now()}
	r.healthMu.Unlock()
}

// markHeartbeat 记录一次选举或续租检查结果。
func (r *LeaderRunner) markHeartbeat(err error) {
	r.healthMu.Lock()
	r.health.lastHeartbeat = time.Now()
	r.health.lastError = ""
	if err != nil {
		r.health.lastError = err.Error()
	}
	r.healthMu.Unlock()
}

// markStopped 标记选举循环已经退出。
func (r *LeaderRunner) markStopped() {
	r.healthMu.Lock()
	r.health.running = false
	r.healthMu.Unlock()
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
