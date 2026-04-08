package taskruntime

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/taskqueue"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// NewCorePlugin 创建任务系统内置核心插件。
func NewCorePlugin() Plugin {
	return NewPluginFunc("core", func(runtime *Runtime) error {
		// 注册 workflow:trigger 任务类型：解析管理端、周期调度或内部投递的工作流启动请求，并创建对应 DAG 实例。
		if err := runtime.RegisterHandler(taskqueue.TypeWorkflowTrigger, asynq.HandlerFunc(runtime.handleWorkflowTrigger)); err != nil {
			return errors.Tag(err)
		}
		// 注册 workflow:noop 任务类型：作为 DAG 依赖汇聚和收尾节点，不执行业务写入，仅推进工作流状态。
		if err := runtime.RegisterHandler(taskqueue.TypeWorkflowNoop, asynq.HandlerFunc(runtime.handleWorkflowNoop)); err != nil {
			return errors.Tag(err)
		}
		// 注册 cache:refresh:request 任务类型：承接零散缓存刷新请求，后续由 group 聚合器合并为批量刷新任务。
		if err := runtime.RegisterHandler(taskqueue.TypeCacheRefreshRequest, asynq.HandlerFunc(runtime.handleCacheRefresh)); err != nil {
			return errors.Tag(err)
		}
		// 注册 cache:refresh:batch 任务类型：执行已聚合的缓存刷新目标列表，减少短时间重复刷新同一批缓存键。
		if err := runtime.RegisterHandler(taskqueue.TypeCacheRefreshBatch, asynq.HandlerFunc(runtime.handleCacheRefresh)); err != nil {
			return errors.Tag(err)
		}
		// 再注册批量聚合器，把短时间内重复的缓存刷新请求合并，减少无效任务开销。
		if err := runtime.RegisterGroupAggregator(taskqueue.GroupCacheRefresh, runtime.aggregateCacheRefreshTasks); err != nil {
			return errors.Tag(err)
		}
		// 最后补上公共缓存刷新工作流定义，给手动触发和周期触发共用。
		return runtime.RegisterWorkflow(runtime.cacheRefreshWorkflow())
	})
}

// handleWorkflowTrigger 解析工作流触发任务，并把请求转交给任务管理器启动工作流。
func (r *Runtime) handleWorkflowTrigger(ctx context.Context, task *asynq.Task) error {
	var payload taskqueue.WorkflowTriggerPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return errors.Wrap(err, "解析工作流触发任务载荷失败")
	}
	if strings.TrimSpace(payload.WorkflowName) == "" {
		return errors.Errorf("工作流名称不能为空")
	}
	// 把触发任务载荷转换成运行时统一的工作流启动参数，后续由 Manager 负责实际编排。
	spec := taskqueue.WorkflowStartSpec{
		WorkflowID:        payload.WorkflowID,
		Name:              payload.WorkflowName,
		Queue:             payload.Queue,
		Targets:           payload.Targets,
		ShardTotal:        payload.ShardTotal,
		GrayPercent:       payload.GrayPercent,
		Source:            payload.Source,
		TriggeredByUserID: payload.TriggeredByUserID,
		TriggeredByUser:   payload.TriggeredByUser,
		UniqueKey:         payload.UniqueKey,
	}
	if payload.Retry != nil {
		spec.RetryOverride = payload.Retry
	}
	if payload.TimeoutSeconds > 0 {
		spec.TimeoutOverride = time.Duration(payload.TimeoutSeconds) * time.Second
	}
	if payload.UniqueTTLSeconds > 0 {
		spec.UniqueTTL = time.Duration(payload.UniqueTTLSeconds) * time.Second
	}
	// 交给任务管理器做幂等、实例初始化和根节点调度；重复触发仅记录日志不报错。
	if _, err := r.manager.StartWorkflow(ctx, spec); err != nil {
		if errors.Is(err, taskqueue.ErrWorkflowAlreadyExists) {
			loggerx.Infow(ctx, "工作流 已存在并跳过重复触发",
				logx.Field("workflow", payload.WorkflowName),
				logx.Field("workflow_id", payload.WorkflowID),
			)
			return nil
		}
		if errors.Is(err, taskqueue.ErrWorkflowNotFound) {
			return errors.Wrapf(err, "启动工作流失败，当前 worker 未注册该工作流 workflow=%s source=%s queue=%s unique_key=%s，请确认 cron-control 与 cron-worker 使用同一镜像、同一 runtime.sample.yaml，且 workflows.user_tag.enabled 已开启",
				payload.WorkflowName,
				payload.Source,
				payload.Queue,
				payload.UniqueKey,
			)
		}
		return errors.Tag(err)
	}
	return nil
}

// handleWorkflowNoop 作为工作流结束节点的空实现，主要用于串联依赖关系。
func (r *Runtime) handleWorkflowNoop(context.Context, *asynq.Task) error {
	return nil
}

// handleCacheRefresh 负责解析缓存刷新任务并分发到各个目标处理器。
func (r *Runtime) handleCacheRefresh(ctx context.Context, task *asynq.Task) error {
	var payload taskqueue.CacheRefreshPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return errors.Wrap(err, "解析缓存刷新任务载荷失败")
	}
	return r.refreshTargets(ctx, payload.Operation, payload.Targets)
}

