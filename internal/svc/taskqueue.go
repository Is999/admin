package svc

import (
	"context"
	"time"

	"admin/internal/types"
)

// TaskOption 定义任务投递时的可选配置函数。
type TaskOption func(options *TaskOptions)

// TaskOptions 描述一次任务投递时可覆盖的运行参数。
type TaskOptions struct {
	Queue     string        // 目标队列名称，为空时使用默认队列
	Retry     int           // 最大重试次数
	Timeout   time.Duration // 单个任务执行超时时间
	Delay     time.Duration // 相对当前时间的延迟投递时间
	ProcessAt *time.Time    // 指定绝对时间投递，和 Delay 互斥
	Deadline  *time.Time    // 任务截止时间，超过后不再执行
	UniqueTTL time.Duration // 去重窗口时长，窗口内相同任务只允许入队一次
	Group     string        // 聚合分组标识，相同分组的任务可被 Asynq 聚合
}

// TaskRuntimeAlert 描述业务模块上报给任务系统的运行异常。
// 任务系统负责限频和外部通知，业务模块只提供排障字段。
type TaskRuntimeAlert struct {
	Kind         string    // 异常类型，用于告警指纹和排障归类
	Title        string    // 告警标题
	Status       string    // 当前处理状态
	Component    string    // 发生异常的任务系统组件
	Operation    string    // 发生异常的运行操作
	TaskName     string    // 关联任务名称
	TaskType     string    // 关联任务类型
	WorkflowName string    // 关联工作流名称
	Cron         string    // 关联周期表达式
	TaskQueue    string    // 关联队列
	UniqueKey    string    // 关联幂等键
	Reason       string    // 异常原因摘要
	Advice       string    // 处理建议，留空时由告警发送端使用通用建议
	OccurredAt   time.Time // 发现异常的时间；为空时由任务系统补齐
}

const (
	// TaskRuntimeAlertKindCacheRefreshEnqueueFailed 表示缓存异步刷新请求投递失败。
	TaskRuntimeAlertKindCacheRefreshEnqueueFailed = "cache_refresh_enqueue_failed"
	// TaskRuntimeAlertKindAdminExcelExportFailed 表示管理员 Excel 异步导出任务失败。
	TaskRuntimeAlertKindAdminExcelExportFailed = "admin_excel_export_failed"
	// TaskRuntimeAlertKindUserExcelExportFailed 表示前台用户 Excel 异步导出任务失败。
	TaskRuntimeAlertKindUserExcelExportFailed = "user_excel_export_failed"
	// TaskRuntimeAlertKindCollectorEnqueueFailed 表示 Collector Kafka 投递失败。
	TaskRuntimeAlertKindCollectorEnqueueFailed = "collector_enqueue_failed"
	// TaskRuntimeAlertKindCollectorWorkerFailed 表示 Collector 后台消费链路失败。
	TaskRuntimeAlertKindCollectorWorkerFailed = "collector_worker_failed"
	// TaskRuntimeAlertKindCollectorInvalidEvent 表示 Collector 收到不可恢复的无效事件。
	TaskRuntimeAlertKindCollectorInvalidEvent = "collector_invalid_event"
	// TaskRuntimeAlertKindCollectorDeadEvent 表示 Collector 事件超过重试次数进入死信。
	TaskRuntimeAlertKindCollectorDeadEvent = "collector_dead_event"
	// TaskRuntimeAlertKindAppLifecycleFailed 表示服务启动或平滑停止生命周期失败。
	TaskRuntimeAlertKindAppLifecycleFailed = "app_lifecycle_failed"
	// TaskRuntimeAlertKindConfigReloadFailed 表示 config.yaml 热加载失败。
	TaskRuntimeAlertKindConfigReloadFailed = "config_reload_failed"
	// TaskRuntimeAlertKindRuntimeConfigReloadFailed 表示 DB 运行配置热加载失败。
	TaskRuntimeAlertKindRuntimeConfigReloadFailed = "runtime_config_reload_failed"
)

// TaskRuntimeAlerter 表示统一运行异常告警入口。
type TaskRuntimeAlerter interface {
	NotifyRuntimeAlert(ctx context.Context, alert TaskRuntimeAlert)
}

// TaskQueueReadiness 约束任务队列对健康检查暴露的底层依赖和运行态探测。
type TaskQueueReadiness interface {
	Ready(ctx context.Context, requireWorker bool, requireScheduler bool) error
}

// NotifyTaskRuntimeAlert 通过独立告警入口上报运行异常；未配置外部告警时静默降级。
func NotifyTaskRuntimeAlert(ctx context.Context, alerter TaskRuntimeAlerter, alert TaskRuntimeAlert) {
	if alerter == nil {
		return
	}
	alerter.NotifyRuntimeAlert(ctx, alert)
}

