package taskqueue

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/logx"
)

// periodicCronParser 统一解析周期任务表达式，支持 5 段 cron、6 段秒级 cron 和 @every 描述符。
var periodicCronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

const (
	// minPeriodicEverySeconds 表示固定间隔周期任务允许的最小秒数，避免误配置 1 秒级任务打爆队列。
	minPeriodicEverySeconds = 5
	// periodicEnqueueGuardTimeout 表示入队前 leader 与队列背压快速检查的超时时间。
	periodicEnqueueGuardTimeout = 500 * time.Millisecond
)

// periodicTaskScheduler 负责在 scheduler leader 内运行本进程的周期任务调度器。
type periodicTaskScheduler struct {
	manager  *Manager                // manager 表示任务队列管理器，提供配置、客户端和命名空间能力
	cron     *cron.Cron              // cron 执行器，支持秒级表达式
	cancel   context.CancelFunc      // cancel 用于停止配置同步 goroutine
	wg       sync.WaitGroup          // wg 等待后台同步 goroutine 退出
	mu       sync.Mutex              // mu 保护 entries 并发读写
	entries  map[string]cron.EntryID // entries 记录配置摘要到 cron entry 的映射
	shutdown bool                    // shutdown 标识调度器是否已进入停止流程
}

// periodicTaskJob 读取 cron 记录的本轮计划时刻并投递周期任务。
type periodicTaskJob struct {
	scheduler        *periodicTaskScheduler    // scheduler 执行 leader 校验、背压检查和入队
	config           *asynq.PeriodicTaskConfig // config 是本调度项的任务和入队选项
	uniqueBySchedule bool                      // uniqueBySchedule 表示入口去重是否包含本轮计划时刻
	entryID          atomic.Int64              // entryID 关联 cron 内部调度记录
}

// newPeriodicTaskJob 创建周期调度任务。
func newPeriodicTaskJob(scheduler *periodicTaskScheduler, config *asynq.PeriodicTaskConfig) *periodicTaskJob {
	return &periodicTaskJob{
		scheduler:        scheduler,
		config:           config,
		uniqueBySchedule: periodicTaskUsesScheduledUnique(scheduler, config),
	}
}

// periodicTaskUsesScheduledUnique 判断周期入口是否需要按计划时刻隔离 Asynq 去重摘要。
func periodicTaskUsesScheduledUnique(scheduler *periodicTaskScheduler, config *asynq.PeriodicTaskConfig) bool {
	if scheduler == nil || scheduler.manager == nil || config == nil || config.Task == nil || config.Task.Type() != TypeWorkflowTrigger {
		return false
	}
	var payload WorkflowTriggerPayload
	if json.Unmarshal(config.Task.Payload(), &payload) != nil {
		return false
	}
	definition, err := scheduler.manager.workflowDefinition(payload.WorkflowName)
	return err == nil && definition.PeriodicUniqueBySchedule
}

// Run 按 cron 本轮登记的计划时刻投递任务。
func (j *periodicTaskJob) Run() {
	if j == nil || j.scheduler == nil || j.scheduler.cron == nil || j.config == nil {
		return
	}
	now := time.Now()
	entry := j.scheduler.cron.Entry(cron.EntryID(j.entryID.Load()))
	scheduledAt := periodicScheduledAt(entry.Prev, now)
	j.scheduler.enqueuePeriodicTask(j.config, scheduledAt, j.uniqueBySchedule)
}

// periodicScheduledAt 优先使用 cron 已登记时刻，记录缺失时回退当前秒。
func periodicScheduledAt(previous, now time.Time) time.Time {
	if previous.IsZero() || previous.After(now) {
		return now.Truncate(time.Second)
	}
	return previous
}

// newPeriodicTaskScheduler 创建支持秒级 cron 的周期任务调度器。
func newPeriodicTaskScheduler(manager *Manager) *periodicTaskScheduler {
	return &periodicTaskScheduler{
		manager: manager,
		cron: cron.New(
			cron.WithParser(periodicCronParser),
			cron.WithLocation(time.Local),
		),
		entries: make(map[string]cron.EntryID),
	}
}

