package taskqueue

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	redislock "admin/internal/infra/redsync"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/task/stats"
	"admin/internal/types"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// QueueCritical 高优先级业务队列。
	QueueCritical = "critical"
	// QueueDefault 默认业务队列。
	QueueDefault = "default"
	// QueueMaintenance 运维/缓存刷新等后台队列。
	QueueMaintenance = "maintenance"

	// TypeWorkflowTrigger 是触发工作流实例的入口任务。
	TypeWorkflowTrigger = "workflow:trigger"
	// TypeWorkflowNoop 是 DAG 中用于收尾/汇聚的空任务。
	TypeWorkflowNoop = "workflow:noop"
	// TypeCacheRefreshRequest 是请求聚合缓存刷新的原始任务。
	TypeCacheRefreshRequest = "cache:refresh:request"
	// TypeCacheRefreshBatch 是聚合后的批量缓存刷新任务。
	TypeCacheRefreshBatch = "cache:refresh:batch"

	// GroupCacheRefresh 是缓存刷新任务的聚合分组。
	GroupCacheRefresh = "cache-refresh"

	headerLocale       = "x-app-locale"               // 任务透传的语言标识
	headerUserID       = "x-app-user-id"              // 任务透传的操作者 ID
	headerUserName     = "x-app-user-name"            // 任务透传的操作者名称
	headerTaskName     = "x-app-task-name"            // 任务展示名称
	headerTaskSource   = "x-app-task-source"          // 任务触发来源
	HeaderPeriodicName = "x-app-periodic-name"        // 周期任务原始名称
	headerWorkflowID   = "x-app-workflow-id"          // 工作流实例 ID
	headerWorkflowName = "x-app-workflow-name"        // 工作流名称
	headerWorkflowNode = "x-app-workflow-node"        // 工作流节点名称
	headerShardIndex   = "x-app-workflow-shard-index" // 工作流分片序号
	headerShardTotal   = "x-app-workflow-shard-total" // 工作流分片总数
	headerAppID        = "x-app-id"                   // 当前应用 app_id

	taskSearchFieldTaskName     = "taskName"     // 任务结果或 payload 中的展示名称字段
	taskSearchFieldTaskType     = "taskType"     // 任务结果中回写的任务类型字段
	taskSearchFieldWorkflowID   = "workflowId"   // 工作流实例 ID 字段，用于 payload/result 链路匹配
	taskSearchFieldWorkflowName = "workflowName" // 工作流名称字段，周期任务排查常用该字段定位入口任务
	taskSearchFieldWorkflowNode = "workflowNode" // 工作流节点字段，用于节点任务按脚本名检索
	taskSearchFieldTaskSource   = "taskSource"   // 任务来源字段，用于区分 api/periodic/internal 等触发来源
	taskSearchFieldMode         = "mode"         // 用户标签等任务的运行模式字段，用于保留 full/delta 等排查入口
	taskSearchFieldUniqueKey    = "uniqueKey"    // 工作流或任务幂等键字段，便于按周期 unique_key 反查任务

	taskExecutionStatusSuccess = "success" // 普通任务执行结果成功状态

	taskFinalWriteTimeout  = 5 * time.Second     // 任务收尾写 Redis 的短超时，避免业务 ctx 超时后失败状态无法落库
	taskListFilterPageSize = 100                 // 任务列表二次过滤单批读取量，和接口最大页大小保持一致
	taskListFilterMaxPages = 50                  // 任务列表二次过滤最大扫描页数，避免无索引历史查询拖垮 Redis
	taskRecordTTLBuffer    = time.Hour           // 终态任务 hash 额外保留窗口，避免清理边界抖动影响列表查询
	taskRuntimeAlertTTL    = 5 * time.Minute     // 任务系统同一运行异常的告警限频窗口，避免后台循环刷屏
	periodicConfigAlertTTL = taskRuntimeAlertTTL // 周期任务同一配置异常复用任务运行异常的告警限频窗口
)

// WorkflowTriggerPayload 是 workflow:trigger 任务的负载。
type WorkflowTriggerPayload struct {
	WorkflowID        string   `json:"workflowId"`                  // 工作流实例 ID
	WorkflowName      string   `json:"workflowName"`                // 工作流定义名称
	Queue             string   `json:"queue,omitempty"`             // 投递队列，留空时回退默认队列
	Targets           []string `json:"targets,omitempty"`           // 本次工作流要处理的目标集合
	ShardTotal        int      `json:"shardTotal,omitempty"`        // 分片总数
	GrayPercent       int      `json:"grayPercent,omitempty"`       // 灰度执行百分比
	Source            string   `json:"source,omitempty"`            // 触发来源
	TriggeredByUserID int      `json:"triggeredByUserId,omitempty"` // 触发人用户 ID
	TriggeredByUser   string   `json:"triggeredByUser,omitempty"`   // 触发人用户名
	PeriodicName      string   `json:"periodicName,omitempty"`      // 周期任务原始名称
	Retry             *int     `json:"retry,omitempty"`             // 重试次数覆盖值
	TimeoutSeconds    int      `json:"timeoutSeconds,omitempty"`    // 超时时间，单位秒
	UniqueKey         string   `json:"uniqueKey,omitempty"`         // 幂等键
	UniqueTTLSeconds  int      `json:"uniqueTTLSeconds,omitempty"`  // 幂等锁有效期，单位秒
}

// CacheRefreshPayload 是缓存刷新任务的负载。
type CacheRefreshPayload struct {
	WorkflowTaskMeta          // 关联的工作流节点元数据，非工作流触发时可为空
	Operation        string   `json:"operation,omitempty"` // 刷新动作说明，便于日志排查
	Targets          []string `json:"targets"`             // 本次需要刷新的缓存目标列表
}

// NoopPayload 是汇聚/收尾节点使用的空任务负载。
type NoopPayload struct {
	WorkflowTaskMeta        // 关联的工作流节点元数据
	Note             string `json:"note,omitempty"` // 备注信息，用于标识空节点用途
}

// TaskExecutionResult 表示任务执行成功后写回到 Asynq 的结果摘要。
// 管理后台的“任务结果”优先展示该结构，便于直接看到本次执行的关键产物。
type TaskExecutionResult struct {
	Status         string              `json:"status"`                   // 执行状态，当前统一写 success
	TraceID        string              `json:"traceId,omitempty"`        // 链路追踪 ID
	TaskID         string              `json:"taskId,omitempty"`         // 任务 ID
	TaskType       string              `json:"taskType,omitempty"`       // 任务类型
	TaskName       string              `json:"taskName,omitempty"`       // 任务名称/脚本名称
	TaskSource     string              `json:"taskSource,omitempty"`     // 任务来源（如 api/periodic/internal）
	TaskQueue      string              `json:"taskQueue,omitempty"`      // 所属队列
	Queue          string              `json:"queue,omitempty"`          // payload 中声明的业务执行队列
	LatencyMS      int64               `json:"latencyMs,omitempty"`      // 执行耗时（毫秒）
	BizCode        int                 `json:"bizCode,omitempty"`        // 统一业务状态码
	BizMessage     string              `json:"bizMessage,omitempty"`     // 统一业务结果说明
	WorkflowID     string              `json:"workflowId,omitempty"`     // 工作流实例 ID
	WorkflowName   string              `json:"workflowName,omitempty"`   // 工作流名称
	WorkflowNode   string              `json:"workflowNode,omitempty"`   // 工作流节点名称
	ShardIndex     int                 `json:"shardIndex,omitempty"`     // 分片索引
	ShardTotal     int                 `json:"shardTotal,omitempty"`     // 分片总数
	GrayPercent    int                 `json:"grayPercent,omitempty"`    // 灰度执行百分比
	Operation      string              `json:"operation,omitempty"`      // 业务动作，如缓存刷新操作名
	Mode           string              `json:"mode,omitempty"`           // 业务模式，如 full/delta/targeted
	TargetCount    int                 `json:"targetCount,omitempty"`    // 目标数量
	UIDCount       int                 `json:"uidCount,omitempty"`       // UID 数量
	TagTypeCount   int                 `json:"tagTypeCount,omitempty"`   // 标签类型数量
	Targets        []string            `json:"targets,omitempty"`        // 目标预览，避免结果过长仅保留前几项
	PayloadKeys    []string            `json:"payloadKeys,omitempty"`    // payload 顶层字段摘要，便于快速判断本次入参结构
	StartedAt      string              `json:"startedAt,omitempty"`      // 开始执行时间
	FinishedAt     string              `json:"finishedAt,omitempty"`     // 结果写回时间
	ExecutionTrace *taskstats.Snapshot `json:"executionTrace,omitempty"` // 本次任务处理量统计摘要
}

// GroupAggregator 定义单个聚合分组的批处理器。
// 同一个 group 内的多条任务会在 worker 聚合窗口结束后交给对应聚合器合并成一个批任务。
type GroupAggregator func(tasks []*asynq.Task) *asynq.Task

// TaskFinalFailureHook 定义任务进入终态失败后的业务清理钩子。
// 钩子只在任务无自动重试机会后触发，用于释放业务侧资源。
type TaskFinalFailureHook func(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, err error) error

// PeriodicConfigInvalidHook 定义周期任务配置异常后的告警钩子。
// 钩子只用于外部通知，不改变调度器跳过无效配置的容错语义。
type PeriodicConfigInvalidHook func(ctx context.Context, report PeriodicConfigInvalidReport) error

