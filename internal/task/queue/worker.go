package taskqueue

import (
	"context"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// Ready 检查任务 Redis 以及当前部署模式要求的 Worker、Scheduler 运行态。
func (m *Manager) Ready(ctx context.Context, requireWorker bool, requireScheduler bool) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.redis.Ping(ctx).Err(); err != nil {
		return errors.Wrap(err, "任务 Redis PING失败")
	}
	if requireWorker {
		if err := m.workerReady(time.Now()); err != nil {
			return errors.Tag(err)
		}
	}
	if requireScheduler && m.schedulerEnabled() && m.periodicTaskCount() > 0 {
		m.lifecycleMu.Lock()
		leader := m.leader
		m.lifecycleMu.Unlock()
		if err := leader.Ready(time.Now()); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// workerReady 校验 Worker 已启动、内部健康检查无错误且心跳未停滞。
func (m *Manager) workerReady(now time.Time) error {
	if m == nil {
		return errors.New("任务 Worker 未初始化")
	}
	m.workerHealthMu.RLock()
	health := m.workerHealth
	m.workerHealthMu.RUnlock()
	if !health.running {
		return errors.New("任务 Worker 未运行")
	}
	if health.lastError != "" {
		return errors.Errorf("任务 Worker 心跳失败: %s", health.lastError)
	}
	if health.lastHeartbeat.IsZero() || now.Sub(health.lastHeartbeat) > workerHeartbeatMaxAge {
		return errors.Errorf("任务 Worker 心跳已超时: max_age=%s", workerHeartbeatMaxAge)
	}
	return nil
}

// markWorkerStarted 标记 Worker 已成功启动。
func (m *Manager) markWorkerStarted() {
	m.workerHealthMu.Lock()
	m.workerHealth = workerHealth{running: true, lastHeartbeat: time.Now()}
	m.workerHealthMu.Unlock()
}

// recordWorkerHeartbeat 记录 Asynq 内部健康检查结果。
func (m *Manager) recordWorkerHeartbeat(err error) {
	m.workerHealthMu.Lock()
	defer m.workerHealthMu.Unlock()
	if !m.workerHealth.running {
		return
	}
	m.workerHealth.lastHeartbeat = time.Now()
	m.workerHealth.lastError = ""
	if err != nil {
		m.workerHealth.lastError = err.Error()
	}
}

// markWorkerStopped 标记 Worker 已进入停止流程。
func (m *Manager) markWorkerStopped() {
	m.workerHealthMu.Lock()
	m.workerHealth.running = false
	m.workerHealthMu.Unlock()
}

// StartWorker 启动任务 Worker。
func (m *Manager) StartWorker() error {
	if !m.IsEnabled() {
		return nil
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.server != nil {
		return nil
	}
	server := asynq.NewServerFromRedisClient(m.redis, asynq.Config{
		Concurrency:              m.concurrency(),
		Queues:                   m.queueWeights(),
		StrictPriority:           m.CurrentConfig().StrictPriority,
		BaseContext:              func() context.Context { return context.Background() },
		ShutdownTimeout:          m.shutdownTimeout(),
		GroupGracePeriod:         m.groupGracePeriod(),
		GroupMaxDelay:            m.groupMaxDelay(),
		GroupMaxSize:             m.groupMaxSize(),
		GroupAggregator:          asynq.GroupAggregatorFunc(m.aggregateTasks),
		Logger:                   m.logger,
		LogLevel:                 asynq.InfoLevel,
		DelayedTaskCheckInterval: m.delayedTaskCheckInterval(),
		TaskCheckInterval:        m.taskCheckInterval(),
		ErrorHandler:             asynq.ErrorHandlerFunc(m.handleTaskError),
		HealthCheckFunc: func(err error) {
			m.recordWorkerHeartbeat(err)
			if err != nil {
				loggerx.Errorw(context.Background(), "任务队列 健康检查失败", err, logx.Field("app_id", m.appNamespace()))
			}
		},
		HealthCheckInterval: workerHealthInterval,
	})
	if err := server.Start(m.mux); err != nil {
		return errors.Tag(err)
	}
	m.server = server
	m.markWorkerStarted()
	m.startArchivedCleanerLocked()
	loggerx.Infow(context.Background(), "任务队列 工作进程已启动",
		logx.Field("app_id", m.appNamespace()),
		logx.Field("concurrency", m.concurrency()),
		logx.Field("queues", m.queueWeights()),
		logx.Field("strict", m.CurrentConfig().StrictPriority),
	)
	return nil
}

// StartScheduler 启动周期任务调度器 leader 选举。
func (m *Manager) StartScheduler() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	// 启动前置条件：任务系统启用 + scheduler 开关开启 + 至少存在一个周期任务。
	if !m.IsEnabled() {
		m.markSchedulerStopped(i18n.MsgKeySchedulerTaskDisabled, "任务系统未启用，调度器未启动")
		return nil
	}
	if !m.schedulerEnabled() {
		m.markSchedulerStopped(i18n.MsgKeySchedulerDisabled, "调度器开关未开启")
		return nil
	}
	if m.periodicTaskCount() == 0 {
		m.markSchedulerStopped(i18n.MsgKeySchedulerNoPeriodicTask, "未配置有效周期任务")
		return nil
	}
	// 避免重复启动：同一进程只允许存在一个 scheduler 选举循环。
	if m.cancel != nil {
		m.markSchedulerWaitingLeader(i18n.MsgKeySchedulerAlreadyRunning, "调度器已在运行，等待 leader 状态变化")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.markSchedulerWaitingLeader(i18n.MsgKeySchedulerElectionStarted, "调度器 leader 选举已启动")
	// 构造 leader 选举器：仅 leader 实例负责 asynq PeriodicTaskManager 调度。
	m.leader = NewLeaderRunner(m.redis, m.schedulerLeaseKey(), m.schedulerLeaseTTL(), m.schedulerRenewInterval(), func(leaderCtx context.Context) (func(), error) {
		// leader 回调内启动调度器，并返回 shutdown hook 供 leader 丢失/Stop 时回收。
		scheduler := newPeriodicTaskScheduler(m)
		if err := scheduler.Start(leaderCtx); err != nil {
			m.markSchedulerSyncFailure(err.Error())
			return nil, errors.Tag(err)
		}
		m.markSchedulerLeaderAcquired()
		loggerx.Infow(leaderCtx, "任务调度 主节点已启动",
			logx.Field("instance", m.instance),
		)
		return func() {
			scheduler.Shutdown()
			m.markSchedulerLeaderReleased(i18n.MsgKeySchedulerLeaderReleased, "调度器已释放 leader，等待重新竞争")
			loggerx.Infow(leaderCtx, "任务调度 主节点已停止",
				logx.Field("instance", m.instance),
			)
		}, nil
	})
	leader := m.leader
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// 后台运行 leader 循环，Stop() 时通过 cancel 触发退出。
		leader.Start(ctx)
	}()
	leader.waitStarted()
	return nil
}

// Stop 停止 Worker 与调度器后台协程，并受应用统一停止期限约束。
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.lifecycleMu.Lock()
	cancel := m.cancel
	m.cancel = nil
	server := m.server
	m.server = nil
	m.markWorkerStopped()
	archivedCleanStop := m.archivedCleanStop
	m.archivedCleanStop = nil
	m.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if archivedCleanStop != nil {
		archivedCleanStop()
	}
	if server != nil {
		shutdownDone := make(chan struct{})
		go func() {
			server.Shutdown()
			close(shutdownDone)
		}()
		select {
		case <-shutdownDone:
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "停止任务 Worker 超时")
		}
	}
	waitDone := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "等待任务后台协程退出超时")
	}
	m.markSchedulerStopped(i18n.MsgKeySchedulerStopped, "调度器已停止")
	return nil
}