// Start 启动周期任务调度器，并按同步间隔热更新配置。
func (s *periodicTaskScheduler) Start(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	if err := s.syncConfigs(); err != nil {
		s.manager.markSchedulerSyncFailure(err.Error())
		s.manager.notifyTaskRuntimeAlert(ctx, TaskRuntimeAlert{
			Kind:      taskRuntimeAlertKindPeriodicSyncFailed,
			Title:     "【P1 周期任务同步失败】",
			Status:    "调度器启动前同步失败，Scheduler 本次启动会中止",
			Component: "scheduler",
			Operation: "sync_periodic_configs",
			Reason:    err.Error(),
		})
		return errors.Tag(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.cron.Start()
	s.manager.markSchedulerHeartbeat()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(s.manager.schedulerSyncInterval())
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
				// 配置同步失败只记录日志，不终止 leader，避免一次异常导致调度器整体退出。
				if err := s.syncConfigs(); err != nil {
					s.manager.markSchedulerSyncFailure(err.Error())
					loggerx.Errorw(context.Background(), "周期任务 同步失败", err)
					s.manager.notifyTaskRuntimeAlert(context.Background(), TaskRuntimeAlert{
						Kind:      taskRuntimeAlertKindPeriodicSyncFailed,
						Title:     "【P1 周期任务同步失败】",
						Status:    "调度器保留上一轮已同步配置，本轮同步失败",
						Component: "scheduler",
						Operation: "sync_periodic_configs",
						Reason:    err.Error(),
					})
				}
				timer.Reset(s.manager.schedulerSyncInterval())
			}
		}
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(s.manager.schedulerHeartbeatInterval())
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
				s.manager.markSchedulerHeartbeat()
				timer.Reset(s.manager.schedulerHeartbeatInterval())
			}
		}
	}()
	return nil
}

// Shutdown 停止周期任务调度器和后台同步 goroutine。
func (s *periodicTaskScheduler) Shutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return
	}
	s.shutdown = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	stopCtx := s.cron.Stop()
	<-stopCtx.Done()
	s.wg.Wait()
}