// PeriodicConfigInvalidReport 描述一次周期任务配置校验失败。
type PeriodicConfigInvalidReport struct {
	Index        int                       // 配置在合并后周期任务列表中的位置
	Task         config.TaskPeriodicConfig // 失败的周期任务配置
	Reason       string                    // 配置失败原因
	OccurredAt   time.Time                 // 发现异常的时间
	TriggerCount int                       // 当前告警窗口累计触发次数，包含本次触发
}

// TaskRuntimeAlertHook 定义任务系统运行异常后的告警钩子。
// 钩子只用于外部通知，不改变调度器和清理任务的容错语义。
type TaskRuntimeAlertHook func(ctx context.Context, alert TaskRuntimeAlert) error

// TaskRuntimeAlert 描述一次任务系统运行期异常。
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
	Advice       string    // 处理建议，留空时使用告警发送端通用建议
	OccurredAt   time.Time // 发现异常的时间
	TriggerCount int       // 当前告警窗口累计触发次数，包含本次触发
}

// periodicConfigAlertState 保存单条周期配置异常的限频状态。
type periodicConfigAlertState struct {
	LastSentAt      time.Time // 最近一次发送告警的时间
	SuppressedCount int       // 限频窗口内被合并的重复触发次数
}

// taskRuntimeAlertState 保存单条运行异常的限频状态。
type taskRuntimeAlertState struct {
	LastSentAt      time.Time // 最近一次发送告警的时间
	SuppressedCount int       // 限频窗口内被合并的重复触发次数
}

// Manager 统一承接任务客户端、Worker、调度器和工作流状态管理。
// 该层只依赖 Redis 与 Asynq，不直接依赖具体业务逻辑。
type Manager struct {
	cfgValue             atomic.Value          // 任务系统配置快照，支持运行期热更新读取
	schedulerStatusValue atomic.Value          // 调度器运行状态快照，供接口与日志复用
	redis                redis.UniversalClient // 任务系统使用的 Redis 客户端
	client               *asynq.Client         // Asynq 任务投递客户端
	inspector            *asynq.Inspector      // Asynq 队列巡检客户端
	mux                  *asynq.ServeMux       // Worker 处理器路由复用器
	server               *asynq.Server         // Worker 服务实例
	leader               *LeaderRunner         // Scheduler 的分布式 leader 选举器
	cancel               context.CancelFunc    // 用于停止调度器 leader 循环
	wg                   sync.WaitGroup        // 等待后台调度协程退出
	logger               asynq.Logger          // Asynq 运行日志适配器
	tracer               trace.Tracer          // 任务链路追踪器
	instance             string                // 当前进程实例 ID，用于 leader 日志和区分多实例

	lifecycleMu              sync.Mutex                          // 保护 Worker / Scheduler 启停状态，避免并发启动或停止产生竞态
	schedulerStatusMu        sync.Mutex                          // 保护调度器状态快照更新，避免多个后台协程并发覆盖
	archivedCleanStop        context.CancelFunc                  // 停止归档失败任务过期清理协程
	mu                       sync.RWMutex                        // 保护工作流定义和周期任务配置的并发读写
	workflows                map[string]*WorkflowDefinition      // 已注册的工作流定义集合
	periodic                 []config.TaskPeriodicConfig         // 通过配置或插件补充的周期任务列表
	handlers                 map[string]struct{}                 // 已注册的任务类型集合，用于限制通用任务投递
	aggregates               map[string]GroupAggregator          // 已注册的分组聚合器
	finalFailureHooks        []TaskFinalFailureHook              // 任务终态失败后的业务清理钩子
	periodicConfigHooks      []PeriodicConfigInvalidHook         // 周期任务配置异常后的外部告警钩子
	runtimeAlertHooks        []TaskRuntimeAlertHook              // 任务系统运行异常后的外部告警钩子
	periodicConfigAlertedMap map[string]periodicConfigAlertState // 周期任务配置异常限频记录
	runtimeAlertedMap        map[string]taskRuntimeAlertState    // 任务系统运行异常限频记录
}

// New 创建任务管理器。
func New(cfg config.TaskQueueConfig, client redis.UniversalClient) *Manager {
	if !cfg.Enabled || client == nil {
		return nil
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil
	}
	m := &Manager{
		redis:                    client,
		client:                   asynq.NewClientFromRedisClient(client),
		inspector:                asynq.NewInspectorFromRedisClient(client),
		mux:                      asynq.NewServeMux(),
		logger:                   asynqLogger{},
		tracer:                   otel.Tracer("admin/task"),
		instance:                 uuid.NewString(),
		workflows:                make(map[string]*WorkflowDefinition),
		periodic:                 make([]config.TaskPeriodicConfig, 0),
		handlers:                 make(map[string]struct{}),
		aggregates:               make(map[string]GroupAggregator),
		periodicConfigAlertedMap: make(map[string]periodicConfigAlertState),
		runtimeAlertedMap:        make(map[string]taskRuntimeAlertState),
	}
	m.UpdateConfig(cfg)
	m.Use(m.traceAndLogMiddleware())
	return m
}

// appNamespace 返回当前任务系统实例的站点命名空间。
// 多站点共用一套 task redis 时，所有队列名、锁 key、工作流 key 都必须带站点前缀，避免互相串扰。
func (m *Manager) appNamespace() string {
	if m == nil {
		return ""
	}
	appID := strings.TrimSpace(m.CurrentConfig().AppID)
	if appID == "" {
		return ""
	}
	return appID
}

// useRedisNamespace 将当前任务管理器配置的 app_id 绑定到 Redis key helper。
func (m *Manager) useRedisNamespace() bool {
	appID := m.appNamespace()
	return appID != "" && appID == runtimecfg.AppID() && keys.Prefix() != ""
}

// namespacedQueueName 返回底层 Asynq 实际使用的队列名。
// 对外接口仍暴露逻辑队列名，内部统一追加站点前缀，避免多站点共用同一套 task redis 时互相消费任务。
func (m *Manager) namespacedQueueName(queue string) string {
	queue = strings.TrimSpace(queue)
	if queue == "" {
		return ""
	}
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskQueueName(queue)
}

// displayQueueName 把底层带站点前缀的队列名还原成对外展示的逻辑队列名。
func (m *Manager) displayQueueName(queue string) string {
	queue = strings.TrimSpace(queue)
	if queue == "" {
		return ""
	}
	return keys.TrimTaskQueueName(queue)
}

// namespacedGroup 返回当前站点下聚合任务使用的分组名。
func (m *Manager) namespacedGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskQueueName(group)
}

// displayGroupName 把底层聚合分组名还原成对外展示名称。
func (m *Manager) displayGroupName(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	return keys.TrimTaskQueueName(group)
}

// CurrentConfig 返回当前生效的任务系统配置快照。
func (m *Manager) CurrentConfig() config.TaskQueueConfig {
	if m == nil {
		return config.TaskQueueConfig{}
	}
	if cfg, ok := m.cfgValue.Load().(config.TaskQueueConfig); ok {
		return cfg
	}
	return config.TaskQueueConfig{}
}

// UpdateConfig 原子替换任务系统配置快照。
// 该方法不会在线重建 Worker / Redis 连接，只影响后续按快照读取的配置项。
func (m *Manager) UpdateConfig(cfg config.TaskQueueConfig) {
	if m == nil {
		return
	}
	m.cfgValue.Store(cfg)
	m.syncSchedulerConfigStatus(cfg)
}

// IsEnabled 返回任务系统是否启用。
func (m *Manager) IsEnabled() bool {
	return m != nil && m.client != nil && m.redis != nil && m.useRedisNamespace()
}

// EnqueueTask 提供轻量级基础任务投递入口。
func (m *Manager) EnqueueTask(ctx context.Context, taskType string, payload []byte, opts ...svc.TaskOption) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}

	// options 汇总调用方传入的函数式配置，后续统一交给 enqueueTaskWithOptions 转成 Asynq 选项。
	options := &svc.TaskOptions{}
	for _, opt := range opts {
		opt(options)
	}

	_, err := m.enqueueTaskWithOptions(ctx, m.newTask(ctx, taskType, payload, nil), options)
	return errors.Tag(err)
}

// RegisterHandler 注册任务处理器。
func (m *Manager) RegisterHandler(pattern string, handler asynq.Handler) (err error) {
	if m == nil || m.mux == nil {
		return nil
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.Errorf("任务处理器 pattern 不能为空")
	}
	if handler == nil {
		return errors.Errorf("任务处理器不能为空: %s", pattern)
	}

	// 先维护本地注册表，避免重复注册穿透到 Asynq ServeMux 后 panic。
	m.mu.Lock()
	if m.handlers == nil {
		m.handlers = make(map[string]struct{})
	}
	if _, exists := m.handlers[pattern]; exists {
		m.mu.Unlock()
		return errors.Errorf("任务处理器已注册: %s", pattern)
	}
	m.handlers[pattern] = struct{}{}
	m.mu.Unlock()

	// Asynq ServeMux 遇到重复注册会 panic，这里统一转成错误返回并回滚注册表。
	defer func() {
		if recovered := recover(); recovered != nil {
			m.mu.Lock()
			delete(m.handlers, pattern)
			m.mu.Unlock()
			err = errors.Errorf("注册任务处理器失败: %s: %v", pattern, recovered)
		}
	}()
	m.mux.Handle(pattern, handler)
	allowTaskMetricTaskTypeLabel(pattern)
	return nil
}

