package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	redislock "admin/internal/infra/redsync"
	"admin/internal/jobs/archive"
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

	headerLocale       = "x-app-locale"
	headerUserID       = "x-app-user-id"
	headerUserName     = "x-app-user-name"
	headerTaskName     = "x-app-task-name"
	headerTaskSource   = "x-app-task-source"
	HeaderPeriodicName = "x-app-periodic-name" // 周期任务原始名称
	headerWorkflowID   = "x-app-workflow-id"
	headerWorkflowName = "x-app-workflow-name"
	headerWorkflowNode = "x-app-workflow-node"
	headerShardIndex   = "x-app-workflow-shard-index"
	headerShardTotal   = "x-app-workflow-shard-total"
	headerAppID        = "x-app-id"

	taskSearchFieldTaskName     = "taskName"     // 任务结果或兼容 payload 中的展示名称字段
	taskSearchFieldTaskType     = "taskType"     // 任务结果中回写的任务类型字段
	taskSearchFieldWorkflowID   = "workflowId"   // 工作流实例 ID 字段，兼容 payload/result 兜底匹配
	taskSearchFieldWorkflowName = "workflowName" // 工作流名称字段，周期任务排查常用该字段定位入口任务
	taskSearchFieldWorkflowNode = "workflowNode" // 工作流节点字段，用于节点任务按脚本名检索
	taskSearchFieldTaskSource   = "taskSource"   // 任务来源字段，用于区分 api/periodic/internal 等触发来源
	taskSearchFieldMode         = "mode"         // 用户标签等任务的运行模式字段，用于保留 full/delta 等排查入口
	taskSearchFieldUniqueKey    = "uniqueKey"    // 工作流或任务幂等键字段，便于按周期 unique_key 反查任务

	taskExecutionStatusSuccess = "success" // 普通任务执行结果成功状态

	taskFinalWriteTimeout  = 5 * time.Second // 任务收尾写 Redis 的短超时，避免业务 ctx 超时后失败状态无法落库
	taskListFilterPageSize = 100             // 任务列表二次过滤单批读取量，和接口最大页大小保持一致
	taskListFilterMaxPages = 50              // 任务列表二次过滤最大扫描页数，避免无索引历史查询拖垮 Redis
	taskRecordTTLBuffer    = time.Hour       // 终态任务 hash 额外保留窗口，避免清理边界抖动影响列表查询

	taskArchivedRetentionDefaultSeconds = 7 * 86400       // 归档失败任务默认保留 7 天
	taskArchivedCleanupInterval         = 5 * time.Minute // 归档失败任务过期清理间隔
	taskArchivedCleanupBatchSize        = 500             // 单队列单轮最多清理数量，避免 Redis Lua 长时间阻塞
	taskArchivedCleanupMaxBatches       = 5               // 单队列单轮最多清理批次数，兼顾历史积压追赶和 Redis 压力
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

// periodicNextRun 表示一个有效周期任务的下一次触发摘要。
type periodicNextRun struct {
	PeriodicName  string // 周期任务展示名，对应 x-app-periodic-name
	WorkflowName  string // 关联工作流名称
	NextProcessAt string // 下一次触发时间，RFC3339
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

// taskRuntimeRecord 表示任务运行时耗时快照。
type taskRuntimeRecord struct {
	StartedAt      string              // 开始执行时间
	FinishedAt     string              // 完成执行时间
	DurationMS     int64               // 执行耗时，毫秒
	ExecutionTrace *taskstats.Snapshot // 本次任务处理量统计摘要
}

// GroupAggregator 定义单个聚合分组的批处理器。
// 同一个 group 内的多条任务会在 worker 聚合窗口结束后交给对应聚合器合并成一个批任务。
type GroupAggregator func(tasks []*asynq.Task) *asynq.Task

// TaskFinalFailureHook 定义任务进入终态失败后的业务清理钩子。
// 钩子只在任务无自动重试机会后触发，用于释放业务侧资源。
type TaskFinalFailureHook func(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, err error) error

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

	lifecycleMu       sync.Mutex                     // 保护 Worker / Scheduler 启停状态，避免并发启动或停止产生竞态
	schedulerStatusMu sync.Mutex                     // 保护调度器状态快照更新，避免多个后台协程并发覆盖
	archivedCleanStop context.CancelFunc             // 停止归档失败任务过期清理协程
	mu                sync.RWMutex                   // 保护工作流定义和周期任务配置的并发读写
	workflows         map[string]*WorkflowDefinition // 已注册的工作流定义集合
	periodic          []config.TaskPeriodicConfig    // 通过配置或插件补充的周期任务列表
	handlers          map[string]struct{}            // 已注册的任务类型集合，用于限制通用任务投递
	aggregates        map[string]GroupAggregator     // 已注册的分组聚合器
	finalFailureHooks []TaskFinalFailureHook         // 任务终态失败后的业务清理钩子
}

// New 创建任务管理器。
func New(cfg config.TaskQueueConfig, client redis.UniversalClient) *Manager {
	if !cfg.Enabled || client == nil {
		return nil
	}
	if keys.NormalizeAppID(cfg.AppID) == "" {
		return nil
	}
	m := &Manager{
		redis:      client,
		client:     asynq.NewClientFromRedisClient(client),
		inspector:  asynq.NewInspectorFromRedisClient(client),
		mux:        asynq.NewServeMux(),
		logger:     asynqLogger{},
		tracer:     otel.Tracer("admin/task"),
		instance:   uuid.NewString(),
		workflows:  make(map[string]*WorkflowDefinition),
		periodic:   make([]config.TaskPeriodicConfig, 0),
		handlers:   make(map[string]struct{}),
		aggregates: make(map[string]GroupAggregator),
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
	appID := keys.NormalizeAppID(m.CurrentConfig().AppID)
	if appID == "" {
		return ""
	}
	return appID
}

// namespacedQueueName 返回底层 Asynq 实际使用的队列名。
// 对外接口仍暴露逻辑队列名，内部统一追加站点前缀，避免多站点共用同一套 task redis 时互相消费任务。
func (m *Manager) namespacedQueueName(queue string) string {
	queue = strings.TrimSpace(queue)
	if queue == "" {
		return ""
	}
	return keys.TaskQueueName(m.appNamespace(), queue)
}

// displayQueueName 把底层带站点前缀的队列名还原成对外展示的逻辑队列名。
func (m *Manager) displayQueueName(queue string) string {
	queue = strings.TrimSpace(queue)
	if queue == "" {
		return ""
	}
	return keys.TrimTaskQueueName(queue)
}

// workflowKeyPrefix 返回当前站点在 Redis 中的工作流 key 前缀。
func (m *Manager) workflowKeyPrefix() string {
	return keys.TaskWorkflowPrefix(m.appNamespace())
}

// namespacedGroup 返回当前站点下聚合任务使用的分组名。
func (m *Manager) namespacedGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	return keys.TaskQueueName(m.appNamespace(), group)
}

// displayGroupName 把底层聚合分组名还原成对外展示名称。
func (m *Manager) displayGroupName(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	return keys.TrimTaskQueueName(group)
}

// namespacedUniqueKey 返回当前站点下的任务去重键。
func (m *Manager) namespacedUniqueKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return keys.TaskQueueName(m.appNamespace(), key)
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
	return m != nil && m.client != nil && m.redis != nil
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

// taskTypeDescription 返回任务类型中文说明。
func taskTypeDescription(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return "工作流触发入口任务，由总控台触发工作流时自动入队"
	case taskType == TypeWorkflowNoop:
		return "工作流空节点任务，仅用于 DAG 收尾和依赖串联"
	case taskType == TypeCacheRefreshRequest:
		return "缓存刷新请求任务，用于把短时间内多次刷新请求聚合"
	case taskType == TypeCacheRefreshBatch:
		return "缓存批量刷新任务，真正执行缓存刷新逻辑"
	case taskType == archive.TaskTypeExecute:
		return "通用归档执行任务，负责按区间批量写历史表、校验并删除热表"
	case taskType == "admin:export":
		return "管理员列表异步导出任务，生成 Excel 并回写进度"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "用户标签工作流节点任务，用于用户标签重建、补算与同步"
	default:
		return "已注册任务处理器"
	}
}

// taskTypeDisplayName 返回任务类型的人类可读名称，优先用于日志和管理后台展示。
func taskTypeDisplayName(taskType string) string {
	description := strings.TrimSpace(taskTypeDescription(strings.TrimSpace(taskType)))
	if description == "" || description == "已注册任务处理器" {
		return strings.TrimSpace(taskType)
	}
	return description
}

// workflowTriggerTaskName 返回工作流触发入口任务的展示名称。
func workflowTriggerTaskName(workflowName string, periodicName string) string {
	periodicName = strings.TrimSpace(periodicName)
	if periodicName != "" {
		return fmt.Sprintf("周期任务触发:%s", periodicName)
	}
	workflowName = strings.TrimSpace(workflowName)
	if workflowName != "" {
		return fmt.Sprintf("工作流触发:%s", workflowName)
	}
	return "工作流触发"
}

// workflowNodeTaskName 返回工作流节点任务的展示名称，便于日志直接识别当前跑的是哪个脚本节点。
func workflowNodeTaskName(workflowName string, workflowNode string, taskType string) string {
	workflowName = strings.TrimSpace(workflowName)
	workflowNode = strings.TrimSpace(workflowNode)
	if workflowName != "" && workflowNode != "" {
		return fmt.Sprintf("%s/%s", workflowName, workflowNode)
	}
	if workflowNode != "" {
		return fmt.Sprintf("%s/%s", strings.TrimSpace(taskType), workflowNode)
	}
	return taskTypeDisplayName(taskType)
}

// periodicTaskDisplayName 返回周期任务触发入口的展示名称。
func periodicTaskDisplayName(item config.TaskPeriodicConfig) string {
	return workflowTriggerTaskName(strings.TrimSpace(item.Workflow), strings.TrimSpace(item.Name))
}

// taskNameFromTask 从任务头、工作流元数据和任务类型中推导稳定的任务名称。
func taskNameFromTask(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	if taskName := strings.TrimSpace(task.Headers()[headerTaskName]); taskName != "" {
		return taskName
	}
	meta := workflowTaskMetaFromTask(task)
	if task.Type() == TypeWorkflowTrigger {
		return workflowTriggerTaskName(meta.WorkflowName, "")
	}
	if meta.WorkflowName != "" || meta.WorkflowNode != "" {
		return workflowNodeTaskName(meta.WorkflowName, meta.WorkflowNode, task.Type())
	}
	return taskTypeDisplayName(task.Type())
}

