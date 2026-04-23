package task

import (
	"context"
	"strings"
	"time"

	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/config"
	"admin_cron/internal/infra/kafkax"
	redislock "admin_cron/internal/infra/redsync"
	usertagoptions "admin_cron/internal/jobs/usertag/options"
	"admin_cron/internal/jobs/usertag/repository"
	"admin_cron/internal/jobs/usertag/route"
	usertagtypes "admin_cron/internal/jobs/usertag/types"
	usertagworkflow "admin_cron/internal/jobs/usertag/workflow"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/taskqueue"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// PluginName 是用户标签任务插件名称，供 taskruntime 包装注册时保持唯一。
	PluginName = "user_tag"
)

const (
	// userTagRuntimeCleanupLockTTL 是运行期辅助表清理互斥锁 TTL，任务存活时由 redsync 自动续期。
	userTagRuntimeCleanupLockTTL = 10 * time.Minute
	// userTagKafkaOutboxRetryScanLockTTL 是异常 outbox 扫描互斥锁 TTL，覆盖单轮批量领取和推送窗口。
	userTagKafkaOutboxRetryScanLockTTL = 5 * time.Minute
)

// Runtime 描述用户标签任务注册所需的最小运行时能力。
// 该接口让 usertag/task 包承载任务细节，同时避免反向依赖 taskruntime 造成包循环。
type Runtime interface {
	// ServiceContext 返回任务处理器共享的服务上下文。
	ServiceContext() *svc.ServiceContext
	// RegisterHandler 注册 Asynq 任务处理器。
	RegisterHandler(pattern string, handler asynq.Handler) error
	// RegisterFinalFailureHook 注册任务终态失败后的业务清理钩子。
	RegisterFinalFailureHook(name string, hook taskqueue.TaskFinalFailureHook) error
	// RegisterWorkflow 注册用户标签工作流定义。
	RegisterWorkflow(def *taskqueue.WorkflowDefinition) error
}

// Setup 注册用户标签全部任务 handler 和工作流定义。
// 后续新增 usertag 定时工作流、独立维护任务或节点处理器，统一在 internal/jobs/usertag/task 下扩展。
func Setup(runtime Runtime) error {
	// 用户标签能力未开启时直接跳过注册，避免把空配置节点挂入任务系统。
	if runtime == nil || runtime.ServiceContext() == nil || !runtime.ServiceContext().CurrentConfig().Workflows.UserTag.Enabled {
		return nil
	}
	// 复用同一个 ServiceContext 给整组节点处理器，保持仓储、配置和依赖注入一致。
	svcCtx := runtime.ServiceContext()
	if err := registerUserTagWorkflowLeaseFinalFailureHook(runtime, svcCtx); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:prepare 任务类型：准备用户标签运行环境，并在全量模式下创建临时结果表。
	if err := runtime.RegisterHandler(TaskTypeUserTagPrepare, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagTask(ctx, task, svcCtx, usertagtypes.NodePrepare)
	})); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:business_hook 任务类型：保留扩展入口，框架默认不绑定具体实现。
	if err := runtime.RegisterHandler(TaskTypeUserTagBusinessHook, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagTask(ctx, task, svcCtx, usertagtypes.NodeBusinessHook)
	})); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:finalize 任务类型：在全量链路成功后原子切换用户标签结果表，并完成必要的状态收尾。
	if err := runtime.RegisterHandler(TaskTypeUserTagFinalize, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagTask(ctx, task, svcCtx, usertagtypes.NodeFinalize)
	})); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:sync_kafka 任务类型：负责 full 切表后的同步快照重建，名称保留兼容历史 DAG。
	if err := runtime.RegisterHandler(TaskTypeUserTagSyncKafka, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagTask(ctx, task, svcCtx, usertagtypes.NodeSyncKafka)
	})); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:kafka_outbox_retry 任务类型：独立重试 outbox 推送 Kafka，不参与用户标签 workflow 成败判定。
	if err := runtime.RegisterHandler(usertagtypes.TaskTypeUserTagKafkaOutboxRetry, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagKafkaOutboxRetryTask(ctx, task, svcCtx)
	})); err != nil {
		return errors.Tag(err)
	}
	// 注册 user_tag:runtime_cleanup 任务类型：独立清理运行期辅助表。
	if err := runtime.RegisterHandler(usertagtypes.TaskTypeUserTagRuntimeCleanup, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return runUserTagRuntimeCleanupTask(ctx, svcCtx)
	})); err != nil {
		return errors.Tag(err)
	}
	// 最后统一注册 full / delta / targeted / recalculate 四类 DAG 定义。
	defaults := NewDefaults(runtime.ServiceContext().CurrentConfig().Workflows.UserTag)
	for _, def := range WorkflowDefinitions(defaults) {
		if err := runtime.RegisterWorkflow(def); err != nil {
			return errors.Tag(err)
		}
	}
	// 注册运行期清理工作流，供周期调度触发容量治理；该工作流只有独立清理节点，不进入用户标签计算 DAG。
	if err := runtime.RegisterWorkflow(RuntimeCleanupWorkflowDefinition(defaults)); err != nil {
		return errors.Tag(err)
	}
	// 注册 outbox 异常扫描工作流，供周期任务定点补推 pending/retry/running 超时数据。
	if err := runtime.RegisterWorkflow(KafkaOutboxRetryScanWorkflowDefinition(defaults)); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// registerUserTagWorkflowLeaseFinalFailureHook 注册 full 工作流终态失败后的租约释放钩子。