// Use 为 Worker 注册任务处理中间件，供运行时或业务插件扩展链路能力。
func (m *Manager) Use(middlewares ...asynq.MiddlewareFunc) {
	if m == nil || m.mux == nil {
		return
	}
	for _, middleware := range middlewares {
		if middleware == nil {
			continue
		}
		m.mux.Use(middleware)
	}
}

// RegisterGroupAggregator 注册聚合分组处理器，供业务插件把多条短任务合并成批处理任务。
func (m *Manager) RegisterGroupAggregator(group string, aggregator GroupAggregator) error {
	if m == nil {
		return nil
	}
	group = m.namespacedGroup(group)
	if group == "" {
		return errors.Errorf("任务分组不能为空")
	}
	if aggregator == nil {
		return errors.Errorf("任务分组聚合器为空: %s", group)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aggregates == nil {
		m.aggregates = make(map[string]GroupAggregator)
	}
	if _, exists := m.aggregates[group]; exists {
		return errors.Errorf("任务分组聚合器已注册: %s", group)
	}
	m.aggregates[group] = aggregator
	return nil
}

// RegisterFinalFailureHook 注册任务终态失败后的清理钩子。
// 钩子由业务插件在启动期注册，Manager 只负责在 Asynq 确认任务不会继续自动重试后按顺序触发。
func (m *Manager) RegisterFinalFailureHook(hook TaskFinalFailureHook) error {
	if m == nil || hook == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finalFailureHooks = append(m.finalFailureHooks, hook)
	return nil
}

// RegisterPeriodicConfigInvalidHook 注册周期任务配置异常告警钩子。
// 钩子会按配置指纹限频触发，避免调度器周期同步时重复推送同一条异常。
func (m *Manager) RegisterPeriodicConfigInvalidHook(hook PeriodicConfigInvalidHook) error {
	if m == nil || hook == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.periodicConfigHooks = append(m.periodicConfigHooks, hook)
	return nil
}

// RegisterRuntimeAlertHook 注册任务系统运行异常告警钩子。
// 钩子会按异常指纹限频触发，用于调度、投递和清理等外层运行错误通知。
func (m *Manager) RegisterRuntimeAlertHook(hook TaskRuntimeAlertHook) error {
	if m == nil || hook == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimeAlertHooks = append(m.runtimeAlertHooks, hook)
	return nil
}

// RegisterWorkflow 注册 DAG 工作流定义。
func (m *Manager) RegisterWorkflow(def *WorkflowDefinition) error {
	if m == nil || def == nil {
		return nil
	}
	if err := validateWorkflowDefinition(def); err != nil {
		return errors.Tag(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workflows == nil {
		m.workflows = make(map[string]*WorkflowDefinition)
	}
	if _, exists := m.workflows[def.Name]; exists {
		return errors.Errorf("工作流已注册: %s", def.Name)
	}
	m.workflows[def.Name] = def
	return nil
}

// ListRegisteredTaskTypes 返回当前进程已注册的任务类型清单。
func (m *Manager) ListRegisteredTaskTypes() []types.TaskTypeRegistryItem {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]types.TaskTypeRegistryItem, 0, len(m.handlers))
	for taskType := range m.handlers {
		taskType = strings.TrimSpace(taskType)
		if taskType == "" {
			continue
		}
		items = append(items, buildTaskTypeRegistryItem(taskType))
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].TaskType < items[right].TaskType
	})
	return items
}

// ListRegisteredWorkflows 返回当前进程已注册的工作流定义快照。
func (m *Manager) ListRegisteredWorkflows() []types.WorkflowRegistryItem {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]types.WorkflowRegistryItem, 0, len(m.workflows))
	for _, def := range m.workflows {
		if def == nil {
			continue
		}
		items = append(items, types.WorkflowRegistryItem{
			Name:           strings.TrimSpace(def.Name),
			Description:    strings.TrimSpace(def.Description),
			DefaultQueue:   strings.TrimSpace(def.DefaultQueue),
			NodeCount:      len(def.Nodes),
			UsageHint:      workflowUsageHint(strings.TrimSpace(def.Name)),
			TargetsExample: workflowTargetsExample(strings.TrimSpace(def.Name)),
		})
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].Name < items[right].Name
	})
	return items
}

// buildTaskTypeRegistryItem 构造任务类型注册清单项，补充管理后台展示需要的说明和示例。
func buildTaskTypeRegistryItem(taskType string) types.TaskTypeRegistryItem {
	item := types.TaskTypeRegistryItem{
		TaskType:          taskType,
		Description:       taskTypeDescription(taskType),
		UsageHint:         taskTypeUsageHint(taskType),
		PayloadExample:    taskTypePayloadExample(taskType),
		ManualRecommended: taskTypeManualRecommended(taskType),
	}
	return item
}

// writeTaskResult 把任务执行结果写回 Asynq，便于管理后台查看成功记录的结果摘要。
func (m *Manager) writeTaskResult(ctx context.Context, task *asynq.Task, meta *requestctx.Meta, startedAt time.Time, statsSnapshot *taskstats.Snapshot) {
	if task == nil || task.ResultWriter() == nil {
		return
	}
	resultBytes, err := json.Marshal(buildTaskExecutionResult(task, meta, startedAt, statsSnapshot))
	if err != nil {
		loggerx.Errorw(ctx, "任务结果 序列化失败", err, taskLogFields(task)...)
		return
	}
	if _, err = task.ResultWriter().Write(resultBytes); err != nil {
		loggerx.Errorw(ctx, "任务结果 回写失败", err, taskLogFields(task)...)
	}
}

// RegisterPeriodicTask 注册额外的周期工作流配置，供插件在启动期补充调度任务。
func (m *Manager) RegisterPeriodicTask(cfg config.TaskPeriodicConfig) error {
	if m == nil {
		return nil
	}
	key, err := periodicTaskKey(cfg)
	if err != nil {
		return errors.Tag(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	currentCfg := m.CurrentConfig()
	for _, existing := range append(append([]config.TaskPeriodicConfig(nil), currentCfg.Periodic...), m.periodic...) {
		existingKey, keyErr := periodicTaskKey(existing)
		if keyErr == nil && existingKey == key {
			return errors.Errorf("周期任务已注册: %s", key)
		}
	}
	m.periodic = append(m.periodic, cfg)
	return nil
}

// StartWorkflow 供 Worker 处理入口任务时直接启动 DAG 实例。
func (m *Manager) StartWorkflow(ctx context.Context, spec WorkflowStartSpec) (string, error) {
	return m.startWorkflow(ctx, spec)
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
			if err != nil {
				loggerx.Errorw(context.Background(), "任务队列 健康检查失败", err, logx.Field("app_id", m.appNamespace()))
			}
		},
	})
	if err := server.Start(m.mux); err != nil {
		return errors.Tag(err)
	}
	m.server = server
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
		m.markSchedulerStopped("任务系统未启用，调度器未启动")
		return nil
	}
	if !m.schedulerEnabled() {
		m.markSchedulerStopped("调度器开关未开启")
		return nil
	}
	if m.periodicTaskCount() == 0 {
		m.markSchedulerStopped("未配置有效周期任务")
		return nil
	}
	// 避免重复启动：同一进程只允许存在一个 scheduler 选举循环。
	if m.cancel != nil {
		m.markSchedulerWaitingLeader("调度器已在运行，等待 leader 状态变化")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.markSchedulerWaitingLeader("调度器 leader 选举已启动")
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
			m.markSchedulerLeaderReleased("调度器已释放 leader，等待重新竞争")
			loggerx.Infow(leaderCtx, "任务调度 主节点已停止",
				logx.Field("instance", m.instance),
			)
		}, nil
	})
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// 后台运行 leader 循环，Stop() 时通过 cancel 触发退出。
		m.leader.Start(ctx)
	}()
	return nil
}