// syncConfigs 对比当前配置快照，动态增删 cron entry。
func (s *periodicTaskScheduler) syncConfigs() error {
	configs, err := s.manager.periodicConfigs()
	if err != nil {
		return errors.Tag(err)
	}
	next := make(map[string]*asynq.PeriodicTaskConfig, len(configs))
	for _, item := range configs {
		if item == nil || item.Task == nil {
			continue
		}
		hash := periodicTaskConfigHash(item)
		next[hash] = item
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, entryID := range s.entries {
		if _, ok := next[hash]; ok {
			continue
		}
		s.cron.Remove(entryID)
		delete(s.entries, hash)
	}
	for hash, item := range next {
		if _, ok := s.entries[hash]; ok {
			continue
		}
		schedule, err := periodicCronParser.Parse(item.Cronspec)
		if err != nil {
			loggerx.Errorw(context.Background(), "周期任务 调度配置无效", err,
				logx.Field("cron", item.Cronspec),
				logx.Field("task_type", item.Task.Type()),
			)
			s.notifyPeriodicRuntimeAlert(context.Background(), item,
				taskRuntimeAlertKindPeriodicScheduleFailed,
				"【P1 周期任务调度配置异常】",
				"已跳过该周期任务，调度器继续运行",
				"register_periodic_schedule",
				err,
			)
			continue
		}
		job := newPeriodicTaskJob(s, item)
		entryID := s.cron.Schedule(schedule, job)
		job.entryID.Store(int64(entryID))
		s.entries[hash] = entryID
	}
	s.manager.markSchedulerSyncSuccess(len(next))
	return nil
}

// enqueuePeriodicTask 把单次周期触发任务投递给 Asynq。
func (s *periodicTaskScheduler) enqueuePeriodicTask(cfg *asynq.PeriodicTaskConfig, scheduledAt time.Time, uniqueBySchedule bool) {
	if s == nil || s.manager == nil || s.manager.client == nil || cfg == nil || cfg.Task == nil {
		return
	}
	taskName := strings.TrimSpace(cfg.Task.Headers()[headerTaskName])
	guardCtx, cancel := context.WithTimeout(context.Background(), periodicEnqueueGuardTimeout)
	defer cancel()
	if ok, err := s.manager.schedulerLeaderStillHeld(guardCtx); err != nil {
		s.manager.markSchedulerEnqueueFailure(taskName, cfg.Task.Type(), "", err.Error())
		loggerx.Errorw(context.Background(), "周期任务 leader 校验失败", err,
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
		)
		s.notifyPeriodicRuntimeAlert(context.Background(), cfg,
			taskRuntimeAlertKindPeriodicLeaderFailed,
			"【P1 周期任务 Leader 校验失败】",
			"已跳过本轮周期投递，调度器继续运行",
			"check_scheduler_leader",
			err,
		)
		return
	} else if !ok {
		loggerx.Infow(context.Background(), "周期任务 跳过非 leader 投递",
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
		)
		return
	}
	if ok, queueName, backlog, limit, err := s.manager.periodicQueueBackpressureOK(guardCtx, cfg); err != nil {
		s.manager.markSchedulerEnqueueFailure(taskName, cfg.Task.Type(), "", err.Error())
		loggerx.Errorw(context.Background(), "周期任务 队列背压检查失败", err,
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
			logx.Field("queue", queueName),
		)
		s.notifyPeriodicRuntimeAlert(context.Background(), cfg,
			taskRuntimeAlertKindPeriodicQueueFailed,
			"【P1 周期任务队列检查失败】",
			"已跳过本轮周期投递，调度器继续运行",
			"check_periodic_queue_backpressure",
			err,
		)
		return
	} else if !ok {
		s.manager.markSchedulerEnqueueFailure(taskName, cfg.Task.Type(), i18n.MsgKeySchedulerBacklogExceeded, "queue backlog exceeded")
		loggerx.Infow(context.Background(), "周期任务 队列积压过高跳过本轮",
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
			logx.Field("queue", queueName),
			logx.Field("backlog", backlog),
			logx.Field("limit", limit),
		)
		return
	}
	enqueueCtx, enqueueCancel := context.WithTimeout(context.Background(), periodicEnqueueGuardTimeout)
	defer enqueueCancel()
	task, err := periodicTaskWithScheduledAt(cfg.Task, scheduledAt, uniqueBySchedule)
	if err != nil {
		s.manager.markSchedulerEnqueueFailure(taskName, cfg.Task.Type(), "", err.Error())
		loggerx.Errorw(context.Background(), "周期任务 构造入口失败", err,
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
		)
		s.notifyPeriodicRuntimeAlert(context.Background(), cfg,
			taskRuntimeAlertKindPeriodicEnqueueFailed,
			"【P1 周期任务入队失败】",
			"本轮周期任务未成功构造，调度器继续运行",
			"build_periodic_task",
			err,
		)
		return
	}
	if _, err := s.manager.client.EnqueueContext(enqueueCtx, task, cfg.Opts...); err != nil {
		s.manager.markSchedulerEnqueueFailure(taskName, cfg.Task.Type(), "", err.Error())
		loggerx.Errorw(context.Background(), "周期任务 入队失败", err,
			logx.Field("cron", cfg.Cronspec),
			logx.Field("task_type", cfg.Task.Type()),
			logx.Field("task_name", taskName),
		)
		if !stderrors.Is(err, asynq.ErrDuplicateTask) {
			s.notifyPeriodicRuntimeAlert(context.Background(), cfg,
				taskRuntimeAlertKindPeriodicEnqueueFailed,
				"【P1 周期任务入队失败】",
				"本轮周期任务未成功投递到队列，调度器继续运行",
				"enqueue_periodic_task",
				err,
			)
		}
		return
	}
	s.manager.markSchedulerEnqueueSuccess(taskName, cfg.Task.Type())
}

// periodicTaskWithScheduledAt 复制周期任务并写入本轮触发时刻，避免修改调度器复用的静态任务。
// 需要按计划时刻隔离去重时同步写入 payload，因为 Asynq Unique 不参与 header 摘要。
func periodicTaskWithScheduledAt(task *asynq.Task, scheduledAt time.Time, uniqueBySchedule bool) (*asynq.Task, error) {
	if task == nil {
		return nil, errors.Errorf("周期任务不能为空")
	}
	headers := make(map[string]string, len(task.Headers())+1)
	for key, value := range task.Headers() {
		headers[key] = value
	}
	scheduledText := ""
	if !scheduledAt.IsZero() {
		scheduledText = scheduledAt.Format(time.RFC3339)
		headers[HeaderScheduledAt] = scheduledText
	}
	payloadBytes := task.Payload()
	if uniqueBySchedule {
		if task.Type() != TypeWorkflowTrigger || scheduledText == "" {
			return nil, errors.Errorf("按计划时刻隔离去重的周期任务缺少工作流入口或计划时间")
		}
		var payload WorkflowTriggerPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, errors.Wrap(err, "解析按计划时刻隔离的周期任务载荷失败")
		}
		payload.ScheduledAt = scheduledText
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, errors.Wrap(err, "序列化按计划时刻隔离的周期任务载荷失败")
		}
	}
	return asynq.NewTaskWithHeaders(task.Type(), payloadBytes, headers), nil
}