// TaskDisplayName 返回任务展示名称，供任务系统外的告警和审计链路复用。
func TaskDisplayName(task *asynq.Task) string {
	return taskNameFromTask(task)
}

// TaskSource 返回任务来源，供任务系统外的告警和审计链路复用。
func TaskSource(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Headers()[headerTaskSource])
}

// taskInfoDisplayName 从 Asynq 任务详情中推导任务展示名称，供任务列表和详情页复用。
func taskInfoDisplayName(info *asynq.TaskInfo) string {
	if info == nil {
		return ""
	}
	if taskName := strings.TrimSpace(info.Headers[headerTaskName]); taskName != "" {
		return taskName
	}
	meta := WorkflowTaskMeta{}
	if len(info.Payload) > 0 {
		_ = json.Unmarshal(info.Payload, &meta)
	}
	if workflowName := helper.FirstNonEmptyString(strings.TrimSpace(info.Headers[headerWorkflowName]), strings.TrimSpace(meta.WorkflowName)); workflowName != "" {
		if info.Type == TypeWorkflowTrigger {
			return workflowTriggerTaskName(workflowName, "")
		}
		workflowNode := helper.FirstNonEmptyString(strings.TrimSpace(info.Headers[headerWorkflowNode]), strings.TrimSpace(meta.WorkflowNode))
		return workflowNodeTaskName(workflowName, workflowNode, info.Type)
	}
	return taskTypeDisplayName(info.Type)
}

// taskLogFields 返回任务执行链路的统一附加日志字段，便于按脚本、来源和工作流快速检索。
func taskLogFields(task *asynq.Task) []logx.LogField {
	if task == nil {
		return nil
	}
	fields := make([]logx.LogField, 0, 4)
	if taskName := taskNameFromTask(task); taskName != "" {
		fields = append(fields, logx.Field("task_name", taskName))
	}
	if taskSource := strings.TrimSpace(task.Headers()[headerTaskSource]); taskSource != "" {
		fields = append(fields, logx.Field("task_source", taskSource))
	}
	return fields
}

// taskStatsLogFields 返回任务执行统计摘要字段，字段名保持稳定便于日志平台聚合。
func taskStatsLogFields(snapshot *taskstats.Snapshot) []logx.LogField {
	if snapshot == nil || snapshot.Empty() {
		return nil
	}
	return []logx.LogField{
		logx.Field("trace_total_count", snapshot.TotalCount),
		logx.Field("trace_read_count", snapshot.ReadCount),
		logx.Field("trace_insert_count", snapshot.InsertCount),
		logx.Field("trace_update_count", snapshot.UpdateCount),
		logx.Field("trace_delete_count", snapshot.DeleteCount),
		logx.Field("trace_upsert_count", snapshot.UpsertCount),
		logx.Field("trace_skip_count", snapshot.SkipCount),
		logx.Field("trace_error_count", snapshot.ErrorCount),
		logx.Field("trace_detail_count", len(snapshot.Details)),
		logx.Field("trace_duration_ms", snapshot.DurationMS),
	}
}

// taskLogMessage 构造任务生命周期日志正文，方便只看 message 列时也能定位任务。
func taskLogMessage(action string, meta *requestctx.Meta) string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "任务 日志"
	}
	parts := []string{action}
	if meta == nil {
		return strings.Join(parts, " ")
	}
	parts = appendTextKV(parts, "task_name", meta.TaskName)
	parts = appendTextKV(parts, "task_type", meta.TaskType)
	parts = appendTextKV(parts, "task_id", meta.TaskID)
	parts = appendTextKV(parts, "queue", meta.TaskQueue)
	parts = appendTextKV(parts, "workflow", meta.WorkflowName)
	parts = appendTextKV(parts, "node", meta.WorkflowNode)
	parts = appendTextKV(parts, "workflow_id", meta.WorkflowID)
	parts = appendTextKV(parts, "mode", meta.Mode)
	if meta.ShardTotal > 0 {
		parts = append(parts, fmt.Sprintf("shard=%d/%d", meta.ShardIndex, meta.ShardTotal))
	}
	if meta.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("latency_ms=%d", meta.LatencyMS))
	}
	return strings.Join(parts, " ")
}

// appendTextKV 向日志正文追加非空键值片段。
func appendTextKV(parts []string, key string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, fmt.Sprintf("%s=%s", key, value))
}

// buildTaskExecutionResult 组装任务成功后的统一结果摘要，供任务详情页直接展示。
func buildTaskExecutionResult(task *asynq.Task, meta *requestctx.Meta, startedAt time.Time, statsSnapshot *taskstats.Snapshot) TaskExecutionResult {
	result := TaskExecutionResult{
		Status:         taskExecutionStatusSuccess,
		FinishedAt:     time.Now().Format(time.RFC3339),
		ExecutionTrace: statsSnapshot,
	}
	if !startedAt.IsZero() {
		result.StartedAt = startedAt.Format(time.RFC3339)
	}
	if task != nil {
		result.TaskSource = strings.TrimSpace(task.Headers()[headerTaskSource])
		fillTaskExecutionResultFromPayload(task, &result)
	}
	if meta == nil {
		if task != nil {
			result.TaskType = strings.TrimSpace(task.Type())
			result.TaskName = taskNameFromTask(task)
		}
		return result
	}
	result.TraceID = strings.TrimSpace(meta.TraceID)
	result.TaskID = strings.TrimSpace(meta.TaskID)
	result.TaskType = strings.TrimSpace(meta.TaskType)
	result.TaskName = strings.TrimSpace(meta.TaskName)
	result.TaskQueue = strings.TrimSpace(meta.TaskQueue)
	result.LatencyMS = meta.LatencyMS
	result.BizCode = meta.BizCode
	result.BizMessage = strings.TrimSpace(meta.BizMessage)
	result.WorkflowID = strings.TrimSpace(meta.WorkflowID)
	result.WorkflowName = strings.TrimSpace(meta.WorkflowName)
	result.WorkflowNode = strings.TrimSpace(meta.WorkflowNode)
	result.ShardIndex = meta.ShardIndex
	result.ShardTotal = meta.ShardTotal
	if result.TaskType == "" && task != nil {
		result.TaskType = strings.TrimSpace(task.Type())
	}
	if result.TaskName == "" && task != nil {
		result.TaskName = taskNameFromTask(task)
	}
	return result
}

// fillTaskExecutionResultFromPayload 从任务 payload 中提炼结果摘要，减少排障时反复展开原始 JSON 的成本。
func fillTaskExecutionResultFromPayload(task *asynq.Task, result *TaskExecutionResult) {
	if task == nil || result == nil || len(task.Payload()) == 0 {
		return
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(task.Payload(), &payloadMap); err != nil || len(payloadMap) == 0 {
		return
	}
	result.PayloadKeys = payloadSummaryKeys(payloadMap)
	result.Queue = helper.FirstNonEmptyString(result.Queue, strings.TrimSpace(anyToString(payloadMap["queue"])))
	result.WorkflowID = helper.FirstNonEmptyString(result.WorkflowID, strings.TrimSpace(anyToString(payloadMap["workflowId"])))
	result.WorkflowName = helper.FirstNonEmptyString(result.WorkflowName, strings.TrimSpace(anyToString(payloadMap["workflowName"])))
	result.WorkflowNode = helper.FirstNonEmptyString(result.WorkflowNode, strings.TrimSpace(anyToString(payloadMap["workflowNode"])))
	if result.ShardTotal == 0 {
		result.ShardTotal = anyToInt(payloadMap["shardTotal"])
	}
	if result.ShardIndex == 0 {
		result.ShardIndex = anyToInt(payloadMap["shardIndex"])
	}
	if result.GrayPercent == 0 {
		result.GrayPercent = anyToInt(payloadMap["grayPercent"])
	}
	result.Operation = helper.FirstNonEmptyString(result.Operation, strings.TrimSpace(anyToString(payloadMap["operation"])))
	result.Mode = helper.FirstNonEmptyString(result.Mode, strings.TrimSpace(anyToString(payloadMap["mode"])))
	if targets := anyToStringSlice(payloadMap["targets"]); len(targets) > 0 {
		result.TargetCount = len(targets)
		result.Targets = limitPreviewStrings(targets, 5)
	}
	if uids := anyToStringSlice(payloadMap["uids"]); len(uids) > 0 {
		result.UIDCount = len(uids)
	}
	if tagTypes := anyToStringSlice(payloadMap["tagTypes"]); len(tagTypes) > 0 {
		result.TagTypeCount = len(tagTypes)
	}
}

// payloadSummaryKeys 返回 payload 顶层字段摘要，固定排序后截断，避免结果展示过长。
func payloadSummaryKeys(payloadMap map[string]any) []string {
	keys := make([]string, 0, len(payloadMap))
	for key := range payloadMap {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return limitPreviewStrings(keys, 12)
}

// anyToString 把任意 JSON 值转成字符串，优先用于结果摘要提取。
func anyToString(value any) string {
	switch current := value.(type) {
	case string:
		return current
	case float64:
		return strconv.FormatInt(int64(current), 10)
	case int:
		return strconv.Itoa(current)
	case int64:
		return strconv.FormatInt(current, 10)
	case json.Number:
		return current.String()
	default:
		return ""
	}
}

// anyToInt 把任意 JSON 值转成整数，常用于提取 shard/gray 等配置字段。
func anyToInt(value any) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	case int64:
		return int(current)
	case json.Number:
		parsed, _ := current.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(current))
		return parsed
	default:
		return 0
	}
}

// anyToStringSlice 把 JSON 数组统一转成字符串切片，便于统计 targets/uids/tagTypes 数量。
func anyToStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(anyToString(item))
		if text == "" {
			continue
		}
		result = append(result, text)
	}
	return result
}