// 该钩子只在 Asynq 确认节点任务不会继续自动重试后触发，避免重试队列中的 full 被提前释放锁。
func registerUserTagWorkflowLeaseFinalFailureHook(runtime Runtime, svcCtx *svc.ServiceContext) error {
	if runtime == nil {
		return nil
	}
	return runtime.RegisterFinalFailureHook("user_tag.workflow_lease.final_failure_release", func(ctx context.Context, task *asynq.Task, meta taskqueue.WorkflowTaskMeta, runErr error) error {
		return releaseUserTagWorkflowLeaseOnFinalFailure(ctx, svcCtx, task, meta)
	})
}

// releaseUserTagWorkflowLeaseOnFinalFailure 在 full 工作流节点终态失败后释放全局写租约。
// 非 full 工作流不持有该租约；解析失败的任务也不会释放，避免误删其他 workflow 的 owner。
func releaseUserTagWorkflowLeaseOnFinalFailure(ctx context.Context, svcCtx *svc.ServiceContext, task *asynq.Task, meta taskqueue.WorkflowTaskMeta) error {
	if task == nil || !strings.HasPrefix(task.Type(), "user_tag:") {
		return nil
	}
	payload, err := DecodePayload(task)
	if err != nil {
		return nil
	}
	workflowID := strings.TrimSpace(payload.WorkflowID)
	if workflowID == "" {
		workflowID = strings.TrimSpace(meta.WorkflowID)
	}
	if workflowID == "" {
		return nil
	}
	mode := strings.TrimSpace(payload.Mode)
	if mode == "" && strings.TrimSpace(meta.WorkflowName) == WorkflowNameUserTagFull {
		mode = usertagtypes.ModeFull
	}
	if mode != usertagtypes.ModeFull {
		return nil
	}
	if workflowName := strings.TrimSpace(meta.WorkflowName); workflowName != "" && workflowName != WorkflowNameUserTagFull {
		return nil
	}
	defaults := NewDefaults(config.UserTagConfig{})
	if svcCtx != nil {
		defaults = NewDefaults(svcCtx.CurrentConfig().Workflows.UserTag)
	}
	deps := repository.NewRuntimeDeps(svcCtx, route.NewShardPlan(defaults.ShardTotal, defaults.RuntimeShardTotal))
	return repository.NewTagRepository(deps).ReleaseWorkflowLease(ctx, usertagtypes.RuntimeOptions{
		WorkflowID: workflowID,
		Mode:       usertagtypes.ModeFull,
	})
}

// runUserTagTask 统一完成任务负载解析和 usertag 节点分发，避免每个节点 handler 重复样板代码。
func runUserTagTask(ctx context.Context, task *asynq.Task, svcCtx *svc.ServiceContext, node string) error {
	// 先把 Asynq 任务载荷还原成工作流节点统一使用的业务参数。
	payload, err := DecodePayload(task)
	if err != nil {
		return errors.Tag(err)
	}
	return runUserTagStage(ctx, svcCtx, node, payload)
}

// runUserTagStage 设置当前节点名称并执行 usertag 工作流阶段。
func runUserTagStage(ctx context.Context, svcCtx *svc.ServiceContext, node string, payload WorkflowPayload) error {
	payload.Node = node
	_, err := usertagworkflow.NewService(ctx, svcCtx).RunStage(payload)
	return errors.Tag(err)
}