// Stop 停止 Worker 与调度器后台协程。
func (m *Manager) Stop(context.Context) error {
	if m == nil {
		return nil
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.server != nil {
		m.server.Shutdown()
		m.server = nil
	}
	if m.archivedCleanStop != nil {
		m.archivedCleanStop()
		m.archivedCleanStop = nil
	}
	m.wg.Wait()
	m.markSchedulerStopped("调度器已停止")
	return nil
}

// EnqueueWorkflowTrigger 通过统一入口任务触发一个工作流实例。
func (m *Manager) EnqueueWorkflowTrigger(ctx context.Context, req *types.TriggerTaskWorkflowReq) (*types.TaskWorkflowTriggerResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("工作流请求不能为空")
	}

	// 入参校验与工作流定义预检查（避免未知 workflow 入队后反复重试）。
	workflowName := strings.TrimSpace(req.Name)
	if _, err := m.workflowDefinition(workflowName); err != nil {
		return nil, errors.Tag(err)
	}
	if req.ProcessAt != "" && req.ProcessInSeconds != nil && *req.ProcessInSeconds > 0 {
		return nil, errors.Errorf("workflow 的 processAt 和 processInSeconds 不能同时设置")
	}

	// 幂等键预占：为延迟/定时触发预留唯一键窗口，避免并发重复触发。
	workflowID := newID()
	uniqueKey := strings.TrimSpace(req.UniqueKey)
	uniqueTTL := m.uniqueTTL(req.UniqueTTLSeconds)
	now := time.Now()
	uniqueReservationTTL, err := m.workflowTriggerUniqueReservationTTL(req, uniqueTTL, now)
	if err != nil {
		return nil, errors.Tag(err)
	}
	uniqueReserved := false
	if uniqueKey != "" && uniqueTTL > 0 {
		if err := redislock.WithLock(ctx, m.redis, m.workflowUniqueLockKey(workflowName, uniqueKey), m.workflowUniqueLockTTL(), func(lockCtx context.Context) error {
			locked, reserveErr := m.reserveWorkflowUnique(lockCtx, workflowName, uniqueKey, workflowID, uniqueReservationTTL)
			if reserveErr != nil {
				return errors.Wrapf(reserveErr, "预占工作流唯一键失败 workflow=%s unique_key=%s workflow_id=%s", workflowName, uniqueKey, workflowID)
			}
			if !locked {
				return ErrWorkflowAlreadyExists
			}
			uniqueReserved = true
			return nil
		}); err != nil {
			return nil, errors.Tag(err)
		}
	}

	// 构造 workflow:trigger payload（补齐触发人、重试/超时覆盖等）。
	payload := WorkflowTriggerPayload{
		WorkflowID:        workflowID,
		WorkflowName:      workflowName,
		Queue:             strings.TrimSpace(req.Queue),
		Targets:           normalizeStrings(req.Targets),
		ShardTotal:        req.ShardTotal,
		GrayPercent:       req.GrayPercent,
		Source:            WorkflowSourceAPI,
		UniqueKey:         uniqueKey,
		TriggeredByUserID: 0,
	}
	if req.Retry != nil {
		payload.Retry = req.Retry
	}
	if req.TimeoutSeconds != nil {
		payload.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.UniqueTTLSeconds != nil {
		payload.UniqueTTLSeconds = *req.UniqueTTLSeconds
	} else if uniqueKey != "" {
		payload.UniqueTTLSeconds = int(uniqueTTL.Seconds())
	}
	if meta := requestctx.FromContext(ctx); meta != nil {
		payload.TriggeredByUserID = meta.UserID
		payload.TriggeredByUser = meta.UserName
	}
	body, err := json.Marshal(payload)
	if err != nil {
		if uniqueReserved {
			_ = m.releaseWorkflowUniqueReservation(ctx, workflowName, uniqueKey, workflowID)
		}
		return nil, errors.Tag(err)
	}

	// 组装 Asynq 入队选项（queue/taskID/retry/timeout/processAt/deadline/unique）。
	task := m.newTask(ctx, TypeWorkflowTrigger, body, map[string]string{
		headerTaskName:     workflowTriggerTaskName(workflowName, ""),
		headerTaskSource:   WorkflowSourceAPI,
		headerWorkflowID:   workflowID,
		headerWorkflowName: workflowName,
	})
	queue := helper.FirstNonEmptyString(req.Queue, m.defaultWorkflowQueue())
	opts := []asynq.Option{
		asynq.Queue(m.namespacedQueueName(queue)),
		asynq.TaskID(workflowID),
		asynq.MaxRetry(m.defaultRetry(req.Retry)),
	}
	if retention := m.completedRetention(); retention > 0 {
		opts = append(opts, asynq.Retention(retention))
	}
	if timeout := m.defaultTimeout(req.TimeoutSeconds); timeout > 0 {
		opts = append(opts, asynq.Timeout(timeout))
	}
	if req.ProcessInSeconds != nil && *req.ProcessInSeconds > 0 {
		opts = append(opts, asynq.ProcessIn(time.Duration(*req.ProcessInSeconds)*time.Second))
	}
	if req.ProcessAt != "" {
		t, parseErr := time.Parse(time.RFC3339, req.ProcessAt)
		if parseErr != nil {
			if uniqueReserved {
				_ = m.releaseWorkflowUniqueReservation(ctx, workflowName, uniqueKey, workflowID)
			}
			return nil, errors.Wrap(parseErr, "解析 workflow processAt 失败")
		}
		opts = append(opts, asynq.ProcessAt(t))
	}
	if req.Deadline != "" {
		t, parseErr := time.Parse(time.RFC3339, req.Deadline)
		if parseErr != nil {
			if uniqueReserved {
				_ = m.releaseWorkflowUniqueReservation(ctx, workflowName, uniqueKey, workflowID)
			}
			return nil, errors.Wrap(parseErr, "解析 workflow deadline 失败")
		}
		opts = append(opts, asynq.Deadline(t))
	}
	if req.UniqueKey != "" {
		uniqueTTL := m.uniqueTTL(req.UniqueTTLSeconds)
		if uniqueTTL > 0 {
			opts = append(opts, asynq.Unique(uniqueTTL))
		}
	}

	// 入队并生成回执；失败时释放幂等预占。
	info, err := m.client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		if uniqueReserved {
			_ = m.releaseWorkflowUniqueReservation(ctx, workflowName, uniqueKey, workflowID)
		}
		return nil, errors.Tag(err)
	}
	resp := &types.TaskWorkflowTriggerResp{
		TaskID:       info.ID,
		WorkflowID:   workflowID,
		WorkflowName: payload.WorkflowName,
		Queue:        m.displayQueueName(info.Queue),
	}
	if !info.NextProcessAt.IsZero() {
		resp.ProcessAt = info.NextProcessAt.Format(time.RFC3339)
	}
	return resp, nil
}