// limitPreviewStrings 截断预览字段条数，避免任务结果因目标列表过长而失控。
func limitPreviewStrings(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
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

// taskRuntimeKey 返回任务运行耗时快照 Redis key。
func (m *Manager) taskRuntimeKey(taskID string) string {
	return keys.TaskRuntimeKey(m.appNamespace(), taskID)
}

// taskRuntimeNeedsWorkflowRetention 判断当前任务运行快照是否需要对齐工作流保留窗口。
func taskRuntimeNeedsWorkflowRetention(ctx context.Context) bool {
	meta := requestctx.FromContext(ctx)
	return meta != nil && strings.TrimSpace(meta.WorkflowID) != ""
}

// taskRuntimeActiveRetention 返回任务执行中快照保留时间。
// active 阶段尚不知道最终状态，先给足排查窗口；完成后会按最终状态收敛 TTL。
func (m *Manager) taskRuntimeActiveRetention(ctx context.Context) time.Duration {
	retention := m.completedRetention()
	if workflowRetention := m.workflowRetention(); taskRuntimeNeedsWorkflowRetention(ctx) && workflowRetention > retention {
		retention = workflowRetention
	}
	if archivedRetention := m.archivedRetention(); archivedRetention > retention {
		retention = archivedRetention
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return retention + time.Hour
}

// taskRuntimeFinalRetention 返回任务完成后运行耗时快照保留时间。
func (m *Manager) taskRuntimeFinalRetention(ctx context.Context, runErr error) time.Duration {
	workflowRetention := m.workflowRetention()
	needsWorkflowRetention := taskRuntimeNeedsWorkflowRetention(ctx)
	if taskWillArchive(ctx, runErr) {
		retention := m.archivedRetention()
		if needsWorkflowRetention && workflowRetention > retention {
			retention = workflowRetention
		}
		if retention <= 0 {
			retention = 24 * time.Hour
		}
		return retention + time.Hour
	}
	retention := m.completedRetention()
	if needsWorkflowRetention && workflowRetention > retention {
		retention = workflowRetention
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return retention + time.Hour
}

// expireFinalTaskRecord 为成功和终态失败任务的 Asynq hash 设置 TTL。
func (m *Manager) expireFinalTaskRecord(ctx context.Context, queue string, taskID string, runErr error) {
	taskID = strings.TrimSpace(taskID)
	queue = strings.TrimSpace(queue)
	if m == nil || m.redis == nil || taskID == "" || queue == "" {
		return
	}
	retention, ok := m.finalTaskRecordRetention(ctx, runErr)
	if !ok || retention <= 0 {
		return
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	if err := m.redis.Expire(writeCtx, keys.TaskAsynqTaskHashKey(queue, taskID), retention+taskRecordTTLBuffer).Err(); err != nil {
		loggerx.Errorw(writeCtx, "任务终态缓存 TTL 设置失败", err, logx.Field("task_id", taskID))
	}
}

// finalTaskRecordRetention 返回终态任务 hash 应使用的保留时长。
func (m *Manager) finalTaskRecordRetention(ctx context.Context, runErr error) (time.Duration, bool) {
	if runErr == nil {
		return m.completedRetention(), true
	}
	if !taskWillArchive(ctx, runErr) {
		return 0, false
	}
	return m.archivedRetention(), true
}

// taskWillArchive 判断本次失败是否会进入 archived 终态。
func taskWillArchive(ctx context.Context, runErr error) bool {
	if runErr == nil || errors.Is(runErr, asynq.RevokeTask) {
		return false
	}
	if errors.Is(runErr, asynq.SkipRetry) {
		return true
	}
	retried, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if !retryOK || !maxOK {
		return false
	}
	return retried >= maxRetry
}

// taskFinalWriteContext 创建任务收尾写 Redis 使用的短后台上下文。
// 收尾写入脱离业务 ctx，避免超时后状态仍停留在 running。
func (m *Manager) taskFinalWriteContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), taskFinalWriteTimeout)
	if meta := requestctx.FromContext(parent); meta != nil {
		metaCopy := *meta
		ctx = requestctx.WithMeta(ctx, &metaCopy)
	}
	return loggerx.BindContext(ctx), cancel
}

// recordTaskRuntimeStart 记录任务开始执行时间，供 active 列表展示已运行时长。
func (m *Manager) recordTaskRuntimeStart(ctx context.Context, taskID string, begin time.Time) {
	taskID = strings.TrimSpace(taskID)
	if m == nil || m.redis == nil || taskID == "" {
		return
	}
	nowText := begin.Format(time.RFC3339)
	values := map[string]any{
		"status":      "running",
		"startedAt":   nowText,
		"startedAtMs": begin.UnixMilli(),
		"updatedAt":   nowText,
	}
	if err := m.redis.HSet(ctx, m.taskRuntimeKey(taskID), values).Err(); err != nil {
		loggerx.Errorw(ctx, "任务耗时记录开始失败", err, logx.Field("task_id", taskID))
		return
	}
	_ = m.redis.Expire(ctx, m.taskRuntimeKey(taskID), m.taskRuntimeActiveRetention(ctx)).Err()
}

// recordTaskRuntimeFinish 记录任务完成耗时，成功和失败都会保留。
func (m *Manager) recordTaskRuntimeFinish(ctx context.Context, taskID string, begin time.Time, runErr error, statsSnapshot *taskstats.Snapshot) {
	taskID = strings.TrimSpace(taskID)
	if m == nil || m.redis == nil || taskID == "" {
		return
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()

	finishedAt := time.Now()
	durationMS := finishedAt.Sub(begin).Milliseconds()
	if durationMS <= 0 {
		durationMS = 1
	}
	status := "success"
	if runErr != nil {
		status = "failed"
	}
	values := map[string]any{
		"startedAt":    begin.Format(time.RFC3339),
		"startedAtMs":  begin.UnixMilli(),
		"status":       status,
		"finishedAt":   finishedAt.Format(time.RFC3339),
		"durationMs":   durationMS,
		"updatedAt":    finishedAt.Format(time.RFC3339),
		"finishedAtMs": finishedAt.UnixMilli(),
	}
	if statsSnapshot != nil && !statsSnapshot.Empty() {
		if rawStats, err := json.Marshal(statsSnapshot); err == nil {
			values["executionTrace"] = string(rawStats)
			values["traceTotalCount"] = statsSnapshot.TotalCount
			values["traceReadCount"] = statsSnapshot.ReadCount
			values["traceInsertCount"] = statsSnapshot.InsertCount
			values["traceUpdateCount"] = statsSnapshot.UpdateCount
			values["traceDeleteCount"] = statsSnapshot.DeleteCount
			values["traceUpsertCount"] = statsSnapshot.UpsertCount
			values["traceSkipCount"] = statsSnapshot.SkipCount
			values["traceErrorCount"] = statsSnapshot.ErrorCount
		}
	}
	if runErr != nil {
		values["lastErr"] = runErr.Error()
	}
	if err := m.redis.HSet(writeCtx, m.taskRuntimeKey(taskID), values).Err(); err != nil {
		loggerx.Errorw(writeCtx, "任务耗时记录完成失败", err, logx.Field("task_id", taskID))
		return
	}
	_ = m.redis.Expire(writeCtx, m.taskRuntimeKey(taskID), m.taskRuntimeFinalRetention(ctx, runErr)).Err()
}

// readTaskRuntime 读取任务运行耗时快照。
func (m *Manager) readTaskRuntime(ctx context.Context, taskID string) taskRuntimeRecord {
	taskID = strings.TrimSpace(taskID)
	if m == nil || m.redis == nil || taskID == "" {
		return taskRuntimeRecord{}
	}
	values, err := m.redis.HGetAll(ctx, m.taskRuntimeKey(taskID)).Result()
	if err != nil || len(values) == 0 {
		return taskRuntimeRecord{}
	}
	return taskRuntimeRecord{
		StartedAt:      strings.TrimSpace(values["startedAt"]),
		FinishedAt:     strings.TrimSpace(values["finishedAt"]),
		DurationMS:     toInt64(values["durationMs"]),
		ExecutionTrace: taskExecutionStatsFromJSON([]byte(strings.TrimSpace(values["executionTrace"]))),
	}
}

// taskDurationFromResult 从成功任务结果中兜底提取历史耗时。
func taskDurationFromResult(result []byte) int64 {
	if len(result) == 0 {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		return 0
	}
	if value := toInt64(payload["durationMs"]); value > 0 {
		return value
	}
	return toInt64(payload["latencyMs"])
}

// taskStartedAtFromResult 从成功任务结果中兜底提取开始执行时间。
func taskStartedAtFromResult(result []byte) string {
	if len(result) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		return ""
	}
	startedAt := strings.TrimSpace(anyToString(payload["startedAt"]))
	if _, err := time.Parse(time.RFC3339, startedAt); err != nil {
		return ""
	}
	return startedAt
}

// taskExecutionStatsFromJSON 从 JSON 字节中读取任务处理量快照，兼容空值和旧数据。
func taskExecutionStatsFromJSON(raw []byte) *taskstats.Snapshot {
	if len(raw) == 0 {
		return nil
	}
	var snapshot taskstats.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Empty() {
		return nil
	}
	return &snapshot
}

// taskExecutionStatsFromResult 从任务结果中提取处理量快照。
func taskExecutionStatsFromResult(result []byte) *taskstats.Snapshot {
	if len(result) == 0 {
		return nil
	}
	var payload struct {
		ExecutionTrace *taskstats.Snapshot `json:"executionTrace"`
	}
	if err := json.Unmarshal(result, &payload); err != nil || payload.ExecutionTrace == nil || payload.ExecutionTrace.Empty() {
		return nil
	}
	return payload.ExecutionTrace
}

// taskTypeUsageHint 返回任务类型使用提示。
func taskTypeUsageHint(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return "通常不建议直接手动投递该类型；优先使用“手动触发工作流”表单。"
	case taskType == TypeWorkflowNoop:
		return "仅工作流内部使用，人工投递没有业务意义。"
	case taskType == TypeCacheRefreshRequest:
		return "适合少量缓存目标补刷；payload.targets 填缓存目标数组。"
	case taskType == TypeCacheRefreshBatch:
		return "该任务通常由聚合器自动生成，不建议人工直接投递。"
	case taskType == archive.TaskTypeExecute:
		return "建议通过归档工作流统一触发；单独投递时只适合排障或补跑。"
	case taskType == "admin:export":
		return "建议优先使用管理员列表页导出入口；手工投递时需提供 jobId、operatorId 和 request。"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "该类型通常由用户标签工作流节点内部调度，不建议脱离工作流单独投递。"
	default:
		return "投递前请确认后端已为该任务类型注册合法 payload 解析逻辑。"
	}
}