// notifyPeriodicRuntimeAlert 从周期任务配置中提取字段并触发统一运行异常告警。
func (s *periodicTaskScheduler) notifyPeriodicRuntimeAlert(ctx context.Context, cfg *asynq.PeriodicTaskConfig, kind, title, status, operation string, runErr error) {
	if s == nil || s.manager == nil || cfg == nil || cfg.Task == nil || runErr == nil {
		return
	}
	alert := periodicRuntimeAlertFromConfig(s.manager, cfg)
	alert.Kind = kind
	alert.Title = title
	alert.Status = status
	alert.Component = "scheduler"
	alert.Operation = operation
	alert.Reason = runErr.Error()
	s.manager.notifyTaskRuntimeAlert(ctx, alert)
}

// periodicRuntimeAlertFromConfig 提取周期任务告警字段，避免 Lark 文本只剩 task_type。
func periodicRuntimeAlertFromConfig(manager *Manager, cfg *asynq.PeriodicTaskConfig) TaskRuntimeAlert {
	alert := TaskRuntimeAlert{}
	if cfg == nil || cfg.Task == nil {
		return alert
	}
	alert.Cron = strings.TrimSpace(cfg.Cronspec)
	alert.TaskType = strings.TrimSpace(cfg.Task.Type())
	alert.TaskName = strings.TrimSpace(cfg.Task.Headers()[headerTaskName])
	var payload WorkflowTriggerPayload
	if len(cfg.Task.Payload()) > 0 && json.Unmarshal(cfg.Task.Payload(), &payload) == nil {
		alert.WorkflowName = strings.TrimSpace(payload.WorkflowName)
		alert.TaskQueue = strings.TrimSpace(payload.Queue)
		alert.UniqueKey = strings.TrimSpace(payload.UniqueKey)
	}
	if manager != nil && alert.TaskQueue == "" {
		alert.TaskQueue = manager.defaultWorkflowQueue()
	}
	return alert
}

// schedulerLeaderStillHeld 在入队前确认当前调度器仍持有 leader 租约。
func (m *Manager) schedulerLeaderStillHeld(ctx context.Context) (bool, error) {
	if m == nil || m.leader == nil {
		return false, nil
	}
	return m.leader.IsLeader(ctx)
}