// runUserTagKafkaOutboxRetryTask 独立重试用户标签 outbox。
// 该任务没有 workflow 头信息，失败只进入任务系统 retry/dead，不会回写用户标签工作流状态。
func runUserTagKafkaOutboxRetryTask(ctx context.Context, task *asynq.Task, svcCtx *svc.ServiceContext) error {
	payload, err := DecodePayload(task)
	if err != nil {
		return errors.Tag(err)
	}
	if payload.Node == usertagtypes.NodeKafkaOutboxRetryScan {
		return runUserTagKafkaOutboxRetryScanTask(ctx, svcCtx)
	}
	defaults := NewDefaults(config.UserTagConfig{})
	if svcCtx != nil {
		defaults = NewDefaults(svcCtx.CurrentConfig().Workflows.UserTag)
	}
	opts, err := userTagOutboxRetryOptions(payload, defaults)
	if err != nil {
		return errors.Tag(err)
	}
	deps := repository.NewRuntimeDeps(svcCtx, route.NewShardPlan(defaults.ShardTotal, defaults.RuntimeShardTotal))
	repo := repository.NewTagRepository(deps)
	if err = rebuildUserTagKafkaOutbox(ctx, repo, opts, payload.UIDs); err != nil {
		return errors.Tag(err)
	}
	_, err = repo.DrainKafkaOutboxShard(ctx, opts, pushUserTagOutboxRetryMessages(ctx, svcCtx))
	return errors.Tag(err)
}

// runUserTagKafkaOutboxRetryScanTask 周期扫描并重推异常用户标签 outbox。
// 只处理超过观察窗口的 outbox 行，避免抢占主工作流新数据。
func runUserTagKafkaOutboxRetryScanTask(ctx context.Context, svcCtx *svc.ServiceContext) error {
	defaults := NewDefaults(config.UserTagConfig{})
	if svcCtx != nil {
		defaults = NewDefaults(svcCtx.CurrentConfig().Workflows.UserTag)
	}
	if svcCtx == nil {
		return errors.Errorf("用户标签 Kafka outbox 异常扫描失败：ServiceContext 为空")
	}
	opts, err := userTagOutboxRetryScanOptions(defaults)
	if err != nil {
		return errors.Tag(err)
	}
	deps := repository.NewRuntimeDeps(svcCtx, route.NewShardPlan(defaults.ShardTotal, defaults.RuntimeShardTotal))
	lockKey := keys.AppScopedKey(svcCtx.CurrentConfig().AppID, keys.UserTagKafkaOutboxRetryScanLock)
	err = redislock.WithLock(ctx, svcCtx.Rds, lockKey, userTagKafkaOutboxRetryScanLockTTL, func(lockCtx context.Context) error {
		_, runErr := repository.NewTagRepository(deps).RetryKafkaOutboxAbnormalRows(lockCtx, opts, pushUserTagOutboxRetryMessages(lockCtx, svcCtx))
		return errors.Tag(runErr)
	})
	return errors.Tag(err)
}

// runUserTagRuntimeCleanupTask 独立清理用户标签运行期辅助表。
// 该任务不读取 workflow 负载，失败只进入任务系统 retry/dead，不会回写或影响任何用户标签计算工作流。
func runUserTagRuntimeCleanupTask(ctx context.Context, svcCtx *svc.ServiceContext) error {
	defaults := NewDefaults(config.UserTagConfig{})
	if svcCtx != nil {
		defaults = NewDefaults(svcCtx.CurrentConfig().Workflows.UserTag)
	}
	if svcCtx == nil {
		return errors.Errorf("用户标签运行期清理失败：ServiceContext 为空")
	}
	deps := repository.NewRuntimeDeps(svcCtx, route.NewShardPlan(defaults.ShardTotal, defaults.RuntimeShardTotal))
	lockKey := keys.AppScopedKey(svcCtx.CurrentConfig().AppID, keys.UserTagRuntimeCleanupLock)
	err := redislock.WithLock(ctx, svcCtx.Rds, lockKey, userTagRuntimeCleanupLockTTL, func(lockCtx context.Context) error {
		return repository.NewTagRepository(deps).CleanupStaleRuntimeTables(lockCtx, time.Now())
	})
	return errors.Tag(err)
}