// taskTypePayloadExample 返回任务类型推荐负载示例。
func taskTypePayloadExample(taskType string) string {
	switch {
	case taskType == TypeWorkflowTrigger:
		return fmt.Sprintf("{\n  \"workflowId\": \"wf-demo-001\",\n  \"workflowName\": \"cache.refresh\",\n  \"targets\": [\"%s\"]\n}", fmt.Sprintf(keys.AdminProfile, 1))
	case taskType == TypeWorkflowNoop:
		return "{\n  \"note\": \"finalize\"\n}"
	case taskType == TypeCacheRefreshRequest, taskType == TypeCacheRefreshBatch:
		return fmt.Sprintf("{\n  \"operation\": \"manual_refresh\",\n  \"targets\": [\"%s\", \"%s\"]\n}",
			fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1))
	case taskType == archive.TaskTypeExecute:
		return "{\n  \"targets\": [\"admin_log\"]\n}"
	case taskType == "admin:export":
		return "{\n  \"jobId\": \"job-demo-001\",\n  \"operatorId\": 1,\n  \"operatorName\": \"admin\",\n  \"request\": {\n    \"username\": \"demo\"\n  }\n}"
	case strings.HasPrefix(taskType, "user_tag:"):
		return "{\n  \"mode\": \"targeted\",\n  \"targets\": [\"uid=10001\"],\n  \"uids\": [10001],\n  \"tagTypes\": [2, 3]\n}"
	default:
		return "{\n  \"demo\": true\n}"
	}
}

// taskTypeManualRecommended 返回当前任务类型是否适合在总控台人工直接投递。
func taskTypeManualRecommended(taskType string) bool {
	switch {
	case taskType == TypeCacheRefreshRequest:
		return true
	case taskType == "admin:export":
		return true
	default:
		return false
	}
}

// workflowUsageHint 返回工作流使用提示。
func workflowUsageHint(name string) string {
	switch name {
	case WorkflowNameCacheRefresh:
		return "适合按目标列表触发缓存刷新；targets 可填写缓存目标或页面带入的缓存键。"
	case archive.WorkflowNameRun:
		return "适合在低峰期批量执行热表归档；targets 可填写 admin_log 等归档任务名。"
	case "user_tag.full.rebuild":
		return "每日全量重建链路，建议在低峰期执行，避免和日常重算任务叠加。"
	case "user_tag.delta.refresh":
		return "用于日常增量刷新用户标签，通常不需要填写 targets。"
	case "user_tag.targeted.calc":
		return "用于指定用户补算，必须在 targets 中填写 uid；多维筛选 hash 不能作为用户标签计算来源。"
	case "user_tag.tag.recalculate":
		return "用于按标签类型全量重算，建议在 targets 中携带 tagType 或执行标识。"
	case "user_tag.runtime.cleanup":
		return "用于机会型清理用户标签运行期辅助表，通常由周期任务触发，不需要填写 targets。"
	case "user_tag.kafka_outbox.retry_scan":
		return "用于周期扫描并重推用户标签 outbox 异常数据，通常由周期任务触发，不需要填写 targets。"
	default:
		return "触发前请确认执行目标和默认队列是否符合当前环境。"
	}
}

// workflowTargetsExample 返回工作流执行目标填写示例。
func workflowTargetsExample(name string) string {
	switch name {
	case WorkflowNameCacheRefresh:
		return strings.Join([]string{fmt.Sprintf(keys.AdminProfile, 1), fmt.Sprintf(keys.AdminRolesDetail, 1)}, ", ")
	case archive.WorkflowNameRun:
		return "admin_log"
	case "user_tag.targeted.calc":
		return "uid:10001, uid:10002"
	case "user_tag.tag.recalculate":
		return "tagType:2, tagType:3"
	case "user_tag.runtime.cleanup":
		return ""
	case "user_tag.kafka_outbox.retry_scan":
		return ""
	default:
		return ""
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
				loggerx.Errorw(nil, "任务队列 健康检查失败", err, logx.Field("app_id", m.appNamespace()))
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

// startArchivedCleanerLocked 启动归档失败任务过期清理协程。
// 调用方需持有 lifecycleMu，避免 Worker 重复启动时创建多个清理循环。
func (m *Manager) startArchivedCleanerLocked() {
	if m == nil || m.redis == nil || m.archivedCleanStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.archivedCleanStop = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.cleanupExpiredArchivedTasks(ctx)
		ticker := time.NewTicker(taskArchivedCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanupExpiredArchivedTasks(ctx)
			}
		}
	}()
}

// cleanupExpiredArchivedTasks 清理超过保留期的 archived 任务。
func (m *Manager) cleanupExpiredArchivedTasks(ctx context.Context) {
	if m == nil || m.redis == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	retention := m.archivedRetention()
	if retention <= 0 {
		return
	}
	cleanupCtx := loggerx.BindContext(ctx)
	cutoff := time.Now().Add(-retention).Unix()
	for _, queue := range m.archivedCleanupQueueNames() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		internalQueue := m.namespacedQueueName(queue)
		deleted, err := m.cleanupExpiredArchivedQueue(cleanupCtx, internalQueue, cutoff, taskArchivedCleanupBatchSize, taskArchivedCleanupMaxBatches)
		if err != nil {
			loggerx.Errorw(cleanupCtx, "归档失败任务过期清理失败", err, logx.Field("queue", queue))
			continue
		}
		if deleted > 0 {
			loggerx.Infow(cleanupCtx, "归档失败任务过期清理完成",
				logx.Field("queue", queue),
				logx.Field("deleted", deleted),
				logx.Field("retention_seconds", int64(retention.Seconds())),
			)
		}
	}
}

// archivedCleanupQueueNames 返回需要执行 archived 清理的逻辑队列名列表。
func (m *Manager) archivedCleanupQueueNames() []string {
	if m == nil || m.inspector == nil {
		return nil
	}
	rawQueueNames, err := m.inspector.Queues()
	if err != nil || len(rawQueueNames) == 0 {
		return m.configuredQueueNames()
	}
	return m.visibleQueueNames(rawQueueNames)
}

// cleanupExpiredArchivedQueue 分批清理单个队列中过期的 archived 任务。
func (m *Manager) cleanupExpiredArchivedQueue(ctx context.Context, internalQueue string, cutoffUnix int64, batchSize int, maxBatches int) (int64, error) {
	if batchSize <= 0 {
		batchSize = taskArchivedCleanupBatchSize
	}
	if maxBatches <= 0 {
		maxBatches = 1
	}
	var total int64
	for i := 0; i < maxBatches; i++ {
		deleted, err := m.deleteExpiredArchivedTasks(ctx, internalQueue, cutoffUnix, batchSize)
		if err != nil {
			return total, errors.Wrapf(err, "清理过期 archived 任务失败 queue=%s", internalQueue)
		}
		total += deleted
		if deleted < int64(batchSize) {
			return total, nil
		}
	}
	return total, nil
}

// deleteExpiredArchivedTasks 删除单个队列中过期的 archived 任务。
func (m *Manager) deleteExpiredArchivedTasks(ctx context.Context, internalQueue string, cutoffUnix int64, batchSize int) (int64, error) {
	internalQueue = strings.TrimSpace(internalQueue)
	if internalQueue == "" {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = taskArchivedCleanupBatchSize
	}
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, "archived")
	if err != nil {
		return 0, errors.Tag(err)
	}
	result, err := deleteExpiredArchivedTasksScript.Run(ctx, m.redis, []string{archivedKey},
		cutoffUnix,
		keys.TaskAsynqTaskHashKeyPrefix(internalQueue),
		batchSize,
	).Result()
	if err != nil {
		return 0, errors.Tag(err)
	}
	return toInt64(result), nil
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
	prefix := keys.TaskQueueNameScope(m.appNamespace())
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
	prefix := keys.TaskQueueNameScope(m.appNamespace())
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