// periodicQueueBackpressureOK 检查周期任务目标队列是否低于配置积压上限。
// 当 max_queue_backlog<=0 时不启用背压，按 cron 配置持续投递。
func (m *Manager) periodicQueueBackpressureOK(ctx context.Context, cfg *asynq.PeriodicTaskConfig) (bool, string, int64, int64, error) {
	if m == nil {
		return false, "", 0, 0, errors.Errorf("任务队列管理器未初始化")
	}
	limit := m.schedulerMaxQueueBacklog()
	queueName := m.periodicTaskQueue(cfg)
	if limit <= 0 {
		return true, queueName, 0, 0, nil
	}
	if m.inspector == nil {
		return false, queueName, 0, limit, errors.Errorf("任务队列巡检器未初始化")
	}
	info, err := m.inspector.GetQueueInfo(queueName)
	if err != nil {
		return false, queueName, 0, limit, errors.Wrap(err, "查询周期任务队列积压失败")
	}
	backlog := queueInfoBacklog(info)
	return backlog <= limit, queueName, backlog, limit, nil
}

// periodicTaskQueue 从周期任务 payload 解析目标工作流队列，并转换为 Asynq 内部队列名。
func (m *Manager) periodicTaskQueue(cfg *asynq.PeriodicTaskConfig) string {
	queue := ""
	if cfg != nil && cfg.Task != nil && len(cfg.Task.Payload()) > 0 {
		var payload WorkflowTriggerPayload
		if err := json.Unmarshal(cfg.Task.Payload(), &payload); err == nil {
			queue = strings.TrimSpace(payload.Queue)
		}
	}
	return m.namespacedQueueName(helper.FirstNonEmptyString(queue, m.defaultWorkflowQueue()))
}

// queueInfoBacklog 计算下游实际积压深度，只统计等待、延迟、重试和聚合中的任务。
func queueInfoBacklog(info *asynq.QueueInfo) int64 {
	if info == nil {
		return 0
	}
	return int64(info.Pending + info.Scheduled + info.Retry + info.Aggregating)
}

// periodicTaskConfigHash 生成调度配置摘要，用于判断是否需要增删 cron entry。
func periodicTaskConfigHash(cfg *asynq.PeriodicTaskConfig) string {
	h := sha256.New()
	_, _ = h.Write([]byte(cfg.Cronspec))
	if cfg.Task != nil {
		_, _ = h.Write([]byte(cfg.Task.Type()))
		_, _ = h.Write(cfg.Task.Payload())
	}
	opts := make([]string, 0, len(cfg.Opts))
	for _, opt := range cfg.Opts {
		if opt == nil {
			continue
		}
		opts = append(opts, opt.String())
	}
	sort.Strings(opts)
	for _, opt := range opts {
		_, _ = h.Write([]byte(opt))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// periodicTaskCronspec 返回周期任务实际交给 cron 执行器的表达式。
func periodicTaskCronspec(item config.TaskPeriodicConfig) (string, error) {
	cronExpr := strings.TrimSpace(item.Cron)
	if item.EverySeconds < 0 {
		return "", errors.Errorf("周期任务 every_seconds 不能小于 0")
	}
	if cronExpr != "" && item.EverySeconds > 0 {
		return "", errors.Errorf("周期任务 cron 和 every_seconds 不能同时配置")
	}
	if item.EverySeconds > 0 {
		if item.EverySeconds < minPeriodicEverySeconds {
			return "", errors.Errorf("周期任务 every_seconds 不能小于 %d", minPeriodicEverySeconds)
		}
		return fmt.Sprintf("@every %ds", item.EverySeconds), nil
	}
	if cronExpr == "" {
		return "", errors.Errorf("周期任务 cron 或 every_seconds 必须配置一个")
	}
	return cronExpr, nil
}

// validatePeriodicCronspec 校验周期表达式是否能被秒级调度器解析。
func validatePeriodicCronspec(cronspec string) error {
	if _, err := periodicCronParser.Parse(strings.TrimSpace(cronspec)); err != nil {
		return errors.Wrap(err, "解析周期任务 cron 失败")
	}
	return nil
}