// WithTaskQueue 指定任务投递队列。
func WithTaskQueue(queue string) TaskOption {
	return func(options *TaskOptions) {
		options.Queue = queue
	}
}

// WithTaskRetry 指定任务最大重试次数。
func WithTaskRetry(retry int) TaskOption {
	return func(options *TaskOptions) {
		options.Retry = retry
	}
}

// WithTaskTimeout 指定任务超时时间。
func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(options *TaskOptions) {
		options.Timeout = timeout
	}
}

// WithTaskDelay 指定任务相对延迟执行时间。
func WithTaskDelay(delay time.Duration) TaskOption {
	return func(options *TaskOptions) {
		options.Delay = delay
	}
}

// WithTaskProcessAt 指定任务绝对执行时间。
func WithTaskProcessAt(processAt time.Time) TaskOption {
	return func(options *TaskOptions) {
		options.ProcessAt = new(processAt)
	}
}

// WithTaskDeadline 指定任务截止时间。
func WithTaskDeadline(deadline time.Time) TaskOption {
	return func(options *TaskOptions) {
		options.Deadline = new(deadline)
	}
}

// WithTaskUniqueTTL 指定任务去重窗口。
func WithTaskUniqueTTL(ttl time.Duration) TaskOption {
	return func(options *TaskOptions) {
		options.UniqueTTL = ttl
	}
}

// WithTaskGroup 指定任务聚合分组。
func WithTaskGroup(group string) TaskOption {
	return func(options *TaskOptions) {
		options.Group = group
	}
}

// TaskQueue 抽象任务系统对业务层暴露的最小能力。
// 业务 logic 只依赖这个接口，不直接依赖底层任务引擎实现，便于后续替换或拆分。
type TaskQueue interface {
	// IsEnabled 返回任务系统当前是否已启用。
	IsEnabled() bool

	// EnqueueTask 按任务类型和原始载荷投递一个通用后台任务。
	// 该接口适合业务层做最小封装后复用，避免直接依赖底层任务引擎细节。
	EnqueueTask(ctx context.Context, taskType string, payload []byte, opts ...TaskOption) error

	// EnqueueCacheRefresh 投递缓存刷新任务，服务缓存管理链路。
	// 后续新业务优先通过 EnqueueTask 自行封装，避免持续扩展特化接口。
	EnqueueCacheRefresh(ctx context.Context, operation string, cacheKeys []string) error

	// EnqueueWorkflowTrigger 提交一个已注册工作流的触发请求。
	// 工作流用于承载多步骤、可编排的后台执行链路。
	EnqueueWorkflowTrigger(ctx context.Context, req *types.TriggerTaskWorkflowReq) (*types.TaskWorkflowTriggerResp, error)

	// GetWorkflowStatus 查询工作流实例当前状态与节点执行结果。
	GetWorkflowStatus(ctx context.Context, workflowID string) (*types.TaskWorkflowStatusResp, error)

	// EnqueueRegisteredTask 按注册表配置投递一个通用任务。
	// 该接口用于后台任务管理页等需要使用任务注册表元数据的场景。
	EnqueueRegisteredTask(ctx context.Context, req *types.EnqueueTaskReq) (*types.TaskEnqueueResp, error)

	// ListRegisteredTaskTypes 返回当前系统已注册的任务类型清单。
	ListRegisteredTaskTypes(ctx context.Context) []types.TaskTypeRegistryItem

	// ListRegisteredWorkflows 返回当前系统已注册的工作流清单。
	ListRegisteredWorkflows(ctx context.Context) []types.WorkflowRegistryItem

	// ListTasks 按队列、状态和过滤条件查询任务列表。
	ListTasks(ctx context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error)

	// GetTaskInfo 查询单个任务的完整详情。
	GetTaskInfo(ctx context.Context, req *types.GetTaskInfoReq) (*types.TaskItem, error)

	// RunTask 让指定任务立即进入待执行状态。
	RunTask(ctx context.Context, req *types.OperateTaskReq) error

	// DeleteTask 删除指定任务。
	DeleteTask(ctx context.Context, req *types.OperateTaskReq) error

	// ListQueues 查询当前任务队列与 worker 概览。
	ListQueues(ctx context.Context) (*types.TaskQueueListResp, error)

	// PauseQueue 暂停指定队列的消费。
	PauseQueue(ctx context.Context, queue string) error

	// ResumeQueue 恢复指定队列的消费。
	ResumeQueue(ctx context.Context, queue string) error
}