// ListTasks 按队列和状态分页查询任务列表。
func (m *Manager) ListTasks(ctx context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("任务列表请求不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queue := strings.TrimSpace(req.Queue)
	state := strings.TrimSpace(req.State)
	group := strings.TrimSpace(req.Group)
	workflowID := strings.TrimSpace(req.WorkflowID)
	// taskName 是任务名称关键字筛选条件，空值时保留 Asynq 原生分页路径。
	taskName := strings.TrimSpace(req.TaskName)
	startTime := strings.TrimSpace(req.StartTime)
	endTime := strings.TrimSpace(req.EndTime)
	timeRange, err := parseTaskListTimeRange(startTime, endTime)
	if err != nil {
		return nil, errors.Tag(err)
	}
	internalQueue := m.namespacedQueueName(queue)
	internalGroup := m.namespacedGroup(group)
	resp := &types.TaskListResp{
		Queue:      queue,
		State:      state,
		Group:      group,
		WorkflowID: workflowID,
		TaskName:   taskName,
		StartTime:  startTime,
		EndTime:    endTime,
		Page:       req.Page,
		PageSize:   req.PageSize,
		Tasks:      make([]types.TaskItem, 0),
	}
	needsItemScan := workflowID != "" || taskName != "" || taskTimeRangeNeedsItemScan(state, timeRange)
	periodicNextRuns := m.periodicNextRuns(time.Now())
	if !needsItemScan {
		items, total, err := m.listTaskInfoPage(ctx, internalQueue, internalGroup, state, req.Page, req.PageSize, timeRange)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = total
		resp.Tasks = make([]types.TaskItem, 0, len(items))
		for _, item := range items {
			resp.Tasks = append(resp.Tasks, m.toTaskItemWithPeriodicNextRuns(ctx, item, periodicNextRuns))
		}
		return resp, nil
	}
	filteredTasks, total, err := m.listTaskItemsByFilters(ctx, internalQueue, internalGroup, state, workflowID, taskName, req.Page, req.PageSize, timeRange, periodicNextRuns)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp.Total = total
	resp.Tasks = filteredTasks
	return resp, nil
}

// taskTimeRangeNeedsItemScan 判断时间范围是否需要读取任务详情后再过滤。
func taskTimeRangeNeedsItemScan(state string, timeRange taskListTimeRange) bool {
	if !timeRange.hasRange {
		return false
	}
	switch strings.TrimSpace(state) {
	case "scheduled", "archived", "completed":
		return false
	default:
		return true
	}
}

// listTaskInfoPage 按状态读取单页 Asynq 任务详情，并返回该状态下的总数。
func (m *Manager) listTaskInfoPage(ctx context.Context, internalQueue string, internalGroup string, state string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if state == "scheduled" {
		return m.listScheduledTaskInfoPage(ctx, internalQueue, page, pageSize, timeRange)
	}
	if taskStateUsesDescZSet(state) {
		return m.listDescZSetTaskInfoPage(ctx, internalQueue, state, page, pageSize, timeRange)
	}
	opts := []asynq.ListOption{asynq.Page(page), asynq.PageSize(pageSize)}
	var (
		items []*asynq.TaskInfo
		err   error
	)
	switch state {
	case "pending":
		items, err = m.inspector.ListPendingTasks(internalQueue, opts...)
	case "active":
		items, err = m.inspector.ListActiveTasks(internalQueue, opts...)
	case "aggregating":
		items, err = m.inspector.ListAggregatingTasks(internalQueue, internalGroup, opts...)
	default:
		return nil, 0, errors.Errorf("不支持的任务状态: %s", state)
	}
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	total := int64(len(items))
	if info, qerr := m.inspector.GetQueueInfo(internalQueue); qerr == nil {
		total = queueInfoTotalByState(info, state)
	}
	return items, total, nil
}

// taskStateUsesDescZSet 判断状态是否需要按 zset 分数倒序读取。
// 这些状态底层都是 zset；倒序读取能让任务列表优先展示最近的重试、归档和完成记录。
func taskStateUsesDescZSet(state string) bool {
	switch strings.TrimSpace(state) {
	case "retry", "archived", "completed":
		return true
	default:
		return false
	}
}

// listDescZSetTaskInfoPage 按 zset 分数倒序读取任务详情，并保留队列级总数。
// completed 的分数为完成时间加保留时长；本项目统一保留时长，因此默认列表可按该分数展示最新完成记录。
func (m *Manager) listDescZSetTaskInfoPage(ctx context.Context, internalQueue string, state string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key, err := keys.TaskAsynqStateZSetKey(internalQueue, state)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	pageStart, pageStop := taskListPageRange(page, pageSize)
	var (
		ids   []string
		total int64
	)
	if minScore, maxScore, ok := descZSetScoreRange(state, timeRange, m.completedRetention()); ok {
		total, err = m.redis.ZCount(ctx, key, minScore, maxScore).Result()
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if total <= 0 {
			return []*asynq.TaskInfo{}, 0, nil
		}
		ids, err = m.redis.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:    minScore,
			Max:    maxScore,
			Offset: pageStart,
			Count:  int64(pageSize),
		}).Result()
	} else {
		total, err = m.taskTotalByState(internalQueue, state)
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if total <= 0 {
			return []*asynq.TaskInfo{}, 0, nil
		}
		ids, err = m.redis.ZRevRange(ctx, key, pageStart, pageStop).Result()
	}
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	items := make([]*asynq.TaskInfo, 0, len(ids))
	for _, id := range ids {
		taskInfo, infoErr := m.inspector.GetTaskInfo(internalQueue, id)
		if errors.Is(infoErr, asynq.ErrTaskNotFound) {
			continue
		}
		if infoErr != nil {
			return nil, 0, errors.Tag(infoErr)
		}
		if taskInfo.State.String() != state {
			continue
		}
		items = append(items, taskInfo)
	}
	return items, total, nil
}

// descZSetScoreRange 返回可直接用 zset 分数过滤的任务时间范围。
// archived 分数就是失败时间；completed 分数是完成时间加保留时长，按当前统一保留配置反推查询范围。
func descZSetScoreRange(state string, timeRange taskListTimeRange, retention time.Duration) (string, string, bool) {
	if !timeRange.hasRange {
		return "", "", false
	}
	scoreOffset := int64(0)
	switch strings.TrimSpace(state) {
	case "archived":
	case "completed":
		scoreOffset = int64(retention.Seconds())
	default:
		return "", "", false
	}
	minScore := "-inf"
	maxScore := "+inf"
	if !timeRange.start.IsZero() {
		minScore = strconv.FormatInt(timeRange.start.Unix()+scoreOffset, 10)
	}
	if !timeRange.end.IsZero() {
		maxScore = strconv.FormatInt(timeRange.end.Unix()+scoreOffset, 10)
	}
	return minScore, maxScore, true
}

// taskTotalByState 返回队列中指定状态的任务总数。
func (m *Manager) taskTotalByState(internalQueue string, state string) (int64, error) {
	info, err := m.inspector.GetQueueInfo(internalQueue)
	if err != nil {
		return 0, errors.Tag(err)
	}
	return queueInfoTotalByState(info, state), nil
}

// listScheduledTaskInfoPage 按计划执行时间倒序读取 scheduled 任务。
// Asynq Inspector 的 ListScheduledTasks 固定按 NextProcessAt 升序且没有公开倒序选项，因此这里只对 scheduled 使用 zset 倒序取 ID，再复用 Inspector 装配任务详情。
func (m *Manager) listScheduledTaskInfoPage(ctx context.Context, internalQueue string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	total, err := m.scheduledTaskTotal(ctx, internalQueue, timeRange)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	if total <= 0 {
		return []*asynq.TaskInfo{}, 0, nil
	}
	ids, err := m.listScheduledTaskIDs(ctx, internalQueue, page, pageSize, timeRange)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	items := make([]*asynq.TaskInfo, 0, len(ids))
	for _, id := range ids {
		taskInfo, infoErr := m.inspector.GetTaskInfo(internalQueue, id)
		if errors.Is(infoErr, asynq.ErrTaskNotFound) {
			continue
		}
		if infoErr != nil {
			return nil, 0, errors.Tag(infoErr)
		}
		if taskInfo.State != asynq.TaskStateScheduled {
			continue
		}
		items = append(items, taskInfo)
	}
	return items, total, nil
}

// scheduledTaskTotal 返回 scheduled 总数；带时间段时按 nextProcessAt 分数范围统计。
func (m *Manager) scheduledTaskTotal(ctx context.Context, internalQueue string, timeRange taskListTimeRange) (int64, error) {
	if !timeRange.hasRange {
		info, err := m.inspector.GetQueueInfo(internalQueue)
		if err != nil {
			return 0, errors.Tag(err)
		}
		return int64(info.Scheduled), nil
	}
	minScore, maxScore := scheduledScoreRange(timeRange)
	total, err := m.redis.ZCount(ctx, keys.TaskAsynqScheduledKey(internalQueue), minScore, maxScore).Result()
	if err != nil {
		return 0, errors.Tag(err)
	}
	return total, nil
}

// listScheduledTaskIDs 按 scheduled.nextProcessAt 倒序读取任务 ID。
func (m *Manager) listScheduledTaskIDs(ctx context.Context, internalQueue string, page int, pageSize int, timeRange taskListTimeRange) ([]string, error) {
	pageStart, pageStop := taskListPageRange(page, pageSize)
	key := keys.TaskAsynqScheduledKey(internalQueue)
	if !timeRange.hasRange {
		return m.redis.ZRevRange(ctx, key, pageStart, pageStop).Result()
	}
	minScore, maxScore := scheduledScoreRange(timeRange)
	return m.redis.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    minScore,
		Max:    maxScore,
		Offset: pageStart,
		Count:  int64(pageSize),
	}).Result()
}

// scheduledScoreRange 把 nextProcessAt 时间范围转换为 Asynq scheduled zset 分数范围。
func scheduledScoreRange(timeRange taskListTimeRange) (string, string) {
	minScore := "-inf"
	maxScore := "+inf"
	if !timeRange.start.IsZero() {
		minScore = strconv.FormatInt(timeRange.start.Unix(), 10)
	}
	if !timeRange.end.IsZero() {
		maxScore = strconv.FormatInt(timeRange.end.Unix(), 10)
	}
	return minScore, maxScore
}

// taskListPageRange 把接口分页参数转换为 Redis 闭区间下标。
func taskListPageRange(page int, pageSize int) (int64, int64) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := int64((page - 1) * pageSize)
	return start, start + int64(pageSize) - 1
}

// listTaskItemsByFilters 在指定队列和状态下扫描任务，并按时间倒序过滤后分页。
// 管理接口层补充倒序和时间段过滤，保证列表排序稳定。
func (m *Manager) listTaskItemsByFilters(ctx context.Context, internalQueue string, internalGroup string, state string, workflowID string, taskName string, page int, pageSize int, timeRange taskListTimeRange, periodicNextRuns []periodicNextRun) ([]types.TaskItem, int64, error) {
	matchedTasks := make([]types.TaskItem, 0)
	workflowID = strings.TrimSpace(workflowID)
	taskName = strings.TrimSpace(taskName)
	for currentPage := 1; currentPage <= taskListFilterMaxPages; currentPage++ {
		items, _, err := m.listTaskInfoPage(ctx, internalQueue, internalGroup, state, currentPage, taskListFilterPageSize, timeRange)
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			taskItem := m.toTaskItemWithPeriodicNextRuns(ctx, item, periodicNextRuns)
			if !taskItemMatchesListFilters(taskItem, workflowID, taskName) {
				continue
			}
			if !timeRange.Contains(taskItem) {
				continue
			}
			matchedTasks = append(matchedTasks, taskItem)
		}
		if len(items) < taskListFilterPageSize {
			break
		}
	}
	sortTaskItemsByTimeDesc(matchedTasks)
	total := int64(len(matchedTasks))
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start >= len(matchedTasks) {
		return []types.TaskItem{}, total, nil
	}
	end := min(start+pageSize, len(matchedTasks))
	return matchedTasks[start:end], total, nil
}

// taskListTimeRange 表示任务列表时间过滤条件。
type taskListTimeRange struct {
	start    time.Time // 开始时间，零值表示不限制
	end      time.Time // 结束时间，零值表示不限制
	hasRange bool      // 是否启用时间过滤
}

// parseTaskListTimeRange 解析任务列表时间范围。
func parseTaskListTimeRange(start, end string) (taskListTimeRange, error) {
	var result taskListTimeRange
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start != "" {
		parsed, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return result, errors.Wrap(err, "解析任务列表开始时间失败")
		}
		result.start = parsed
		result.hasRange = true
	}
	if end != "" {
		parsed, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return result, errors.Wrap(err, "解析任务列表结束时间失败")
		}
		result.end = parsed
		result.hasRange = true
	}
	if !result.start.IsZero() && !result.end.IsZero() && result.start.After(result.end) {
		return result, errors.Errorf("任务列表开始时间不能晚于结束时间")
	}
	return result, nil
}