// GetWorkflowStatus 读取工作流实例的当前执行状态。
func (m *Manager) GetWorkflowStatus(ctx context.Context, workflowID string) (*types.TaskWorkflowStatusResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	meta, nodes, err := m.readWorkflowStatus(ctx, workflowID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp := &types.TaskWorkflowStatusResp{
		WorkflowID:   meta.WorkflowID,
		WorkflowName: meta.WorkflowName,
		Status:       meta.Status,
		Source:       meta.Source,
		Queue:        m.displayQueueName(meta.Queue),
		Targets:      meta.Targets,
		ShardTotal:   meta.ShardTotal,
		GrayPercent:  meta.GrayPercent,
		ErrorMessage: meta.ErrorMessage,
		CreatedAt:    meta.CreatedAt,
		UpdatedAt:    meta.UpdatedAt,
		FinishedAt:   meta.FinishedAt,
		DurationMS:   durationBetweenRFC3339(meta.CreatedAt, meta.FinishedAt),
		Nodes:        make([]types.TaskWorkflowNodeItem, 0, len(nodes)),
	}
	workflowStatsSnapshots := make([]*taskstats.Snapshot, 0, len(nodes))
	workflowProgressItems := make([]*taskstats.Progress, 0, len(nodes))
	def, _ := m.workflowDefinition(meta.WorkflowName)
	for _, node := range nodes {
		nodeProgress := workflowNodeProgress(node, plannedWorkflowNodeInstances(def, meta, node))
		if nodeProgress != nil {
			workflowProgressItems = append(workflowProgressItems, nodeProgress)
		}
		shardTraces := make([]types.TaskWorkflowShardTraceItem, 0, len(node.ShardTraces))
		for _, shardTrace := range node.ShardTraces {
			shardTraces = append(shardTraces, types.TaskWorkflowShardTraceItem{
				ShardIndex:     shardTrace.ShardIndex,
				ShardTotal:     shardTrace.ShardTotal,
				Status:         shardTrace.Status,
				Progress:       shardTrace.Progress,
				ExecutionTrace: shardTrace.ExecutionTrace,
			})
		}
		if node.ExecutionTrace != nil && !node.ExecutionTrace.Empty() {
			workflowStatsSnapshots = append(workflowStatsSnapshots, node.ExecutionTrace)
		}
		resp.Nodes = append(resp.Nodes, types.TaskWorkflowNodeItem{
			Name:           node.Name,
			TaskType:       node.TaskType,
			Queue:          m.displayQueueName(node.Queue),
			Status:         node.Status,
			DependsOn:      node.DependsOn,
			Expected:       node.Expected,
			Succeeded:      node.Succeeded,
			Failed:         node.Failed,
			Skipped:        node.Skipped,
			ErrorMessage:   node.ErrorMessage,
			StartedAt:      node.StartedAt,
			FinishedAt:     node.FinishedAt,
			DurationMS:     durationBetweenRFC3339(node.StartedAt, node.FinishedAt),
			Progress:       nodeProgress,
			ExecutionTrace: node.ExecutionTrace,
			ShardTraces:    shardTraces,
		})
	}
	resp.Progress = taskstats.MergeProgress(taskstats.ProgressUnitNodeInstance, meta.Status, workflowProgressItems...)
	resp.ExecutionTrace = taskstats.MergeSnapshots("workflow."+strings.TrimSpace(meta.WorkflowName), workflowStatsSnapshots...)
	return resp, nil
}

// ListQueues 返回当前队列和在线 worker 的运行概览。
func (m *Manager) ListQueues(ctx context.Context) (*types.TaskQueueListResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	rawQueueNames, err := m.inspector.Queues()
	if err != nil {
		// 某些 Redis 托管环境会限制 Asynq `Queues()` 内部脚本命令，
		// 这里降级回配置中的静态队列名，保证运维页至少能看到核心队列概览。
		rawQueueNames = m.configuredQueueNames()
		if len(rawQueueNames) == 0 {
			return nil, errors.Tag(err)
		}
	}
	queueNames := m.visibleQueueNames(rawQueueNames)
	sort.Strings(queueNames)
	resp := &types.TaskQueueListResp{
		Queues: make([]types.TaskQueueItem, 0, len(queueNames)),
	}
	resp.Scheduler = m.schedulerStatusSnapshot()
	for _, queue := range queueNames {
		info, infoErr := m.inspector.GetQueueInfo(m.namespacedQueueName(queue))
		if infoErr != nil {
			// 部分托管 Redis 会限制 Asynq 队列巡检脚本，查询单个队列概览失败时
			// 仍然返回静态队列名与零值指标，保证前端任务队列页可用。
			resp.Queues = append(resp.Queues, types.TaskQueueItem{
				Name: queue,
			})
			continue
		}
		resp.Queues = append(resp.Queues, types.TaskQueueItem{
			Name:        m.displayQueueName(info.Queue),
			Paused:      info.Paused,
			Size:        info.Size,
			Pending:     info.Pending,
			Active:      info.Active,
			Scheduled:   info.Scheduled,
			Retry:       info.Retry,
			Archived:    info.Archived,
			Completed:   info.Completed,
			Aggregating: info.Aggregating,
			Processed:   info.Processed,
			Failed:      info.Failed,
			LatencyMS:   info.Latency.Milliseconds(),
			MemoryUsage: info.MemoryUsage,
		})
	}
	servers, err := m.inspector.Servers()
	if err != nil {
		return resp, nil
	}
	resp.Servers = make([]types.TaskServerItem, 0, len(servers))
	for _, server := range servers {
		serverQueues, visible := m.visibleServerQueues(server.Queues)
		if !visible {
			// 共享 Redis 下只展示当前 app_id 队列对应的 worker。
			continue
		}
		resp.Servers = append(resp.Servers, types.TaskServerItem{
			ID:             server.ID,
			Host:           server.Host,
			PID:            server.PID,
			Status:         server.Status,
			Concurrency:    server.Concurrency,
			StrictPriority: server.StrictPriority,
			Queues:         serverQueues,
			StartedAt:      server.Started.Format(time.RFC3339),
		})
	}
	return resp, nil
}

// visibleServerQueues 过滤单个 worker 监听的队列，只返回当前站点可见的逻辑队列名。
// 共享 task redis 时，通过队列名前缀判断 worker 归属。
func (m *Manager) visibleServerQueues(queues map[string]int) (map[string]int, bool) {
	if len(queues) == 0 {
		return nil, false
	}
	result := make(map[string]int, len(queues))
	prefix := keys.TaskQueueNameScope()
	for queueName, weight := range queues {
		queueName = strings.TrimSpace(queueName)
		if queueName == "" || !strings.HasPrefix(queueName, prefix) {
			continue
		}
		result[keys.TrimTaskQueueName(queueName)] = weight
	}
	return result, len(result) > 0
}

// configuredQueueNames 从当前任务系统配置快照中提取静态队列名列表。
func (m *Manager) configuredQueueNames() []string {
	cfg := m.CurrentConfig()
	if len(cfg.Queues) == 0 {
		return nil
	}
	queueNames := make([]string, 0, len(cfg.Queues))
	for queueName := range cfg.Queues {
		queueName = strings.TrimSpace(queueName)
		if queueName == "" {
			continue
		}
		queueNames = append(queueNames, queueName)
	}
	return queueNames
}

// visibleQueueNames 返回当前站点需要展示的逻辑队列名列表。
// 共享 Redis 场景下按当前 app_id 过滤队列。
func (m *Manager) visibleQueueNames(queueNames []string) []string {
	if len(queueNames) == 0 {
		return nil
	}
	prefix := keys.TaskQueueNameScope()
	result := make([]string, 0, len(queueNames))
	for _, queueName := range queueNames {
		queueName = strings.TrimSpace(queueName)
		if queueName == "" || !strings.HasPrefix(queueName, prefix) {
			continue
		}
		result = append(result, keys.TrimTaskQueueName(queueName))
	}
	if len(result) > 0 {
		return result
	}
	return m.configuredQueueNames()
}

// PauseQueue 暂停某个队列的消费。
func (m *Manager) PauseQueue(ctx context.Context, queue string) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	_ = ctx
	return m.inspector.PauseQueue(m.namespacedQueueName(strings.TrimSpace(queue)))
}

// ResumeQueue 恢复某个队列的消费。
func (m *Manager) ResumeQueue(ctx context.Context, queue string) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	_ = ctx
	return m.inspector.UnpauseQueue(m.namespacedQueueName(strings.TrimSpace(queue)))
}

// RunTask 让 scheduled/retry/archived 任务立即转入 pending 队列执行。
func (m *Manager) RunTask(ctx context.Context, req *types.OperateTaskReq) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return errors.Errorf("任务操作请求不能为空")
	}
	if err := req.Validate(); err != nil {
		return errors.Tag(err)
	}
	internalQueue := m.namespacedQueueName(strings.TrimSpace(req.Queue))
	taskID := strings.TrimSpace(req.TaskID)
	info, err := m.inspector.GetTaskInfo(internalQueue, taskID)
	if err != nil {
		return errors.Tag(err)
	}
	if info != nil && info.State == asynq.TaskStateArchived {
		if err = m.prepareWorkflowArchivedTaskRerun(ctx, info); err != nil {
			return errors.Tag(err)
		}
	}
	if err = m.inspector.RunTask(internalQueue, taskID); err != nil {
		return errors.Tag(err)
	}
	if info != nil && info.State == asynq.TaskStateArchived {
		if err = m.persistTaskRecordForRerun(ctx, internalQueue, taskID); err != nil {
			return errors.Wrapf(err, "清理归档任务重跑缓存 TTL 失败 task_id=%s", taskID)
		}
	}
	return nil
}

// prepareWorkflowArchivedTaskRerun 在归档失败的工作流节点任务重跑前修复 Redis 节点状态。
// RunTask 前先清理归档失败节点状态，保证重跑成功后 DAG 能继续推进。
func (m *Manager) prepareWorkflowArchivedTaskRerun(ctx context.Context, info *asynq.TaskInfo) error {
	if m == nil || info == nil {
		return nil
	}
	meta := workflowTaskMetaFromTaskInfo(info)
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return nil
	}
	status, err := m.redis.HGet(ctx, m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode), "status").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "读取工作流节点状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if status == NodeStatusSuccess || status == NodeStatusSkipped {
		return nil
	}
	now := time.Now().Format(time.RFC3339)
	if _, err = workflowArchivedTaskRerunRepairScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), now, m.workflowRetention().Milliseconds()).Result(); err != nil {
		return errors.Wrapf(err, "修复归档工作流任务重跑状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	// failed 终态 key 必须同步移除，否则本次重跑若再次失败会因 SetNX 已存在而无法刷新工作流主记录错误摘要。
	if err = m.redis.Del(ctx, m.workflowFailedKey(meta.WorkflowID)).Err(); err != nil {
		return errors.Wrapf(err, "清理工作流失败终态标记失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	return m.updateWorkflowMeta(ctx, meta.WorkflowID, map[string]any{
		"status":       WorkflowStatusRunning,
		"errorMessage": "",
		"finishedAt":   "",
	})
}

// persistTaskRecordForRerun 移除任务详情 hash 的 TTL，避免已归档任务重跑后继续使用原过期时间。
func (m *Manager) persistTaskRecordForRerun(ctx context.Context, queue string, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	queue = strings.TrimSpace(queue)
	if m == nil || m.redis == nil || taskID == "" || queue == "" {
		return nil
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	return errors.Tag(m.redis.Persist(writeCtx, keys.TaskAsynqTaskHashKey(queue, taskID)).Err())
}

// DeleteTask 删除 pending/scheduled/retry/archived 状态的任务。
func (m *Manager) DeleteTask(ctx context.Context, req *types.OperateTaskReq) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	_ = ctx
	return m.inspector.DeleteTask(m.namespacedQueueName(strings.TrimSpace(req.Queue)), strings.TrimSpace(req.TaskID))
}

// EnqueueRegisteredTask 通过统一入口投递已注册任务类型，便于提供通用后台管理 API。
func (m *Manager) EnqueueRegisteredTask(ctx context.Context, req *types.EnqueueTaskReq) (*types.TaskEnqueueResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("任务请求不能为空")
	}
	taskType := strings.TrimSpace(req.TaskType)
	if !m.hasHandler(taskType) {
		return nil, ErrTaskTypeNotFound
	}
	options, err := m.taskOptionsFromRequest(req)
	if err != nil {
		return nil, errors.Tag(err)
	}
	info, err := m.enqueueTaskWithOptions(ctx, m.newTask(ctx, taskType, req.Payload, map[string]string{
		headerTaskName: taskTypeDisplayName(taskType),
	}), options)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp := &types.TaskEnqueueResp{
		TaskID:   info.ID,
		TaskType: taskType,
		Queue:    m.displayQueueName(info.Queue),
	}
	if !info.NextProcessAt.IsZero() {
		resp.ProcessAt = info.NextProcessAt.Format(time.RFC3339)
	}
	return resp, nil
}