// rebuildUserTagKafkaOutbox 在独立重试任务中补构造 outbox。
// 显式 UID 优先；否则按当前 workflow 的 runtime_uid 分批重建。
func rebuildUserTagKafkaOutbox(ctx context.Context, repo *repository.TagRepository, opts usertagtypes.RuntimeOptions, uids []int64) error {
	if len(uids) > 0 {
		_, err := repo.BuildKafkaOutboxForUIDs(ctx, opts, uids)
		return errors.Tag(err)
	}
	if opts.Mode != usertagtypes.ModeDelta && opts.Mode != usertagtypes.ModeRecalculate {
		return nil
	}
	return repo.WalkRuntimeUIDBatches(ctx, opts, func(batch []int64) error {
		_, err := repo.BuildKafkaOutboxForUIDs(ctx, opts, batch)
		return errors.Tag(err)
	})
}

// userTagOutboxRetryOptions 构造 outbox 重试任务运行参数。
// outbox 重试只依赖工作流定位字段和 batchSize。
func userTagOutboxRetryOptions(payload WorkflowPayload, defaults Defaults) (usertagtypes.RuntimeOptions, error) {
	mode := strings.TrimSpace(payload.Mode)
	switch mode {
	case usertagtypes.ModeDelta, usertagtypes.ModeTargeted, usertagtypes.ModeRecalculate, usertagtypes.ModeFull:
	default:
		return usertagtypes.RuntimeOptions{}, errors.Errorf("outbox 重试 mode 非法 mode=%s", mode)
	}
	opts := usertagtypes.RuntimeOptions{
		WorkflowID:           strings.TrimSpace(payload.WorkflowID),
		Mode:                 mode,
		ShardIndex:           payload.ShardIndex,
		ShardTotal:           positiveOr(payload.ShardTotal, defaults.ShardTotal),
		BatchSize:            positiveOr(payload.BatchSize, defaults.KafkaBatchSize),
		WorkerCount:          positiveOr(payload.WorkerCount, defaults.WorkerCount),
		DryRun:               payload.DryRun,
		SyncSnapshotOnly:     payload.SyncSnapshotOnly,
		MarketingSyncEnabled: defaults.MarketingSyncEnabled,
	}
	if err := usertagoptions.ValidateRuntimeOptions(opts); err != nil {
		return opts, errors.Tag(err)
	}
	return opts, nil
}

// userTagOutboxRetryScanOptions 构造周期扫描异常 outbox 的运行参数。
// 扫描任务按全局 state 索引领取异常行，不需要 workflow_id；mode 仅用于复用非 full 的同步快照语义。
func userTagOutboxRetryScanOptions(defaults Defaults) (usertagtypes.RuntimeOptions, error) {
	opts := usertagtypes.RuntimeOptions{
		Mode:                 usertagtypes.ModeDelta,
		ShardIndex:           0,
		ShardTotal:           positiveOr(defaults.ShardTotal, 1),
		BatchSize:            positiveOr(defaults.KafkaBatchSize, defaults.BatchSize),
		WorkerCount:          positiveOr(defaults.WorkerCount, 1),
		MarketingSyncEnabled: defaults.MarketingSyncEnabled,
	}
	if err := usertagoptions.ValidateRuntimeOptions(opts); err != nil {
		return opts, errors.Tag(err)
	}
	return opts, nil
}

// pushUserTagOutboxRetryMessages 返回独立 outbox 重试任务使用的 Kafka 推送函数。
// Kafka 不可用时必须返回错误，让任务系统和 outbox 状态机继续按 retry/dead 处理。
func pushUserTagOutboxRetryMessages(ctx context.Context, svcCtx *svc.ServiceContext) func([]model.UserTagMessage) error {
	return func(messages []model.UserTagMessage) error {
		if len(messages) == 0 {
			return nil
		}
		if svcCtx == nil || svcCtx.Kafka == nil || !svcCtx.Kafka.Enabled() {
			return errors.Errorf("用户标签 outbox 重试失败：Kafka 生产者未初始化或未启用")
		}
		items := make([]kafkax.JSONMessage, 0, len(messages))
		for _, message := range messages {
			items = append(items, kafkax.JSONMessage{
				Key:   message.EventID,
				Value: model.NewUserTagEventEnvelope(message),
			})
		}
		return svcCtx.Kafka.WriteKeyedJSON(ctx, items)
	}
}

// positiveOr 返回正数配置，非正数时使用兜底值。
func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