// Contains 判断任务是否命中时间范围；未启用时间范围时全部放行。
func (r taskListTimeRange) Contains(item types.TaskItem) bool {
	if !r.hasRange {
		return true
	}
	itemTime, ok := taskItemPrimaryTime(item)
	if !ok {
		return false
	}
	if !r.start.IsZero() && itemTime.Before(r.start) {
		return false
	}
	if !r.end.IsZero() && itemTime.After(r.end) {
		return false
	}
	return true
}

// sortTaskItemsByTimeDesc 按任务活动时间倒序排序，时间相同再按任务 ID 降序稳定展示。
func sortTaskItemsByTimeDesc(items []types.TaskItem) {
	slices.SortFunc(items, func(left, right types.TaskItem) int {
		leftTime, _ := taskItemPrimaryTime(left)
		rightTime, _ := taskItemPrimaryTime(right)
		switch {
		case leftTime.After(rightTime):
			return -1
		case leftTime.Before(rightTime):
			return 1
		default:
			return strings.Compare(right.ID, left.ID)
		}
	})
}

// taskItemPrimaryTime 返回任务列表排序和时间过滤使用的活动时间。
func taskItemPrimaryTime(item types.TaskItem) (time.Time, bool) {
	candidates := taskItemPrimaryTimeCandidates(item)
	for _, rawValue := range candidates {
		rawValue = strings.TrimSpace(rawValue)
		if rawValue == "" {
			continue
		}
		parsedValue, err := time.Parse(time.RFC3339, rawValue)
		if err == nil {
			return parsedValue, true
		}
	}
	return time.Time{}, false
}

// taskItemPrimaryTimeCandidates 按任务生命周期收集活动时间候选字段。
func taskItemPrimaryTimeCandidates(item types.TaskItem) []string {
	switch strings.TrimSpace(strings.ToLower(item.State)) {
	case "scheduled":
		return []string{item.NextProcessAt, item.Deadline}
	case "pending", "aggregating":
		return []string{item.NextProcessAt, item.Deadline}
	default:
		return []string{item.CompletedAt, item.LastFailedAt, item.StartedAt, item.NextProcessAt, item.Deadline}
	}
}

// taskItemMatchesListFilters 判断任务是否命中任务列表的链路筛选条件。
// taskName 覆盖展示名、任务类型、工作流元数据、任务头和载荷摘要。
func taskItemMatchesListFilters(item types.TaskItem, workflowID string, taskName string) bool {
	workflowID = strings.TrimSpace(workflowID)
	taskName = strings.TrimSpace(taskName)
	if workflowID != "" && !taskItemMatchesWorkflowID(item, workflowID) {
		return false
	}
	if taskName == "" {
		return true
	}
	needle := strings.ToLower(taskName)
	for _, candidate := range taskItemSearchCandidates(item) {
		if strings.Contains(strings.ToLower(strings.TrimSpace(candidate)), needle) {
			return true
		}
	}
	return false
}

// taskItemMatchesWorkflowID 判断任务是否属于指定工作流实例。
// 工作流 ID 可能来自标准字段、任务头或旧任务 payload；这里逐层兜底，避免历史任务无法通过链路筛选定位。
func taskItemMatchesWorkflowID(item types.TaskItem, workflowID string) bool {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return true
	}
	for _, candidate := range taskItemWorkflowIDCandidates(item) {
		if strings.TrimSpace(candidate) == workflowID {
			return true
		}
	}
	return false
}

// taskItemWorkflowIDCandidates 收集工作流 ID 候选值。
// 候选值只包含精确 ID 字段，不包含任意 header 全量扫描，避免误把其它值撞成同一 workflowId。
func taskItemWorkflowIDCandidates(item types.TaskItem) []string {
	candidates := []string{
		item.WorkflowID,
		item.Headers[headerWorkflowID],
	}
	appendTaskSearchJSONFields(&candidates, item.Payload, taskSearchFieldWorkflowID)
	appendTaskSearchJSONFields(&candidates, item.Result, taskSearchFieldWorkflowID)
	return candidates
}

// taskItemSearchCandidates 收集任务名称检索候选值。
// 这里保持只读转换，不访问 Redis/MySQL，避免带 taskName 筛选的受控扫描退化为更重的外部查询。
func taskItemSearchCandidates(item types.TaskItem) []string {
	candidates := []string{
		item.ID,
		item.TaskName,
		item.TaskType,
		item.WorkflowID,
		item.Group,
		item.Headers[headerTaskName],
		item.Headers[headerTaskSource],
		item.Headers[HeaderPeriodicName],
		item.Headers[headerWorkflowID],
		item.Headers[headerWorkflowName],
		item.Headers[headerWorkflowNode],
	}
	appendTaskSearchJSONFields(&candidates, item.Payload,
		taskSearchFieldTaskName,
		taskSearchFieldTaskType,
		taskSearchFieldWorkflowID,
		taskSearchFieldWorkflowName,
		taskSearchFieldWorkflowNode,
		taskSearchFieldTaskSource,
		"periodicName",
		taskSearchFieldMode,
		taskSearchFieldUniqueKey,
	)
	appendTaskSearchJSONFields(&candidates, item.Result,
		taskSearchFieldTaskName,
		taskSearchFieldTaskType,
		taskSearchFieldWorkflowID,
		taskSearchFieldWorkflowName,
		taskSearchFieldWorkflowNode,
		taskSearchFieldTaskSource,
		"periodicName",
		taskSearchFieldMode,
		taskSearchFieldUniqueKey,
	)
	return candidates
}

// appendTaskSearchJSONFields 从 JSON 对象中追加指定字段值。
// payload/result 字段不存在或 JSON 非对象时直接跳过。
func appendTaskSearchJSONFields(candidates *[]string, raw json.RawMessage, fieldNames ...string) {
	if candidates == nil || len(raw) == 0 || len(fieldNames) == 0 {
		return
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil || len(payloadMap) == 0 {
		return
	}
	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}
		value := strings.TrimSpace(anyToString(payloadMap[fieldName]))
		if value == "" {
			continue
		}
		*candidates = append(*candidates, value)
	}
}

// periodicNextRuns 计算当前有效周期任务的下一次触发时间，用于列表补齐 completed/archived 的 nextProcessAt。
func (m *Manager) periodicNextRuns(now time.Time) []periodicNextRun {
	if m == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	items := m.allPeriodicTasks()
	result := make([]periodicNextRun, 0, len(items))
	for _, item := range items {
		normalized, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr != nil {
			continue
		}
		cronspec, err := periodicTaskCronspec(normalized)
		if err != nil {
			continue
		}
		schedule, err := periodicCronParser.Parse(cronspec)
		if err != nil {
			continue
		}
		nextAt := schedule.Next(now)
		if nextAt.IsZero() {
			continue
		}
		result = append(result, periodicNextRun{
			PeriodicName:  periodicTaskDisplayName(normalized),
			WorkflowName:  strings.TrimSpace(normalized.Workflow),
			NextProcessAt: nextAt.Format(time.RFC3339),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NextProcessAt == result[j].NextProcessAt {
			return result[i].PeriodicName < result[j].PeriodicName
		}
		return result[i].NextProcessAt < result[j].NextProcessAt
	})
	return result
}

// periodicNextProcessAtForTaskItem 从周期任务配置中为历史任务补齐下一次触发时间。
func periodicNextProcessAtForTaskItem(item types.TaskItem, periodicNextRuns []periodicNextRun) string {
	if len(periodicNextRuns) == 0 || !taskItemHasPeriodicSource(item) {
		return ""
	}
	periodicName := taskItemPeriodicName(item)
	if periodicName != "" {
		for _, nextRun := range periodicNextRuns {
			if nextRun.PeriodicName == periodicName {
				return nextRun.NextProcessAt
			}
		}
	}
	workflowName := taskItemWorkflowName(item)
	if workflowName == "" {
		return ""
	}
	for _, nextRun := range periodicNextRuns {
		if nextRun.WorkflowName == workflowName {
			return nextRun.NextProcessAt
		}
	}
	return ""
}

// taskItemHasPeriodicSource 判断任务是否由周期调度触发。
func taskItemHasPeriodicSource(item types.TaskItem) bool {
	if taskItemPeriodicName(item) != "" {
		return true
	}
	source := helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[headerTaskSource]),
		taskItemJSONField(item.Payload, "source"),
		taskItemJSONField(item.Payload, taskSearchFieldTaskSource),
		taskItemJSONField(item.Result, taskSearchFieldTaskSource),
	)
	return source == WorkflowSourcePeriodic
}

// taskItemPeriodicName 从任务头或 JSON 字段中读取周期任务名称。
func taskItemPeriodicName(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[HeaderPeriodicName]),
		taskItemJSONField(item.Payload, "periodicName"),
		taskItemJSONField(item.Result, "periodicName"),
	)
}

// taskItemWorkflowName 从任务头或 JSON 字段中读取工作流名称。
func taskItemWorkflowName(item types.TaskItem) string {
	return helper.FirstNonEmptyString(
		strings.TrimSpace(item.Headers[headerWorkflowName]),
		taskItemJSONField(item.Payload, taskSearchFieldWorkflowName),
		taskItemJSONField(item.Result, taskSearchFieldWorkflowName),
	)
}