// EnqueueCacheRefresh 投递缓存刷新请求任务。
// 多个短时间内连续到来的刷新请求会通过 Group 聚合为一个批量刷新任务。
func (m *Manager) EnqueueCacheRefresh(ctx context.Context, operation string, cacheKeys []string) error {
	if !m.IsEnabled() {
		return ErrTaskQueueDisabled
	}
	targets := normalizeStrings(cacheKeys)
	if len(targets) == 0 {
		return nil
	}
	body, err := json.Marshal(CacheRefreshPayload{
		Operation: strings.TrimSpace(operation),
		Targets:   targets,
	})
	if err != nil {
		return errors.Tag(err)
	}
	task := m.newTask(ctx, TypeCacheRefreshRequest, body, map[string]string{
		headerTaskName:   taskTypeDisplayName(TypeCacheRefreshRequest),
		headerTaskSource: WorkflowSourceInternal,
	})
	_, err = m.client.EnqueueContext(ctx, task,
		asynq.Queue(m.namespacedQueueName(QueueMaintenance)),
		asynq.Group(m.namespacedGroup(GroupCacheRefresh)),
		asynq.Timeout(2*time.Minute),
		asynq.MaxRetry(max(m.CurrentConfig().DefaultRetry, 3)),
		asynq.Retention(m.completedRetention()),
		asynq.Unique(15*time.Second),
	)
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return errors.Tag(err)
}

// aggregateTasks 根据任务分组查找聚合器，并把同组短任务合并成批任务。
func (m *Manager) aggregateTasks(group string, tasks []*asynq.Task) *asynq.Task {
	m.mu.RLock()
	aggregator := m.aggregates[strings.TrimSpace(group)]
	m.mu.RUnlock()
	if aggregator == nil {
		return nil
	}
	return aggregator(tasks)
}

// handleTaskError 统一记录任务失败日志，并在终态失败时回写工作流节点状态。
func (m *Manager) handleTaskError(ctx context.Context, task *asynq.Task, err error) {
	// ErrorHandler 拿到的上下文不保证包含业务中间件补齐后的链路字段，
	// 这里统一从任务头重建一次请求元数据，确保失败日志也能稳定带出 trace/workflow 信息。
	ctx = m.enrichTaskContext(ctx, task)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	taskID, _ := asynq.GetTaskID(ctx)
	meta := workflowTaskMetaFromTask(task)
	fields := append(loggerx.FieldsFromContext(ctx),
		logx.Field("retried", retried),
		logx.Field("max_retry", maxRetry),
	)
	fields = append(fields, taskLogFields(task)...)
	fields = append(fields, taskStatsLogFields(m.readTaskRuntime(ctx, taskID).ExecutionTrace)...)
	loggerx.Errorw(ctx, "任务处理失败", err, fields...)
	if taskWillArchive(ctx, err) {
		if markErr := m.markTaskFailureWithFinalContext(ctx, meta, err); markErr != nil {
			loggerx.Errorw(ctx, "任务失败状态回写失败", markErr, fields...)
		}
		m.runTaskFinalFailureHooks(ctx, task, meta, err, fields)
	}
}

// markTaskFailureWithFinalContext 使用短后台 ctx 回写工作流失败状态。
// 该方法只负责终态兜底写 Redis，保留原业务 ctx 的链路字段，但不继承其取消信号。
func (m *Manager) markTaskFailureWithFinalContext(ctx context.Context, meta WorkflowTaskMeta, err error) error {
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	_, markErr := m.markTaskFailure(writeCtx, meta, err)
	return errors.Tag(markErr)
}

// runTaskFinalFailureHooks 在任务终态失败后触发业务清理钩子。
// 这里使用短后台 ctx，避免业务 ctx 超时后租约释放、失败兜底状态等收尾动作继续复用已取消上下文。
func (m *Manager) runTaskFinalFailureHooks(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error, fields []logx.LogField) {
	m.mu.RLock()
	hooks := append([]TaskFinalFailureHook(nil), m.finalFailureHooks...)
	m.mu.RUnlock()
	if len(hooks) == 0 {
		return
	}
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hookCtx, cancel := m.taskFinalWriteContext(ctx)
		if err := hook(hookCtx, task, meta, runErr); err != nil {
			loggerx.Errorw(hookCtx, "任务终态失败清理钩子执行失败", err, fields...)
		}
		cancel()
	}
}

// enrichTaskContext 从 Asynq 任务头和运行时上下文中重建统一日志字段。
// 任务失败回调和正常处理链都复用该方法，保证 trace、task、workflow、locale、user 信息输出一致。
func (m *Manager) enrichTaskContext(ctx context.Context, task *asynq.Task) context.Context {
	// 兜底补一个基础 context，避免极端场景传入 nil 导致后续链路字段无法写入。
	if ctx == nil {
		ctx = context.Background()
	}
	// 统一初始化请求元数据，保证后续日志字段都写入同一份上下文容器。
	ctx, _ = requestctx.New(ctx)
	if task == nil {
		return loggerx.BindContext(ctx)
	}

	// 先把任务头中的远端 trace 上下文提取出来，便于失败日志也能挂回原链路。
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(task.Headers()))
	queue, _ := asynq.GetQueueName(ctx)
	taskID, _ := asynq.GetTaskID(ctx)
	requestctx.SetTask(ctx, taskID, task.Type(), queue)
	requestctx.SetTaskName(ctx, taskNameFromTask(task))
	requestctx.SetRequest(ctx, "TASK", task.Type(), "")
	requestctx.SetLocale(ctx, task.Headers()[headerLocale])
	if uid, convErr := strconv.Atoi(task.Headers()[headerUserID]); convErr == nil {
		requestctx.SetUser(ctx, uid, task.Headers()[headerUserName], "")
	}
	if wfMeta := workflowTaskMetaFromTask(task); wfMeta.WorkflowID != "" {
		requestctx.SetWorkflow(ctx, wfMeta.WorkflowID, wfMeta.WorkflowName, wfMeta.WorkflowNode, wfMeta.ShardIndex, wfMeta.ShardTotal)
	}
	if mode := taskPayloadString(task, "mode"); mode != "" {
		requestctx.SetMode(ctx, mode)
	}

	// 如果当前上下文已经存在 span，则优先回写当前 span；否则回写从任务头透传过来的父级 span。
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		requestctx.SetTrace(ctx, spanCtx.TraceID().String(), spanCtx.SpanID().String())
	}
	return loggerx.BindContext(ctx)
}

// traceAndLogMiddleware 为所有任务处理链统一补充 trace、请求上下文和结构化日志。
func (m *Manager) traceAndLogMiddleware() asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			// 先把任务上下文基础字段补齐，保证后续成功/失败日志都复用同一套元数据。
			ctx = m.enrichTaskContext(ctx, task)
			ctx, _ = taskstats.WithTracker(ctx, taskNameFromTask(task))
			meta := requestctx.FromContext(ctx)

			// 开始 consumer span，并把 trace_id/span_id 回写到上下文供日志复用。
			ctx, span := m.tracer.Start(ctx, task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
			queue, _ := asynq.GetQueueName(ctx)
			begin := time.Now()
			taskID := ""
			if meta != nil {
				taskID = meta.TaskID
			}
			m.recordTaskRuntimeStart(ctx, taskID, begin)
			sc := span.SpanContext()
			requestctx.SetTrace(ctx, sc.TraceID().String(), sc.SpanID().String())
			ctx = loggerx.BindContext(ctx)
			loggerx.Infow(ctx, taskLogMessage("任务 开始执行", requestctx.FromContext(ctx)), taskLogFields(task)...)

			// 执行业务 handler，并在工作流状态回写完成后统一确认最终任务结果。
			err := next.ProcessTask(ctx, task)
			requestctx.SetLatency(ctx, time.Since(begin))
			statsSnapshot := taskstats.SnapshotFromContext(ctx)
			statsWriteCtx, statsWriteCancel := m.taskFinalWriteContext(ctx)
			statsErr := m.recordWorkflowTaskStats(statsWriteCtx, workflowTaskMetaFromTask(task), statsSnapshot)
			statsWriteCancel()
			if statsErr != nil {
				loggerx.Errorw(ctx, "工作流节点处理量记录失败", statsErr, taskLogFields(task)...)
			}
			if err == nil {
				// 成功则回写工作流节点状态，推动 DAG 继续执行后继节点。
				if markErr := m.markTaskSuccess(ctx, workflowTaskMetaFromTask(task)); markErr != nil {
					err = markErr
				}
			}
			m.recordTaskRuntimeFinish(ctx, taskID, begin, err, statsSnapshot)
			recordTaskExecutionMetrics(queue, task.Type(), err, time.Since(begin), statsSnapshot)
			if err != nil {
				requestctx.SetError(ctx, err, err.Error())
				requestctx.SetResponse(ctx, 500, 2, err.Error(), err.Error())
			} else {
				requestctx.SetResponse(ctx, 200, 1, "成功", "")
			}
			fields := []logx.LogField{logx.Field("latency_ms", requestctx.FromContext(ctx).LatencyMS)}
			fields = append(fields, taskLogFields(task)...)
			fields = append(fields, taskStatsLogFields(statsSnapshot)...)
			if err != nil {
				span.SetStatus(otelcodes.Error, err.Error())
				span.RecordError(err)
			} else {
				m.writeTaskResult(ctx, task, requestctx.FromContext(ctx), begin, statsSnapshot)
				loggerx.Infow(ctx, taskLogMessage("任务 执行完成", requestctx.FromContext(ctx)), fields...)
				span.SetStatus(otelcodes.Ok, "ok")
			}
			m.expireFinalTaskRecord(ctx, queue, taskID, err)

			// 统一写入 span attributes/status，方便按 queue/task_type/workflow 维度检索。
			span.SetAttributes(
				attribute.String("messaging.system", "redis"),
				attribute.String("messaging.destination", queue),
				attribute.String("messaging.operation", "process"),
				attribute.String("app.task_type", task.Type()),
			)
			if meta != nil && meta.WorkflowID != "" {
				span.SetAttributes(
					attribute.String("app.workflow_id", meta.WorkflowID),
					attribute.String("app.workflow_name", meta.WorkflowName),
					attribute.String("app.workflow_node", meta.WorkflowNode),
				)
			}
			span.SetAttributes(loggerx.TraceAttributesFromMeta(requestctx.FromContext(ctx))...)
			span.End()
			if err != nil {
				return errors.Tag(err)
			}
			return nil
		})
	}
}