// aggregateCacheRefreshTasks 把同组缓存刷新请求合并为一条批量任务，减少短时间内重复刷新成本。
func (r *Runtime) aggregateCacheRefreshTasks(tasks []*asynq.Task) *asynq.Task {
	targetSet := make(map[string]struct{})
	operationSet := make(map[string]struct{})
	headers := map[string]string{"x-app-task-source": taskqueue.WorkflowSourceInternal}
	// 逐条读取原始短任务，合并请求头、操作类型和目标列表，生成一条批量刷新任务。
	for _, task := range tasks {
		if task == nil {
			continue
		}
		for key, value := range task.Headers() {
			if headers[key] == "" && value != "" {
				headers[key] = value
			}
		}
		var payload taskqueue.CacheRefreshPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			continue
		}
		if payload.Operation != "" {
			operationSet[payload.Operation] = struct{}{}
		}
		for _, target := range taskqueue.SplitStrings(payload.Targets, 1, 0) {
			targetSet[target] = struct{}{}
		}
	}
	targets := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	operations := make([]string, 0, len(operationSet))
	for operation := range operationSet {
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	// 合并后的 payload 只保留去重后的目标和操作摘要，避免同一窗口内重复刷新同类缓存。
	body, err := json.Marshal(taskqueue.CacheRefreshPayload{
		Operation: strings.Join(operations, ","),
		Targets:   targets,
	})
	if err != nil {
		return nil
	}
	return asynq.NewTaskWithHeaders(taskqueue.TypeCacheRefreshBatch, body, headers, asynq.Timeout(5*time.Minute), asynq.MaxRetry(3))
}

// refreshTargets 顺序执行目标刷新逻辑，并聚合多个目标的错误结果。
func (r *Runtime) refreshTargets(ctx context.Context, operation string, targets []string) error {
	var errs []error
	// 这里按目标顺序串行执行，优先保证同批次错误可观测和刷新结果可追踪。
	for _, target := range taskqueue.SplitStrings(targets, 1, 0) {
		handler := r.CacheRefreshHandler(target)
		if handler == nil {
			errs = append(errs, errors.Wrapf(errors.Errorf("不支持的缓存刷新目标: %s", target), "target=%s", target))
			continue
		}
		err := handler(ctx, r.svc)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "target=%s", target))
		}
	}
	if len(errs) > 0 {
		return stderrors.Join(errs...)
	}
	// 只有外层明确传入操作名时才打印完成日志，避免单目标零散调用刷屏。
	if operation != "" {
		loggerx.Infow(ctx, "缓存 刷新完成",
			logx.Field("operation", operation),
			logx.Field("targets", strings.Join(targets, ",")),
		)
	}
	return nil
}

// cacheRefreshWorkflow 定义通用缓存刷新工作流，支持分片和灰度下发。
func (r *Runtime) cacheRefreshWorkflow() *taskqueue.WorkflowDefinition {
	return &taskqueue.WorkflowDefinition{
		Name:         taskqueue.WorkflowNameCacheRefresh,
		Description:  "批量刷新公共下拉缓存工作流",
		DefaultQueue: taskqueue.QueueMaintenance,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			"refresh.batch": {
				Name:             "refresh.batch",
				TaskType:         taskqueue.TypeCacheRefreshBatch,
				Queue:            taskqueue.QueueMaintenance,
				MaxRetry:         3,
				Timeout:          5 * time.Minute,
				SupportsSharding: true,
				SupportsGray:     true,
				BuildPayload: func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
					targets := spec.Targets
					if len(targets) == 0 {
						targets = r.DefaultCacheRefreshTargets()
					}
					// 每个分片只处理自己负责的目标集合，避免多个任务重复刷新同一批缓存键。
					return json.Marshal(taskqueue.CacheRefreshPayload{
						WorkflowTaskMeta: taskqueue.WorkflowTaskMeta{
							WorkflowID:   spec.WorkflowID,
							WorkflowName: spec.Name,
							WorkflowNode: node.Name,
							ShardIndex:   shardIndex,
							ShardTotal:   shardTotal,
						},
						Operation: fmt.Sprintf("workflow:%s", spec.Name),
						Targets:   taskqueue.SplitStrings(targets, shardTotal, shardIndex),
					})
				},
			},
			"finalize": {
				Name:      "finalize",
				TaskType:  taskqueue.TypeWorkflowNoop,
				Queue:     taskqueue.QueueMaintenance,
				DependsOn: []string{"refresh.batch"},
				Timeout:   30 * time.Second,
				BuildPayload: func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
					// finalize 节点只承担 DAG 收尾职责，不再执行业务刷新逻辑。
					return json.Marshal(taskqueue.NoopPayload{
						WorkflowTaskMeta: taskqueue.WorkflowTaskMeta{
							WorkflowID:   spec.WorkflowID,
							WorkflowName: spec.Name,
							WorkflowNode: node.Name,
							ShardIndex:   shardIndex,
							ShardTotal:   shardTotal,
						},
						Note: "cache refresh workflow finished",
					})
				},
			},
		},
	}
}