// taskItemJSONField 从任务 JSON 对象读取首层字符串字段。
func taskItemJSONField(raw json.RawMessage, fieldName string) string {
	fieldName = strings.TrimSpace(fieldName)
	if len(raw) == 0 || fieldName == "" {
		return ""
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil || len(payloadMap) == 0 {
		return ""
	}
	return strings.TrimSpace(anyToString(payloadMap[fieldName]))
}

// GetTaskInfo 查询单个任务的详情。
func (m *Manager) GetTaskInfo(ctx context.Context, req *types.GetTaskInfoReq) (*types.TaskItem, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("任务详情请求不能为空")
	}
	_ = ctx
	info, err := m.inspector.GetTaskInfo(m.namespacedQueueName(strings.TrimSpace(req.Queue)), strings.TrimSpace(req.TaskID))
	if err != nil {
		return nil, errors.Tag(err)
	}
	return new(m.toTaskItem(ctx, info)), nil
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

// persistTaskRecordForRerun 移除任务详情 hash 的 TTL，避免 archived 任务重跑后沿用旧过期时间。
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
			notifyPeriodicTaskConfigInvalid(idx, item, invalidErr.Error())
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
		notifyPeriodicTaskConfigInvalid(idx, normalized, invalidErr.Error())
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
func notifyPeriodicTaskConfigInvalid(index int, item config.TaskPeriodicConfig, reason string) {
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
	loggerx.ErrorTextw(nil, "周期任务 配置无效", reason, fields...)
}

// workflowMetaKey 返回工作流主记录在 Redis 中的 key。
func (m *Manager) workflowMetaKey(workflowID string) string {
	return keys.TaskWorkflowMetaKey(m.appNamespace(), workflowID)
}

// workflowNodesKey 返回工作流节点集合在 Redis 中的 key。
func (m *Manager) workflowNodesKey(workflowID string) string {
	return keys.TaskWorkflowNodesKey(m.appNamespace(), workflowID)
}

// workflowNodeKey 返回单个工作流节点状态记录的 Redis key。
func (m *Manager) workflowNodeKey(workflowID, nodeName string) string {
	return keys.TaskWorkflowNodeKey(m.appNamespace(), workflowID, nodeName)
}

// workflowNodeScheduledKey 返回节点调度去重标记的 Redis key。
func (m *Manager) workflowNodeScheduledKey(workflowID, nodeName string) string {
	return keys.TaskWorkflowNodeScheduledKey(m.appNamespace(), workflowID, nodeName)
}

// workflowNodeFinalizedKey 返回节点完成收口标记的 Redis key。
func (m *Manager) workflowNodeFinalizedKey(workflowID, nodeName string) string {
	return keys.TaskWorkflowNodeFinalizedKey(m.appNamespace(), workflowID, nodeName)
}

// workflowInstanceKey 返回单个分片任务实例终态巡检 Redis key。
// 终态去重标记已收敛到 workflowNodeKey 对应 hash 内，此方法用于回归确认不再写入实例散列 key。
func (m *Manager) workflowInstanceKey(workflowID, nodeName string, shardIndex int) string {
	return keys.TaskWorkflowNodeInstanceKey(m.appNamespace(), workflowID, nodeName, shardIndex)
}

// workflowCompletedKey 返回工作流完成标记的 Redis key。
func (m *Manager) workflowCompletedKey(workflowID string) string {
	return keys.TaskWorkflowCompletedKey(m.appNamespace(), workflowID)
}

// workflowFailedKey 返回工作流失败标记的 Redis key。
func (m *Manager) workflowFailedKey(workflowID string) string {
	return keys.TaskWorkflowFailedKey(m.appNamespace(), workflowID)
}

// workflowUniqueKey 返回工作流幂等占位记录的 Redis key。
func (m *Manager) workflowUniqueKey(name, key string) string {
	return keys.TaskWorkflowUniqueKey(m.appNamespace(), name, key)
}

// workflowUniqueLockKey 返回工作流幂等预占时使用的短锁 Redis key。
func (m *Manager) workflowUniqueLockKey(name, key string) string {
	return keys.TaskWorkflowUniqueLockKey(m.appNamespace(), name, key)
}

// workflowUniqueLockTTL 返回幂等预占互斥锁的超时时间。
func (m *Manager) workflowUniqueLockTTL() time.Duration {
	return 15 * time.Second
}

// workflowRetention 返回工作流元数据在 Redis 中的保留时长。
func (m *Manager) workflowRetention() time.Duration {
	seconds := m.CurrentConfig().WorkflowRetentionSeconds
	if seconds <= 0 {
		seconds = 86400
	}
	return time.Duration(seconds) * time.Second
}

// completedRetention 返回已完成任务在 Asynq 中的保留时长。
// 该配置用于任务列表查看成功执行记录；未显式配置时默认保留 24 小时。
func (m *Manager) completedRetention() time.Duration {
	seconds := m.CurrentConfig().CompletedRetentionSeconds
	if seconds <= 0 {
		seconds = 86400
	}
	return time.Duration(seconds) * time.Second
}

// archivedRetention 返回归档失败任务在 Asynq 中的保留时长。
func (m *Manager) archivedRetention() time.Duration {
	seconds := m.CurrentConfig().ArchivedRetentionSeconds
	if seconds <= 0 {
		seconds = taskArchivedRetentionDefaultSeconds
	}
	return time.Duration(seconds) * time.Second
}

// defaultWorkflowQueue 返回工作流默认执行队列。
func (m *Manager) defaultWorkflowQueue() string {
	return helper.FirstNonEmptyString(m.CurrentConfig().DefaultQueue, QueueDefault)
}

// queueWeights 返回 Worker 队列权重配置；未配置时使用默认权重。
func (m *Manager) queueWeights() map[string]int {
	cfg := m.CurrentConfig()
	result := make(map[string]int)
	if len(cfg.Queues) > 0 {
		for queueName, weight := range cfg.Queues {
			result[m.namespacedQueueName(queueName)] = weight
		}
		return result
	}
	result[m.namespacedQueueName(QueueCritical)] = 6
	result[m.namespacedQueueName(QueueDefault)] = 3
	result[m.namespacedQueueName(QueueMaintenance)] = 1
	return result
}

// concurrency 返回 Worker 并发度。
func (m *Manager) concurrency() int {
	if m.CurrentConfig().Concurrency > 0 {
		return m.CurrentConfig().Concurrency
	}
	return 10
}

// defaultRetry 返回任务最终使用的重试次数。
func (m *Manager) defaultRetry(override *int) int {
	if override != nil {
		return *override
	}
	if m.CurrentConfig().DefaultRetry > 0 {
		return m.CurrentConfig().DefaultRetry
	}
	return 3
}

// defaultTimeout 返回任务最终使用的超时时间。
func (m *Manager) defaultTimeout(override *int) time.Duration {
	if override != nil && *override > 0 {
		return time.Duration(*override) * time.Second
	}
	if m.CurrentConfig().DefaultTimeoutSeconds > 0 {
		return time.Duration(m.CurrentConfig().DefaultTimeoutSeconds) * time.Second
	}
	return 2 * time.Minute
}

// uniqueTTL 返回任务唯一键的去重窗口时长。
func (m *Manager) uniqueTTL(override *int) time.Duration {
	if override != nil && *override > 0 {
		return time.Duration(*override) * time.Second
	}
	if m.CurrentConfig().DefaultUniqueTTLSeconds > 0 {
		return time.Duration(m.CurrentConfig().DefaultUniqueTTLSeconds) * time.Second
	}
	return 0
}

// shutdownTimeout 返回 Worker 关闭时的优雅退出等待时长。
func (m *Manager) shutdownTimeout() time.Duration {
	if m.CurrentConfig().ShutdownTimeoutSeconds > 0 {
		return time.Duration(m.CurrentConfig().ShutdownTimeoutSeconds) * time.Second
	}
	return 15 * time.Second
}

// groupGracePeriod 返回聚合分组收尾等待时间。
func (m *Manager) groupGracePeriod() time.Duration {
	if m.CurrentConfig().GroupGracePeriodSeconds > 0 {
		return time.Duration(m.CurrentConfig().GroupGracePeriodSeconds) * time.Second
	}
	return 5 * time.Second
}

// groupMaxDelay 返回聚合任务允许等待的最长窗口。
func (m *Manager) groupMaxDelay() time.Duration {
	if m.CurrentConfig().GroupMaxDelaySeconds > 0 {
		return time.Duration(m.CurrentConfig().GroupMaxDelaySeconds) * time.Second
	}
	return 20 * time.Second
}

// groupMaxSize 返回单个聚合任务可容纳的最大条数。
func (m *Manager) groupMaxSize() int {
	if m.CurrentConfig().GroupMaxSize > 0 {
		return m.CurrentConfig().GroupMaxSize
	}
	return 100
}

// delayedTaskCheckInterval 返回 Asynq 检查延迟任务的轮询间隔。
func (m *Manager) delayedTaskCheckInterval() time.Duration {
	if m.CurrentConfig().DelayedTaskCheckSeconds > 0 {
		return time.Duration(m.CurrentConfig().DelayedTaskCheckSeconds) * time.Second
	}
	return 5 * time.Second
}

// taskCheckInterval 返回 Worker 普通任务轮询间隔。
func (m *Manager) taskCheckInterval() time.Duration {
	if m.CurrentConfig().TaskCheckSeconds > 0 {
		return time.Duration(m.CurrentConfig().TaskCheckSeconds) * time.Second
	}
	return time.Second
}

// schedulerEnabled 判断是否需要启动周期调度模块。
func (m *Manager) schedulerEnabled() bool {
	return m.CurrentConfig().Scheduler.Enabled
}

// allPeriodicTasks 合并配置文件和插件注册的周期任务，并去重输出。
func (m *Manager) allPeriodicTasks() []config.TaskPeriodicConfig {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg := m.CurrentConfig()
	items := make([]config.TaskPeriodicConfig, 0, len(cfg.Periodic)+len(m.periodic))
	items = append(items, cfg.Periodic...)
	items = append(items, m.periodic...)
	return dedupePeriodicTasks(enabledPeriodicTasks(items))
}

// periodicTaskCount 返回当前有效周期任务数量。
func (m *Manager) periodicTaskCount() int {
	if m == nil {
		return 0
	}
	return len(m.allPeriodicTasks())
}

// schedulerLeaseKey 返回调度 leader 竞争锁的 Redis key。
func (m *Manager) schedulerLeaseKey() string {
	return m.schedulerLeaseKeyByConfig(m.CurrentConfig())
}

// schedulerLeaseKeyByConfig 根据给定配置计算调度 leader 锁的 Redis key。
func (m *Manager) schedulerLeaseKeyByConfig(cfg config.TaskQueueConfig) string {
	return keys.TaskSchedulerLeaderRedisKey(cfg.AppID, cfg.Scheduler.LeaseKey)
}

// schedulerLeaseTTL 返回调度 leader 锁租约时长。
func (m *Manager) schedulerLeaseTTL() time.Duration {
	return m.schedulerLeaseTTLByConfig(m.CurrentConfig())
}

// schedulerLeaseTTLByConfig 根据给定配置返回调度 leader 锁租约时长。
func (m *Manager) schedulerLeaseTTLByConfig(cfg config.TaskQueueConfig) time.Duration {
	if cfg.Scheduler.LeaseTTLSeconds > 0 {
		return time.Duration(cfg.Scheduler.LeaseTTLSeconds) * time.Second
	}
	return 30 * time.Second
}

// schedulerRenewInterval 返回调度 leader 锁续租间隔。
func (m *Manager) schedulerRenewInterval() time.Duration {
	return m.schedulerRenewIntervalByConfig(m.CurrentConfig())
}

// schedulerRenewIntervalByConfig 根据给定配置返回调度 leader 锁续租间隔。
func (m *Manager) schedulerRenewIntervalByConfig(cfg config.TaskQueueConfig) time.Duration {
	if cfg.Scheduler.RenewIntervalSeconds > 0 {
		return time.Duration(cfg.Scheduler.RenewIntervalSeconds) * time.Second
	}
	return 10 * time.Second
}

// schedulerSyncInterval 返回周期任务配置同步间隔。
func (m *Manager) schedulerSyncInterval() time.Duration {
	return m.schedulerSyncIntervalByConfig(m.CurrentConfig())
}

// schedulerSyncIntervalByConfig 根据给定配置返回周期任务配置同步间隔。
func (m *Manager) schedulerSyncIntervalByConfig(cfg config.TaskQueueConfig) time.Duration {
	if cfg.Scheduler.SyncIntervalSeconds > 0 {
		return time.Duration(cfg.Scheduler.SyncIntervalSeconds) * time.Second
	}
	return time.Minute
}

// schedulerHeartbeatInterval 返回调度器心跳上报间隔。
func (m *Manager) schedulerHeartbeatInterval() time.Duration {
	return m.schedulerHeartbeatIntervalByConfig(m.CurrentConfig())
}

// schedulerHeartbeatIntervalByConfig 根据给定配置返回调度器心跳上报间隔。
func (m *Manager) schedulerHeartbeatIntervalByConfig(cfg config.TaskQueueConfig) time.Duration {
	if cfg.Scheduler.HeartbeatIntervalSeconds > 0 {
		return time.Duration(cfg.Scheduler.HeartbeatIntervalSeconds) * time.Second
	}
	return 10 * time.Second
}

// schedulerMaxQueueBacklog 返回周期任务投递前允许的队列积压上限，0 表示不启用背压。
func (m *Manager) schedulerMaxQueueBacklog() int64 {
	if m == nil {
		return 0
	}
	limit := m.CurrentConfig().Scheduler.MaxQueueBacklog
	if limit <= 0 {
		return 0
	}
	return int64(limit)
}

// workflowTriggerUniqueReservationTTL 计算触发请求在排队延迟场景下需要额外预留的幂等窗口。
func (m *Manager) workflowTriggerUniqueReservationTTL(req *types.TriggerTaskWorkflowReq, uniqueTTL time.Duration, now time.Time) (time.Duration, error) {
	if req == nil || uniqueTTL <= 0 {
		return uniqueTTL, nil
	}
	delay := time.Duration(0)
	if req.ProcessAt != "" {
		// processAt 使用 RFC3339 绝对时间，解析失败要尽早返回给 API 调用方，而不是默默降级成即时任务。
		processAt, err := time.Parse(time.RFC3339, req.ProcessAt)
		if err != nil {
			return 0, errors.Wrap(err, "解析 workflow processAt 失败")
		}
		if processAt.After(now) {
			delay = processAt.Sub(now)
		}
	}
	if delay <= 0 && req.ProcessInSeconds != nil && *req.ProcessInSeconds > 0 {
		delay = time.Duration(*req.ProcessInSeconds) * time.Second
	}
	return uniqueTTL + delay, nil
}

// periodicTaskKey 生成周期任务的稳定去重键。
func periodicTaskKey(cfg config.TaskPeriodicConfig) (string, error) {
	cron, err := periodicTaskCronspec(cfg)
	if err != nil {
		return "", errors.Tag(err)
	}
	workflow := strings.TrimSpace(cfg.Workflow)
	if workflow == "" {
		return "", errors.Errorf("周期任务 workflow 不能为空")
	}
	if name := strings.TrimSpace(cfg.Name); name != "" {
		return "name:" + name, nil
	}
	return fmt.Sprintf("cron:%s|workflow:%s|queue:%s", cron, workflow, strings.TrimSpace(cfg.Queue)), nil
}

// PeriodicTaskConfigKey 返回周期任务的稳定去重键，供 bootstrap 配置合并阶段复用。
// 该键与调度器注册阶段保持一致，避免外部配置文件加载成功但运行期才暴露重复任务。
func PeriodicTaskConfigKey(cfg config.TaskPeriodicConfig) (string, error) {
	return periodicTaskKey(cfg)
}

// enabledPeriodicTasks 过滤显式关闭的周期任务；未配置 enabled 时保持兼容默认启用。
func enabledPeriodicTasks(items []config.TaskPeriodicConfig) []config.TaskPeriodicConfig {
	result := make([]config.TaskPeriodicConfig, 0, len(items))
	for _, item := range items {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		result = append(result, item)
	}
	return result
}

// dedupePeriodicTasks 按稳定键去重周期任务配置。
func dedupePeriodicTasks(items []config.TaskPeriodicConfig) []config.TaskPeriodicConfig {
	result := make([]config.TaskPeriodicConfig, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key, err := periodicTaskKey(item)
		if err != nil {
			result = append(result, item)
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

// normalizeStrings 去空、去重并排序字符串切片。
func normalizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

// newID 生成新的全局唯一标识。
func newID() string { return uuid.NewString() }

// mustJSON 把任意值序列化为 JSON 字符串；调用方默认传入可序列化结构。
func mustJSON(value any) string { return string(mustJSONBytes(value)) }

// mustJSONBytes 把任意值序列化为 JSON 字节切片。
func mustJSONBytes(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

// toInt 尝试把 Redis 读取值或头信息转换为 int。
func toInt(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		return i
	default:
		return 0
	}
}

// toInt64 尝试把 Redis 读取值或 JSON 字段转换为 int64。
func toInt64(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, _ := v.Int64()
		return parsed
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i
	default:
		return 0
	}
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

// clampPercent 把灰度百分比收敛到 1~100 的有效区间。
func clampPercent(percent int) int {
	if percent <= 0 {
		return 100
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// toTaskItem 把 Asynq 任务详情转换成对外返回结构。
// 返回前把内部队列名和聚合分组还原成逻辑名称。
func (m *Manager) toTaskItem(ctx context.Context, info *asynq.TaskInfo) types.TaskItem {
	return m.toTaskItemWithPeriodicNextRuns(ctx, info, nil)
}

// toTaskItemWithPeriodicNextRuns 在任务基础字段之外补齐周期任务下一次触发时间。
func (m *Manager) toTaskItemWithPeriodicNextRuns(ctx context.Context, info *asynq.TaskInfo, periodicNextRuns []periodicNextRun) types.TaskItem {
	if info == nil {
		return types.TaskItem{}
	}
	runtimeRecord := m.readTaskRuntime(ctx, info.ID)
	startedAt := runtimeRecord.StartedAt
	if startedAt == "" {
		startedAt = taskStartedAtFromResult(info.Result)
	}
	durationMS := runtimeRecord.DurationMS
	if durationMS <= 0 && startedAt != "" && info.State == asynq.TaskStateActive {
		if parsedStartedAt, err := time.Parse(time.RFC3339, startedAt); err == nil {
			durationMS = time.Since(parsedStartedAt).Milliseconds()
		}
	}
	if durationMS <= 0 {
		durationMS = taskDurationFromResult(info.Result)
	}
	executionTrace := runtimeRecord.ExecutionTrace
	if executionTrace == nil {
		executionTrace = taskExecutionStatsFromResult(info.Result)
	}
	item := types.TaskItem{
		ID:             info.ID,
		Queue:          m.displayQueueName(info.Queue),
		TaskType:       info.Type,
		TaskName:       taskInfoDisplayName(info),
		WorkflowID:     strings.TrimSpace(info.Headers[headerWorkflowID]),
		State:          info.State.String(),
		Group:          m.displayGroupName(info.Group),
		MaxRetry:       info.MaxRetry,
		Retried:        info.Retried,
		LastErr:        info.LastErr,
		TimeoutSec:     int64(info.Timeout.Seconds()),
		StartedAt:      startedAt,
		DurationMS:     durationMS,
		Deadline:       formatTime(info.Deadline),
		NextProcessAt:  formatTime(info.NextProcessAt),
		CompletedAt:    formatTime(info.CompletedAt),
		LastFailedAt:   formatTime(info.LastFailedAt),
		IsOrphaned:     info.IsOrphaned,
		Headers:        info.Headers,
		Payload:        append([]byte(nil), info.Payload...),
		Result:         append([]byte(nil), info.Result...),
		ExecutionTrace: executionTrace,
	}
	if item.NextProcessAt == "" {
		item.NextProcessAt = periodicNextProcessAtForTaskItem(item, periodicNextRuns)
	}
	return item
}

// queueInfoTotalByState 根据任务状态返回队列中对应的统计总数。
func queueInfoTotalByState(info *asynq.QueueInfo, state string) int64 {
	switch state {
	case "pending":
		return int64(info.Pending)
	case "active":
		return int64(info.Active)
	case "scheduled":
		return int64(info.Scheduled)
	case "retry":
		return int64(info.Retry)
	case "archived":
		return int64(info.Archived)
	case "completed":
		return int64(info.Completed)
	case "aggregating":
		return int64(info.Aggregating)
	default:
		return int64(info.Size)
	}
}

// formatTime 把零值时间转换为空串，其他时间统一格式化为 RFC3339。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// durationBetweenRFC3339 计算两个 RFC3339 时间之间的耗时，结束时间为空时按当前时间计算。
func durationBetweenRFC3339(startText string, endText string) int64 {
	startText = strings.TrimSpace(startText)
	if startText == "" {
		return 0
	}
	start, err := time.Parse(time.RFC3339, startText)
	if err != nil {
		return 0
	}
	end := time.Now()
	if strings.TrimSpace(endText) != "" {
		parsedEnd, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(endText))
		if parseErr != nil {
			return 0
		}
		end = parsedEnd
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