// newTask 把当前上下文中的链路、语言和用户信息注入 Asynq 任务头。
func (m *Manager) newTask(ctx context.Context, taskType string, payload []byte, extraHeaders map[string]string) *asynq.Task {
	headers := make(map[string]string, len(extraHeaders)+6)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
	if meta := requestctx.FromContext(ctx); meta != nil {
		if meta.Locale != "" {
			headers[headerLocale] = meta.Locale
		}
		if meta.UserID > 0 {
			headers[headerUserID] = strconv.Itoa(meta.UserID)
		}
		if meta.UserName != "" {
			headers[headerUserName] = meta.UserName
		}
	}
	if appID := m.appNamespace(); appID != "" {
		headers[headerAppID] = appID
	}
	for key, value := range extraHeaders {
		if value != "" {
			headers[key] = value
		}
	}
	return asynq.NewTaskWithHeaders(taskType, payload, headers)
}

// workflowTaskMetaFromTask 从任务头和 payload 中提取工作流元数据。
func workflowTaskMetaFromTask(task *asynq.Task) WorkflowTaskMeta {
	if task == nil {
		return WorkflowTaskMeta{}
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   helper.FirstNonEmptyString(task.Headers()[headerWorkflowID]),
		WorkflowName: helper.FirstNonEmptyString(task.Headers()[headerWorkflowName]),
		WorkflowNode: helper.FirstNonEmptyString(task.Headers()[headerWorkflowNode]),
		ShardIndex:   toInt(task.Headers()[headerShardIndex]),
		ShardTotal:   toInt(task.Headers()[headerShardTotal]),
	}
	if len(task.Payload()) == 0 {
		return meta
	}
	_ = json.Unmarshal(task.Payload(), &meta)
	if meta.WorkflowID == "" {
		meta.WorkflowID = task.Headers()[headerWorkflowID]
	}
	if meta.WorkflowName == "" {
		meta.WorkflowName = task.Headers()[headerWorkflowName]
	}
	if meta.WorkflowNode == "" {
		meta.WorkflowNode = task.Headers()[headerWorkflowNode]
	}
	if meta.ShardTotal == 0 {
		meta.ShardTotal = toInt(task.Headers()[headerShardTotal])
	}
	return meta
}

// workflowTaskMetaFromTaskInfo 从 Asynq 任务详情中提取工作流元数据。
// 立即执行从 TaskInfo 中复原 workflowID、node 和 shardIndex。
func workflowTaskMetaFromTaskInfo(info *asynq.TaskInfo) WorkflowTaskMeta {
	if info == nil {
		return WorkflowTaskMeta{}
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   helper.FirstNonEmptyString(info.Headers[headerWorkflowID]),
		WorkflowName: helper.FirstNonEmptyString(info.Headers[headerWorkflowName]),
		WorkflowNode: helper.FirstNonEmptyString(info.Headers[headerWorkflowNode]),
		ShardIndex:   toInt(info.Headers[headerShardIndex]),
		ShardTotal:   toInt(info.Headers[headerShardTotal]),
	}
	if len(info.Payload) > 0 {
		_ = json.Unmarshal(info.Payload, &meta)
	}
	if meta.WorkflowID == "" {
		meta.WorkflowID = info.Headers[headerWorkflowID]
	}
	if meta.WorkflowName == "" {
		meta.WorkflowName = info.Headers[headerWorkflowName]
	}
	if meta.WorkflowNode == "" {
		meta.WorkflowNode = info.Headers[headerWorkflowNode]
	}
	if meta.ShardTotal == 0 {
		meta.ShardTotal = toInt(info.Headers[headerShardTotal])
	}
	return meta
}

// taskPayloadString 从任务 payload 中读取指定首层字段，主要用于 mode 等日志检索字段补齐。
func taskPayloadString(task *asynq.Task, key string) string {
	if task == nil || len(task.Payload()) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(task.Payload(), &payloadMap); err != nil {
		return ""
	}
	return strings.TrimSpace(anyToString(payloadMap[key]))
}

// periodicConfigs 把系统配置和插件注册的周期任务转换成 Asynq 调度配置。
func (m *Manager) periodicConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	items := m.allPeriodicTasks()
	configs := make([]*asynq.PeriodicTaskConfig, 0, len(items))
	for idx, item := range items {
		item, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr != nil {
			m.notifyPeriodicTaskConfigInvalid(idx, item, invalidErr.Error())
			continue
		}
		payload := WorkflowTriggerPayload{
			WorkflowName:     item.Workflow,
			Queue:            item.Queue,
			Targets:          item.Targets,
			ShardTotal:       max(item.ShardTotal, 1),
			GrayPercent:      item.GrayPercent,
			Source:           WorkflowSourcePeriodic,
			PeriodicName:     periodicTaskDisplayName(item),
			TimeoutSeconds:   item.TimeoutSeconds,
			UniqueKey:        item.UniqueKey,
			UniqueTTLSeconds: item.UniqueTTLSeconds,
		}
		var retryOverride *int
		if item.Retry > 0 {
			retryOverride = new(item.Retry)
			payload.Retry = retryOverride
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Tag(err)
		}
		task := asynq.NewTaskWithHeaders(TypeWorkflowTrigger, body, map[string]string{
			headerTaskName:     periodicTaskDisplayName(item),
			headerTaskSource:   WorkflowSourcePeriodic,
			HeaderPeriodicName: periodicTaskDisplayName(item),
			headerWorkflowName: item.Workflow,
		})
		opts := []asynq.Option{
			asynq.Queue(m.namespacedQueueName(helper.FirstNonEmptyString(item.Queue, m.defaultWorkflowQueue()))),
			asynq.MaxRetry(m.defaultRetry(retryOverride)),
		}
		if retention := m.completedRetention(); retention > 0 {
			opts = append(opts, asynq.Retention(retention))
		}
		if item.TimeoutSeconds > 0 {
			opts = append(opts, asynq.Timeout(time.Duration(item.TimeoutSeconds)*time.Second))
		}
		if item.Deadline != "" {
			if deadline, parseErr := time.Parse(time.RFC3339, item.Deadline); parseErr == nil {
				opts = append(opts, asynq.Deadline(deadline))
			}
		}
		if item.UniqueKey != "" && item.UniqueTTLSeconds > 0 {
			opts = append(opts, asynq.Unique(time.Duration(item.UniqueTTLSeconds)*time.Second))
		}
		configs = append(configs, &asynq.PeriodicTaskConfig{
			Cronspec: item.Cron,
			Task:     task,
			Opts:     opts,
		})
	}
	return configs, nil
}

// ValidatePeriodicTaskDefinitions 检查周期任务引用的工作流是否已在当前进程注册。
// 滚动发布时只记录未知工作流，调度同步阶段继续跳过无效配置。
func (m *Manager) ValidatePeriodicTaskDefinitions() error {
	if m == nil || !m.IsEnabled() {
		return nil
	}
	items := m.allPeriodicTasks()
	for idx, item := range items {
		normalized, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr == nil {
			continue
		}
		m.notifyPeriodicTaskConfigInvalid(idx, normalized, invalidErr.Error())
	}
	return nil
}

// validatePeriodicTaskConfig 校验并归一化单个周期任务配置，异常时仅跳过当前任务。
func (m *Manager) validatePeriodicTaskConfig(item config.TaskPeriodicConfig) (config.TaskPeriodicConfig, error) {
	cronspec, err := periodicTaskCronspec(item)
	if err != nil {
		return item, errors.Tag(err)
	}
	if err = validatePeriodicCronspec(cronspec); err != nil {
		return item, errors.Tag(err)
	}
	item.Cron = cronspec
	item.Name = strings.TrimSpace(item.Name)
	item.Workflow = strings.TrimSpace(item.Workflow)
	if item.Workflow == "" {
		return item, errors.Errorf("周期任务 workflow 不能为空")
	}
	if _, err = m.workflowDefinition(item.Workflow); err != nil {
		return item, errors.Tag(err)
	}
	item.Queue = strings.TrimSpace(item.Queue)
	item.Targets = normalizeStrings(item.Targets)
	item.UniqueKey = strings.TrimSpace(item.UniqueKey)
	item.Deadline = strings.TrimSpace(item.Deadline)
	if item.ShardTotal < 0 {
		return item, errors.Errorf("周期任务 shard_total 不能小于 0")
	}
	if item.GrayPercent < 0 || item.GrayPercent > 100 {
		return item, errors.Errorf("周期任务 gray_percent 必须在 0 到 100 之间")
	}
	if item.Retry < 0 {
		return item, errors.Errorf("周期任务 retry 不能小于 0")
	}
	if item.TimeoutSeconds < 0 {
		return item, errors.Errorf("周期任务 timeout_seconds 不能小于 0")
	}
	if item.UniqueTTLSeconds < 0 {
		return item, errors.Errorf("周期任务 unique_ttl_seconds 不能小于 0")
	}
	if item.Deadline != "" {
		if _, err = time.Parse(time.RFC3339, item.Deadline); err != nil {
			return item, errors.Wrap(err, "解析周期任务 deadline 失败")
		}
	}
	if item.UniqueKey != "" && item.UniqueTTLSeconds == 0 {
		// 周期任务配置了 unique_key 时默认复用任务系统全局去重 TTL，避免遗漏 TTL 后静默失去防重效果。
		item.UniqueTTLSeconds = int(m.uniqueTTL(nil).Seconds())
	}
	return item, nil
}

// notifyPeriodicTaskConfigInvalid 记录周期任务配置异常，保证错误配置可被日志检索和告警系统发现。
func (m *Manager) notifyPeriodicTaskConfigInvalid(index int, item config.TaskPeriodicConfig, reason string) {
	fields := []logx.LogField{
		logx.Field("index", index),
		logx.Field("task_name", strings.TrimSpace(item.Name)),
		logx.Field("cron", strings.TrimSpace(item.Cron)),
		logx.Field("every_seconds", item.EverySeconds),
		logx.Field("workflow", strings.TrimSpace(item.Workflow)),
		logx.Field("task_queue", strings.TrimSpace(item.Queue)),
		logx.Field("unique_key", strings.TrimSpace(item.UniqueKey)),
		logx.Field("failure_reason", strings.TrimSpace(reason)),
	}
	loggerx.ErrorTextw(context.Background(), "周期任务 配置无效", reason, fields...)
	m.runPeriodicConfigInvalidHooks(index, item, reason, fields)
}

// runPeriodicConfigInvalidHooks 推送周期任务配置异常告警；同一异常在限频窗口内只通知一次。
func (m *Manager) runPeriodicConfigInvalidHooks(index int, item config.TaskPeriodicConfig, reason string, fields []logx.LogField) {
	if m == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	now := time.Now()
	key := periodicConfigInvalidAlertKey(index, item, reason)
	m.mu.Lock()
	if len(m.periodicConfigHooks) == 0 {
		m.mu.Unlock()
		return
	}
	if m.periodicConfigAlertedMap == nil {
		m.periodicConfigAlertedMap = make(map[string]periodicConfigAlertState)
	}
	state := m.periodicConfigAlertedMap[key]
	if !state.LastSentAt.IsZero() && now.Sub(state.LastSentAt) < periodicConfigAlertTTL {
		state.SuppressedCount++
		m.periodicConfigAlertedMap[key] = state
		m.mu.Unlock()
		return
	}
	triggerCount := state.SuppressedCount + 1
	m.periodicConfigAlertedMap[key] = periodicConfigAlertState{LastSentAt: now}
	hooks := append([]PeriodicConfigInvalidHook(nil), m.periodicConfigHooks...)
	m.mu.Unlock()

	report := PeriodicConfigInvalidReport{
		Index:        index,
		Task:         item,
		Reason:       reason,
		OccurredAt:   now,
		TriggerCount: triggerCount,
	}
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(context.Background(), report); err != nil {
			loggerx.Errorw(context.Background(), "周期任务配置异常告警钩子执行失败", err, fields...)
		}
	}
}

// periodicConfigInvalidAlertKey 生成周期任务异常告警指纹，用于对重复同步日志限频。
func periodicConfigInvalidAlertKey(index int, item config.TaskPeriodicConfig, reason string) string {
	return strings.Join([]string{
		strconv.Itoa(index),
		strings.TrimSpace(item.Name),
		strings.TrimSpace(item.Workflow),
		strings.TrimSpace(item.Cron),
		strconv.Itoa(item.EverySeconds),
		strings.TrimSpace(item.Queue),
		strings.TrimSpace(item.UniqueKey),
		strings.TrimSpace(reason),
	}, "\x00")
}

// workflowMetaKey 返回工作流主记录在 Redis 中的 key。
func (m *Manager) workflowMetaKey(workflowID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowMetaKey(workflowID)
}

// workflowNodesKey 返回工作流节点集合在 Redis 中的 key。
func (m *Manager) workflowNodesKey(workflowID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowNodesKey(workflowID)
}

// workflowNodeKey 返回单个工作流节点状态记录的 Redis key。
func (m *Manager) workflowNodeKey(workflowID, nodeName string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowNodeKey(workflowID, nodeName)
}

// workflowNodeScheduledKey 返回节点调度去重标记的 Redis key。
func (m *Manager) workflowNodeScheduledKey(workflowID, nodeName string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowNodeScheduledKey(workflowID, nodeName)
}

// workflowNodeFinalizedKey 返回节点完成收口标记的 Redis key。
func (m *Manager) workflowNodeFinalizedKey(workflowID, nodeName string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowNodeFinalizedKey(workflowID, nodeName)
}

// workflowCompletedKey 返回工作流完成标记的 Redis key。
func (m *Manager) workflowCompletedKey(workflowID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowCompletedKey(workflowID)
}

// workflowFailedKey 返回工作流失败标记的 Redis key。
func (m *Manager) workflowFailedKey(workflowID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowFailedKey(workflowID)
}

// workflowUniqueKey 返回工作流幂等占位记录的 Redis key。
func (m *Manager) workflowUniqueKey(name, key string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowUniqueKey(name, key)
}

// workflowUniqueLockKey 返回工作流幂等预占时使用的短锁 Redis key。
func (m *Manager) workflowUniqueLockKey(name, key string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskWorkflowUniqueLockKey(name, key)
}

// enqueueTaskWithOptions 把统一任务选项转换为 Asynq 投递参数并执行入队。
func (m *Manager) enqueueTaskWithOptions(ctx context.Context, task *asynq.Task, options *svc.TaskOptions) (*asynq.TaskInfo, error) {
	if task == nil {
		return nil, errors.Errorf("任务不能为空")
	}
	if options == nil {
		options = &svc.TaskOptions{}
	}
	if options.Delay > 0 && options.ProcessAt != nil {
		return nil, errors.Errorf("任务 delay 和 processAt 不能同时设置")
	}
	var asynqOpts []asynq.Option
	asynqOpts = append(asynqOpts, asynq.Queue(m.namespacedQueueName(helper.FirstNonEmptyString(options.Queue, m.defaultWorkflowQueue()))))
	if options.Retry > 0 {
		asynqOpts = append(asynqOpts, asynq.MaxRetry(options.Retry))
	}
	if options.Timeout > 0 {
		asynqOpts = append(asynqOpts, asynq.Timeout(options.Timeout))
	}
	if options.Delay > 0 {
		asynqOpts = append(asynqOpts, asynq.ProcessIn(options.Delay))
	}
	if options.ProcessAt != nil && !options.ProcessAt.IsZero() {
		asynqOpts = append(asynqOpts, asynq.ProcessAt(*options.ProcessAt))
	}
	if options.Deadline != nil && !options.Deadline.IsZero() {
		asynqOpts = append(asynqOpts, asynq.Deadline(*options.Deadline))
	}
	if options.UniqueTTL > 0 {
		asynqOpts = append(asynqOpts, asynq.Unique(options.UniqueTTL))
	}
	if group := strings.TrimSpace(options.Group); group != "" {
		asynqOpts = append(asynqOpts, asynq.Group(m.namespacedGroup(group)))
	}
	if retention := m.completedRetention(); retention > 0 {
		asynqOpts = append(asynqOpts, asynq.Retention(retention))
	}
	return m.client.EnqueueContext(ctx, task, asynqOpts...)
}

// taskOptionsFromRequest 把通用投递请求转换为内部任务选项结构。
func (m *Manager) taskOptionsFromRequest(req *types.EnqueueTaskReq) (*svc.TaskOptions, error) {
	options := &svc.TaskOptions{
		Queue: strings.TrimSpace(req.Queue),
		Group: strings.TrimSpace(req.Group),
	}
	if req.Retry != nil {
		options.Retry = *req.Retry
	}
	if req.TimeoutSeconds != nil {
		options.Timeout = time.Duration(*req.TimeoutSeconds) * time.Second
	}
	if req.ProcessInSeconds != nil {
		options.Delay = time.Duration(*req.ProcessInSeconds) * time.Second
	}
	if req.UniqueTTLSeconds != nil {
		options.UniqueTTL = time.Duration(*req.UniqueTTLSeconds) * time.Second
	}
	if req.ProcessAt != "" {
		processAt, err := time.Parse(time.RFC3339, req.ProcessAt)
		if err != nil {
			return nil, errors.Tag(err)
		}
		options.ProcessAt = &processAt
	}
	if req.Deadline != "" {
		deadline, err := time.Parse(time.RFC3339, req.Deadline)
		if err != nil {
			return nil, errors.Tag(err)
		}
		options.Deadline = &deadline
	}
	return options, nil
}

// hasHandler 判断指定任务类型是否已经注册处理器。
func (m *Manager) hasHandler(taskType string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.handlers[strings.TrimSpace(taskType)]
	return ok
}
