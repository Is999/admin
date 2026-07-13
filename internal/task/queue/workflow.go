package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"admin/helper"
	"admin/internal/task/stats"
	"admin/internal/task/taskwire"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// WorkflowNameCacheRefresh 是内置的缓存刷新工作流名称。
	WorkflowNameCacheRefresh = "cache.refresh"

	// WorkflowSourceAPI 表示由管理接口手动触发。
	WorkflowSourceAPI = taskwire.WorkflowSourceAPI
	// WorkflowSourcePeriodic 表示由周期调度触发。
	WorkflowSourcePeriodic = taskwire.WorkflowSourcePeriodic
	// WorkflowSourceInternal 表示由应用内部逻辑触发。
	WorkflowSourceInternal = taskwire.WorkflowSourceInternal

	// WorkflowStatusPending 表示工作流已创建但尚未开始。
	WorkflowStatusPending = "pending"
	// WorkflowStatusRunning 表示工作流正在执行。
	WorkflowStatusRunning = "running"
	// WorkflowStatusSuccess 表示工作流全部成功完成。
	WorkflowStatusSuccess = "success"
	// WorkflowStatusFailed 表示工作流执行失败。
	WorkflowStatusFailed = "failed"

	// NodeStatusPending 表示节点尚未调度。
	NodeStatusPending = "pending"
	// NodeStatusRunning 表示节点已调度且仍在执行。
	NodeStatusRunning = "running"
	// NodeStatusSuccess 表示节点全部实例执行成功。
	NodeStatusSuccess = "success"
	// NodeStatusFailed 表示节点存在终态失败实例。
	NodeStatusFailed = "failed"
	// NodeStatusSkipped 表示节点因灰度被整体跳过。
	NodeStatusSkipped = "skipped"

	// workflowNodeInstanceFieldPrefix 是节点 hash 内部的分片结果状态字段前缀。
	// 结果状态与成功/失败计数放在同一个 Redis hash 中，避免 Redis Cluster Lua 跨 slot 写多 key。
	workflowNodeInstanceFieldPrefix = "__instance_done:"
	// workflowNodeTraceFieldPrefix 是节点 hash 内部的分片处理量快照字段前缀。
	workflowNodeTraceFieldPrefix = "executionTrace:shard:"
	// workflowNodeBusinessFailureFieldPrefix 是节点 hash 内部的业务失败次数前缀。
	workflowNodeBusinessFailureFieldPrefix = "__business_failures:"
	// workflowNodeOutcomeRerunPrepared 表示失败分片已完成重跑准备，可安全重复调用 RunTask。
	workflowNodeOutcomeRerunPrepared = "rerun_prepared"
	// workflowRepairRetryCount 是根节点或下游分片投递失败时预留的编排重试次数。
	workflowRepairRetryCount = 3
)

// workflowNodeInstanceField 返回节点 hash 中单个分片的结果状态字段名。
func workflowNodeInstanceField(shardIndex int) string {
	return fmt.Sprintf("%s%d", workflowNodeInstanceFieldPrefix, shardIndex)
}

// workflowNodeTraceField 返回节点 hash 中单个分片的处理量快照字段名。
func workflowNodeTraceField(shardIndex int) string {
	return fmt.Sprintf("%s%d", workflowNodeTraceFieldPrefix, shardIndex)
}

// workflowNodeBusinessFailureField 返回单个分片的业务失败次数 Redis 字段名。
func workflowNodeBusinessFailureField(shardIndex int) string {
	return fmt.Sprintf("%s%d", workflowNodeBusinessFailureFieldPrefix, shardIndex)
}

// WorkflowStartSpec 表示工作流实例的启动参数。
type WorkflowStartSpec struct {
	WorkflowID        string        // 工作流实例 ID，留空时自动生成
	Name              string        // 工作流定义名称
	Queue             string        // 执行队列，留空时走默认队列
	Targets           []string      // 工作流处理目标集合
	ShardTotal        int           // 分片总数
	GrayPercent       int           // 灰度执行百分比
	Source            string        // 触发来源
	PeriodicName      string        // 周期任务原始名称
	ScheduledAt       string        // 周期任务本轮计划触发时间，RFC3339
	TriggeredByUserID int           // 触发人用户 ID
	TriggeredByUser   string        // 触发人用户名
	TraceID           string        // 上游链路追踪 ID
	RetryOverride     *int          // 节点默认重试次数覆盖值
	TimeoutOverride   time.Duration // 节点默认超时时间覆盖值
	UniqueKey         string        // 幂等键
	UniqueTTL         time.Duration // 幂等锁有效期
}

// WorkflowDefinition 描述一个 DAG 工作流定义。
// 节点图在进程启动时注册到内存中，实例状态则落在 Redis 中以支持多实例协同。
type WorkflowDefinition struct {
	Name                     string                             // 工作流名称
	Description              string                             // 工作流默认说明，注册表展示优先使用多语言展示元数据
	DefaultQueue             string                             // 默认执行队列
	MaxShardTotal            int                                // 允许的最大分片数，0 表示不限制
	PeriodicUniqueBySchedule bool                               // 周期触发时是否按计划时刻隔离幂等键，避免延迟执行吞掉下一周期实例
	Nodes                    map[string]*WorkflowNodeDefinition // 节点定义集合，key 为节点名
}

// WorkflowNodeDefinition 描述 DAG 中的单个节点。
// 节点负责声明执行任务类型、依赖关系、重试/超时策略，以及是否支持分片和灰度。
type WorkflowNodeDefinition struct {
	Name             string                                                                                                 // 节点名称，作为工作流内唯一标识
	TaskType         string                                                                                                 // 对应的 Asynq 任务类型
	Queue            string                                                                                                 // 节点执行队列，留空时回退工作流默认队列
	DependsOn        []string                                                                                               // 依赖的上游节点列表，只有依赖全部完成后才会调度
	MaxRetry         int                                                                                                    // 节点最大重试次数，0 表示不自动重试，负数表示禁止工作流全局重试覆盖
	Timeout          time.Duration                                                                                          // 单个节点任务的超时时间
	SupportsSharding bool                                                                                                   // 是否支持按目标集合切成多个分片并行执行
	SupportsGray     bool                                                                                                   // 是否支持按灰度百分比跳过部分分片
	BuildPayload     func(spec WorkflowStartSpec, node *WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) // 根据启动参数和分片信息构造节点任务负载
}

// WorkflowTaskMeta 是写入任务 payload 的工作流元数据。
// 任务成功/失败时会依赖这些字段回写节点状态。
type WorkflowTaskMeta struct {
	WorkflowID   string `json:"workflowId,omitempty"`   // 工作流实例 ID
	WorkflowName string `json:"workflowName,omitempty"` // 工作流名称
	WorkflowNode string `json:"workflowNode,omitempty"` // 当前节点名称
	ShardIndex   int    `json:"shardIndex,omitempty"`   // 当前分片索引
	ShardTotal   int    `json:"shardTotal,omitempty"`   // 分片总数
}

// WorkflowMetaRecord 是工作流实例在 Redis 中保存的主记录。
type WorkflowMetaRecord struct {
	WorkflowID   string   `json:"workflowId"`             // 工作流实例 ID
	WorkflowName string   `json:"workflowName"`           // 工作流名称
	Status       string   `json:"status"`                 // 当前整体状态
	Source       string   `json:"source"`                 // 触发来源
	Queue        string   `json:"queue"`                  // 默认执行队列
	Targets      []string `json:"targets,omitempty"`      // 目标集合
	ShardTotal   int      `json:"shardTotal"`             // 分片总数
	GrayPercent  int      `json:"grayPercent"`            // 灰度百分比
	ErrorMessage string   `json:"errorMessage,omitempty"` // 失败原因摘要
	CreatedAt    string   `json:"createdAt"`              // 创建时间
	UpdatedAt    string   `json:"updatedAt"`              // 最近更新时间
	FinishedAt   string   `json:"finishedAt,omitempty"`   // 完成时间
}

// WorkflowNodeRecord 是节点状态在 Redis 中的快照结构。
type WorkflowNodeRecord struct {
	Name           string                       `json:"name"`                     // 节点名称
	TaskType       string                       `json:"taskType"`                 // 节点任务类型
	Queue          string                       `json:"queue"`                    // 节点队列
	Status         string                       `json:"status"`                   // 当前节点状态
	DependsOn      []string                     `json:"dependsOn,omitempty"`      // 依赖节点列表
	Expected       int                          `json:"expected"`                 // 预期执行实例数
	Succeeded      int                          `json:"succeeded"`                // 已成功实例数
	Failed         int                          `json:"failed"`                   // 已失败实例数
	Skipped        int                          `json:"skipped"`                  // 已跳过实例数
	ErrorMessage   string                       `json:"errorMessage,omitempty"`   // 节点错误摘要
	StartedAt      string                       `json:"startedAt,omitempty"`      // 首次开始时间
	FinishedAt     string                       `json:"finishedAt,omitempty"`     // 完成时间
	Progress       *taskstats.Progress          `json:"progress,omitempty"`       // 节点实例执行进度
	ExecutionTrace *taskstats.Snapshot          `json:"executionTrace,omitempty"` // 节点处理量聚合摘要
	ShardTraces    []WorkflowNodeShardTraceItem `json:"shardTraces,omitempty"`    // 分片处理量明细
}

// WorkflowNodeShardTraceItem 表示工作流节点中单个分片的处理量快照。
type WorkflowNodeShardTraceItem struct {
	ShardIndex     int                 `json:"shardIndex"`               // 分片下标
	ShardTotal     int                 `json:"shardTotal"`               // 分片总数
	Status         string              `json:"status,omitempty"`         // 分片状态
	Progress       *taskstats.Progress `json:"progress,omitempty"`       // 分片执行进度
	ExecutionTrace *taskstats.Snapshot `json:"executionTrace,omitempty"` // 分片处理量摘要
}

// startWorkflow 创建工作流实例元数据，并调度所有根节点。
func (m *Manager) startWorkflow(ctx context.Context, spec WorkflowStartSpec) (string, error) {
	if m == nil || !m.IsEnabled() {
		return "", ErrTaskQueueDisabled
	}
	def, err := m.workflowDefinition(spec.Name)
	if err != nil {
		return "", errors.Tag(err)
	}

	spec.Name = def.Name
	spec.Targets = normalizeStrings(spec.Targets)
	if spec.ShardTotal <= 0 {
		spec.ShardTotal = 1
	}
	if def.MaxShardTotal > 0 && spec.ShardTotal > def.MaxShardTotal {
		return "", errors.Errorf("工作流分片数超过上限 workflow=%s shard_total=%d max=%d", def.Name, spec.ShardTotal, def.MaxShardTotal)
	}
	if spec.GrayPercent <= 0 || spec.GrayPercent > 100 {
		spec.GrayPercent = 100
	}
	if spec.Queue == "" {
		spec.Queue = helper.FirstNonEmptyString(def.DefaultQueue, m.defaultWorkflowQueue())
	}
	if spec.WorkflowID == "" {
		spec.WorkflowID = newID()
	}
	if spec.Source == "" {
		spec.Source = WorkflowSourceInternal
	}
	if err = applyPeriodicScheduleUniqueKey(def, &spec); err != nil {
		return "", errors.Tag(err)
	}
	metaKey := m.workflowMetaKey(spec.WorkflowID)
	exists, err := m.redis.Exists(ctx, metaKey).Result()
	if err != nil {
		return "", errors.Wrap(err, "检查工作流是否存在失败")
	}
	if exists > 0 {
		complete, completeErr := m.workflowMetadataComplete(ctx, spec.WorkflowID, def)
		if completeErr != nil {
			return "", errors.Tag(completeErr)
		}
		if complete {
			status, statusErr := m.redis.HGet(ctx, metaKey, "status").Result()
			if statusErr != nil {
				return "", errors.Wrap(statusErr, "读取工作流状态失败")
			}
			if status == WorkflowStatusSuccess || status == WorkflowStatusFailed {
				return spec.WorkflowID, nil
			}
			if status == WorkflowStatusPending {
				if statusErr = m.updateWorkflowMeta(ctx, spec.WorkflowID, map[string]any{"status": WorkflowStatusRunning}); statusErr != nil {
					return "", errors.Wrap(statusErr, "恢复工作流运行状态失败")
				}
			}
			storedSpec, specErr := m.workflowSpecByID(ctx, spec.WorkflowID)
			if specErr != nil {
				return "", errors.Tag(specErr)
			}
			for _, nodeName := range sortedNodeNames(def) {
				if scheduleErr := m.scheduleNode(ctx, def, storedSpec, nodeName); scheduleErr != nil {
					return "", errors.Tag(scheduleErr)
				}
			}
			return spec.WorkflowID, nil
		}
		// Redis 异常可能留下只有 meta、缺少 nodes/node hash 的半初始化数据。
		// 这里先按精确 key 清理，再重建完整工作流元数据，避免后续误判工作流已存在但无法推进。
		if err = m.cleanupIncompleteWorkflowMetadata(ctx, spec.WorkflowID, def); err != nil {
			return "", errors.Tag(err)
		}
	}
	if spec.UniqueKey != "" && spec.UniqueTTL > 0 {
		locked, lockErr := m.ensureWorkflowUnique(ctx, spec.Name, spec.UniqueKey, spec.WorkflowID, spec.UniqueTTL)
		if lockErr != nil {
			return "", errors.Wrap(lockErr, "锁定工作流唯一键失败")
		}
		if !locked {
			return spec.WorkflowID, ErrWorkflowAlreadyExists
		}
	}

	nodeNames := sortedNodeNames(def)
	now := time.Now().Format(time.RFC3339)
	// Redis Cluster 下 MULTI/EXEC 不能跨 slot；工作流 meta、nodes、node hash key 不保证同 slot。
	// 初始化写入使用普通 pipeline，由完整性检查负责清理并重建极端情况下的半初始化数据。
	pipe := m.redis.Pipeline()
	metaValues := map[string]any{
		"workflowId":       spec.WorkflowID,
		"workflowName":     spec.Name,
		"status":           WorkflowStatusPending,
		"source":           spec.Source,
		"periodicName":     spec.PeriodicName,
		"scheduledAt":      spec.ScheduledAt,
		"queue":            spec.Queue,
		"targets":          mustJSON(spec.Targets),
		"shardTotal":       spec.ShardTotal,
		"grayPercent":      spec.GrayPercent,
		"uniqueKey":        spec.UniqueKey,
		"uniqueTTLSeconds": int64(spec.UniqueTTL / time.Second),
		"errorMessage":     "",
		"createdAt":        now,
		"updatedAt":        now,
		"finishedAt":       "",
	}
	if spec.RetryOverride != nil {
		metaValues["retryOverride"] = *spec.RetryOverride
	}
	if spec.TimeoutOverride > 0 {
		metaValues["timeoutOverrideSeconds"] = int64(spec.TimeoutOverride / time.Second)
	}
	pipe.HSet(ctx, metaKey, metaValues)
	for _, nodeName := range nodeNames {
		node := def.Nodes[nodeName]
		pipe.SAdd(ctx, m.workflowNodesKey(spec.WorkflowID), nodeName)
		nodeKey := m.workflowNodeKey(spec.WorkflowID, nodeName)
		queue := helper.FirstNonEmptyString(node.Queue, spec.Queue)
		pipe.HSet(ctx, nodeKey,
			"name", nodeName,
			"taskType", node.TaskType,
			"queue", queue,
			"status", NodeStatusPending,
			"dependsOn", mustJSON(node.DependsOn),
			"expected", 0,
			"succeeded", 0,
			"failed", 0,
			"skipped", 0,
			"errorMessage", "",
			"startedAt", "",
			"finishedAt", "",
		)
	}
	if _, err = pipe.Exec(ctx); err != nil {
		return "", errors.Wrap(err, "保存工作流元数据失败")
	}

	if err = m.updateWorkflowMeta(ctx, spec.WorkflowID, map[string]any{"status": WorkflowStatusRunning, "updatedAt": now}); err != nil {
		return "", errors.Tag(err)
	}
	for _, nodeName := range rootNodeNames(def) {
		if err = m.scheduleNode(ctx, def, spec, nodeName); err != nil {
			return "", errors.Tag(err)
		}
	}
	return spec.WorkflowID, nil
}

// workflowUniqueKeyForSchedule 把周期实例的计划时刻加入幂等键，同一时刻防重、相邻周期互不吞任务。
func workflowUniqueKeyForSchedule(baseKey, scheduledAt string) (string, error) {
	baseKey = strings.TrimSpace(baseKey)
	scheduledAt = strings.TrimSpace(scheduledAt)
	if baseKey == "" {
		return "", errors.Errorf("周期工作流幂等键不能为空")
	}
	parsed, err := time.Parse(time.RFC3339, scheduledAt)
	if err != nil {
		return "", errors.Wrap(err, "解析周期工作流计划触发时间失败")
	}
	if parsed.Nanosecond() != 0 {
		return "", errors.Errorf("周期工作流计划触发时间必须按整秒对齐: %s", scheduledAt)
	}
	return baseKey + ":" + strconv.FormatInt(parsed.Unix(), 10), nil
}

// applyPeriodicScheduleUniqueKey 仅为声明该语义的周期工作流加入计划时刻后缀。
func applyPeriodicScheduleUniqueKey(def *WorkflowDefinition, spec *WorkflowStartSpec) error {
	if def == nil || spec == nil || !def.PeriodicUniqueBySchedule || spec.Source != WorkflowSourcePeriodic || spec.UniqueKey == "" || spec.UniqueTTL <= 0 {
		return nil
	}
	uniqueKey, err := workflowUniqueKeyForSchedule(spec.UniqueKey, spec.ScheduledAt)
	if err != nil {
		return errors.Tag(err)
	}
	spec.UniqueKey = uniqueKey
	return nil
}

// scheduleNode 在依赖满足后调度指定节点，并按分片策略拆分为多个任务实例。
func (m *Manager) scheduleNode(ctx context.Context, def *WorkflowDefinition, spec WorkflowStartSpec, nodeName string) error {
	if err := m.scheduleNodeTasks(ctx, def, spec, nodeName); err != nil {
		return errors.Join(ErrWorkflowDispatch, err)
	}
	return nil
}

// scheduleNodeTasks 执行单个节点的状态初始化与分片投递。
func (m *Manager) scheduleNodeTasks(ctx context.Context, def *WorkflowDefinition, spec WorkflowStartSpec, nodeName string) error {
	node := def.Nodes[nodeName]
	if node == nil {
		return errors.Errorf("节点不存在: %s", nodeName)
	}
	ready, err := m.nodeReady(ctx, spec.WorkflowID, node.DependsOn)
	if err != nil {
		return errors.Tag(err)
	}
	if !ready {
		return nil
	}

	nodeKey := m.workflowNodeKey(spec.WorkflowID, nodeName)
	status, err := m.redis.HGet(ctx, nodeKey, "status").Result()
	if err != nil && err != redis.Nil {
		return errors.Wrap(err, "读取节点调度状态失败")
	}
	if status == NodeStatusSuccess || status == NodeStatusSkipped || status == NodeStatusFailed {
		return nil
	}

	queue := helper.FirstNonEmptyString(node.Queue, spec.Queue, m.defaultWorkflowQueue())
	selectedShards := selectedShardIndexes(spec.WorkflowID, nodeName, effectiveShardTotal(node, spec), spec.GrayPercent, node.SupportsGray)
	if status == "" || status == NodeStatusPending {
		now := time.Now().Format(time.RFC3339)
		updates := map[string]any{
			"queue":     queue,
			"status":    NodeStatusRunning,
			"expected":  len(selectedShards),
			"startedAt": now,
			"updatedAt": now,
		}
		if len(selectedShards) == 0 {
			updates["status"] = NodeStatusSkipped
			updates["skipped"] = effectiveShardTotal(node, spec)
			updates["finishedAt"] = now
		}
		if err = m.updateWorkflowNode(ctx, spec.WorkflowID, nodeName, updates); err != nil {
			return errors.Tag(err)
		}
		if len(selectedShards) == 0 {
			return m.onNodeCompleted(ctx, def, spec, nodeName)
		}
	}
	instanceFields := make([]string, 0, len(selectedShards))
	for _, shardIndex := range selectedShards {
		instanceFields = append(instanceFields, workflowNodeInstanceField(shardIndex))
	}
	instanceOutcomes, err := m.redis.HMGet(ctx, nodeKey, instanceFields...).Result()
	if err != nil {
		return errors.Wrap(err, "读取节点分片终态失败")
	}

	for index, shardIndex := range selectedShards {
		if outcome, _ := instanceOutcomes[index].(string); outcome == "succeeded" {
			continue
		}
		businessRetry := effectiveRetry(node, spec)
		payload, buildErr := node.BuildPayload(spec, node, shardIndex, effectiveShardTotal(node, spec))
		if buildErr != nil {
			return errors.Wrapf(buildErr, "构建节点负载失败 node=%s shard=%d", nodeName, shardIndex)
		}
		headers := map[string]string{
			headerTaskName:      workflowNodeTaskName(spec.Name, nodeName, node.TaskType),
			headerWorkflowID:    spec.WorkflowID,
			headerWorkflowName:  spec.Name,
			headerWorkflowNode:  nodeName,
			headerShardIndex:    fmt.Sprintf("%d", shardIndex),
			headerShardTotal:    fmt.Sprintf("%d", effectiveShardTotal(node, spec)),
			headerBusinessRetry: strconv.Itoa(businessRetry),
		}
		if spec.PeriodicName != "" {
			headers[HeaderPeriodicName] = spec.PeriodicName
			headers[headerTaskSource] = spec.Source
		}
		if spec.ScheduledAt != "" {
			headers[HeaderScheduledAt] = spec.ScheduledAt
		}
		task := m.newTask(ctx, node.TaskType, payload, headers)
		maxRetry := workflowTaskRetryBudget(businessRetry)
		if node.MaxRetry < 0 {
			maxRetry = 0
		}
		opts := []asynq.Option{
			asynq.Queue(m.namespacedQueueName(queue)),
			asynq.MaxRetry(maxRetry),
			asynq.TaskID(workflowNodeTaskID(spec.WorkflowID, nodeName, shardIndex)),
			asynq.Retention(taskCompletedRetention),
		}
		if timeout := effectiveTimeout(node, spec); timeout > 0 {
			opts = append(opts, asynq.Timeout(timeout))
		}
		if _, err = m.client.EnqueueContext(ctx, task, opts...); err != nil {
			if errors.Is(err, asynq.ErrTaskIDConflict) {
				continue
			}
			return errors.Wrapf(err, "投递节点任务失败 node=%s shard=%d", nodeName, shardIndex)
		}
	}
	return nil
}

// workflowTaskSettled 判断工作流或分片是否已终结，技术重试据此跳过重复业务执行。
func (m *Manager) workflowTaskSettled(ctx context.Context, task *asynq.Task) (bool, error) {
	meta := workflowTaskMetaFromTask(task)
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return false, nil
	}
	status, err := m.redis.HGet(ctx, m.workflowMetaKey(meta.WorkflowID), "status").Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, errors.Join(ErrWorkflowDispatch, errors.Wrapf(err, "读取工作流终态失败 workflow_id=%s", meta.WorkflowID))
	}
	if status == WorkflowStatusFailed {
		outcome, outcomeErr := m.redis.HGet(ctx, m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode), workflowNodeInstanceField(meta.ShardIndex)).Result()
		if outcomeErr != nil && outcomeErr != redis.Nil {
			return false, errors.Join(ErrWorkflowDispatch, errors.Wrapf(outcomeErr, "读取失败工作流分片状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex))
		}
		switch outcome {
		case "succeeded", "failed":
			return true, nil
		case workflowNodeOutcomeRerunPrepared:
			return false, errors.Wrap(asynq.SkipRetry, "手工重跑期间工作流已再次失败")
		default:
			return false, errors.Wrap(asynq.SkipRetry, "工作流已失败，未执行分片转入归档")
		}
	}
	outcome, err := m.redis.HGet(ctx, m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode), workflowNodeInstanceField(meta.ShardIndex)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, errors.Join(ErrWorkflowDispatch, errors.Wrapf(err, "读取工作流分片终态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex))
	}
	return outcome == "succeeded", nil
}

// applyWorkflowBusinessRetryLimit 按实际业务失败次数收口重试，编排补投失败不占用业务预算。
func (m *Manager) applyWorkflowBusinessRetryLimit(ctx context.Context, task *asynq.Task, runErr error) error {
	if runErr == nil || errors.Is(runErr, asynq.SkipRetry) || errors.Is(runErr, asynq.RevokeTask) {
		return runErr
	}
	meta := workflowTaskMetaFromTask(task)
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return runErr
	}
	businessRetry := max(toInt(task.Headers()[headerBusinessRetry]), 0)
	nodeKey := m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode)
	failures, err := m.redis.HIncrBy(ctx, nodeKey, workflowNodeBusinessFailureField(meta.ShardIndex), 1).Result()
	if err != nil {
		return errors.Join(ErrWorkflowDispatch,
			errors.Wrapf(err, "记录工作流分片业务失败次数失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex))
	}
	if failures > int64(businessRetry) {
		return errors.Wrap(asynq.SkipRetry, runErr.Error())
	}
	return runErr
}

// markTaskSuccess 记录单个节点分片任务成功，并在达到预期实例数后推进 DAG。
func (m *Manager) markTaskSuccess(ctx context.Context, meta WorkflowTaskMeta) error {
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return nil
	}
	// 先原子记录业务成功结果；即使工作流被其它分片并发置为 failed，也必须保留本分片终态供手工重跑汇合。
	result, err := recordNodeOutcomeScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), "succeeded").Int()
	if err != nil {
		return errors.Tag(err)
	}
	if result == -2 {
		return nil
	}
	if result < 0 {
		return errors.Errorf("记录工作流节点成功终态返回非法结果 workflow_id=%s node=%s shard=%d result=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex, result)
	}
	if err = m.redis.HDel(ctx, m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode), workflowNodeBusinessFailureField(meta.ShardIndex)).Err(); err != nil {
		return errors.Wrapf(err, "清理工作流分片业务失败次数失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	failed, err := m.workflowFailed(ctx, meta.WorkflowID)
	if err != nil || failed {
		return errors.Tag(err)
	}
	def, err := m.workflowDefinition(meta.WorkflowName)
	if err != nil {
		return errors.Tag(err)
	}
	spec, err := m.workflowSpecByID(ctx, meta.WorkflowID)
	if err != nil {
		return errors.Tag(err)
	}
	finished, err := m.nodeReachedExpected(ctx, meta.WorkflowID, meta.WorkflowNode)
	if err != nil || !finished {
		return errors.Tag(err)
	}
	return m.onNodeCompleted(ctx, def, spec, meta.WorkflowNode)
}

// markTaskFailure 记录单个节点分片任务失败，并把工作流整体标记为失败。
// 返回值 failureRecorded 表示本次失败是否首次写入节点终态；重复晚到失败返回 false。
func (m *Manager) markTaskFailure(ctx context.Context, meta WorkflowTaskMeta, err error) (bool, error) {
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return meta.WorkflowID != "" && meta.WorkflowNode == "", nil
	}
	result, setErr := recordNodeOutcomeScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), "failed").Int()
	if setErr != nil {
		return false, errors.Wrapf(setErr, "记录工作流节点失败终态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if result == -2 {
		return false, nil
	}
	if result < 0 {
		return false, errors.Errorf("记录工作流节点失败终态返回非法结果 workflow_id=%s node=%s shard=%d result=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex, result)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if incErr := m.updateWorkflowNode(ctx, meta.WorkflowID, meta.WorkflowNode, map[string]any{
		"status":       NodeStatusFailed,
		"errorMessage": message,
		"finishedAt":   time.Now().Format(time.RFC3339),
	}); incErr != nil {
		return false, errors.Wrapf(incErr, "更新工作流节点失败状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if failErr := m.failWorkflow(ctx, meta.WorkflowID, fmt.Sprintf("节点执行失败 node=%s shard=%d err=%s", meta.WorkflowNode, meta.ShardIndex, message)); failErr != nil {
		return true, errors.Wrapf(failErr, "标记工作流失败失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	return result > 0, nil
}

// recordWorkflowTaskStats 保存单个节点分片的处理量快照，供工作流状态页按节点和分片聚合展示。
func (m *Manager) recordWorkflowTaskStats(ctx context.Context, meta WorkflowTaskMeta, snapshot *taskstats.Snapshot) error {
	if m == nil || snapshot == nil || snapshot.Empty() || strings.TrimSpace(meta.WorkflowID) == "" || strings.TrimSpace(meta.WorkflowNode) == "" {
		return nil
	}
	shardTotal := meta.ShardTotal
	if shardTotal <= 0 {
		shardTotal = 1
	}
	payload := WorkflowNodeShardTraceItem{
		ShardIndex:     meta.ShardIndex,
		ShardTotal:     shardTotal,
		ExecutionTrace: snapshot,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrapf(err, "序列化工作流节点处理量失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	return m.updateWorkflowNode(ctx, meta.WorkflowID, meta.WorkflowNode, map[string]any{
		workflowNodeTraceField(meta.ShardIndex): string(raw),
	})
}

// onNodeCompleted 在节点全部完成后推进下游节点，并判断工作流是否整体结束。
func (m *Manager) onNodeCompleted(ctx context.Context, def *WorkflowDefinition, spec WorkflowStartSpec, nodeName string) error {
	failed, err := m.workflowFailed(ctx, spec.WorkflowID)
	if err != nil || failed {
		return errors.Tag(err)
	}
	nodeKey := m.workflowNodeKey(spec.WorkflowID, nodeName)
	nodeStatus, err := m.redis.HGet(ctx, nodeKey, "status").Result()
	if err != nil && err != redis.Nil {
		return errors.Tag(err)
	}
	if nodeStatus != NodeStatusSkipped && nodeStatus != NodeStatusSuccess {
		if err = m.updateWorkflowNode(ctx, spec.WorkflowID, nodeName, map[string]any{
			"status":       NodeStatusSuccess,
			"errorMessage": "",
			"finishedAt":   time.Now().Format(time.RFC3339),
		}); err != nil {
			return errors.Tag(err)
		}
	}
	for _, child := range childNodeNames(def, nodeName) {
		if err = m.scheduleNode(ctx, def, spec, child); err != nil {
			return errors.Tag(err)
		}
	}
	completed, err := m.workflowAllCompleted(ctx, spec.WorkflowID, def)
	if err != nil {
		return errors.Tag(err)
	}
	if completed {
		return m.completeWorkflow(ctx, spec.WorkflowID)
	}
	return nil
}

// workflowFailed 判断工作流是否已经进入不可逆失败终态。
func (m *Manager) workflowFailed(ctx context.Context, workflowID string) (bool, error) {
	status, err := m.redis.HGet(ctx, m.workflowMetaKey(workflowID), "status").Result()
	if err != nil {
		return false, errors.Wrapf(err, "读取工作流状态失败 workflow_id=%s", workflowID)
	}
	return status == WorkflowStatusFailed, nil
}

// workflowNodeTaskID 返回工作流分片稳定任务 ID，重复调度时只补齐缺失分片。
func workflowNodeTaskID(workflowID, nodeName string, shardIndex int) string {
	return workflowID + ":" + nodeName + ":" + strconv.Itoa(shardIndex)
}

// workflowAllCompleted 检查工作流中所有节点是否都已达到成功或跳过终态。
func (m *Manager) workflowAllCompleted(ctx context.Context, workflowID string, def *WorkflowDefinition) (bool, error) {
	for _, nodeName := range sortedNodeNames(def) {
		status, err := m.redis.HGet(ctx, m.workflowNodeKey(workflowID, nodeName), "status").Result()
		if err != nil && err != redis.Nil {
			return false, errors.Tag(err)
		}
		switch status {
		case NodeStatusSuccess, NodeStatusSkipped:
		default:
			return false, nil
		}
	}
	return true, nil
}

// nodeReady 判断某个节点依赖的上游节点是否全部完成。
func (m *Manager) nodeReady(ctx context.Context, workflowID string, dependsOn []string) (bool, error) {
	for _, parent := range dependsOn {
		status, err := m.redis.HGet(ctx, m.workflowNodeKey(workflowID, parent), "status").Result()
		if err != nil && err != redis.Nil {
			return false, errors.Tag(err)
		}
		if status != NodeStatusSuccess && status != NodeStatusSkipped {
			return false, nil
		}
	}
	return true, nil
}

// nodeReachedExpected 判断节点成功实例数是否已经达到预期值且没有失败实例。
func (m *Manager) nodeReachedExpected(ctx context.Context, workflowID, nodeName string) (bool, error) {
	vals, err := m.redis.HMGet(ctx, m.workflowNodeKey(workflowID, nodeName), "expected", "succeeded", "failed").Result()
	if err != nil {
		return false, errors.Tag(err)
	}
	expected := toInt(vals[0])
	succeeded := toInt(vals[1])
	failed := toInt(vals[2])
	return expected > 0 && succeeded >= expected && failed == 0, nil
}

// completeWorkflow 把工作流整体标记为成功完成。
func (m *Manager) completeWorkflow(ctx context.Context, workflowID string) error {
	transition, err := m.transitionWorkflowStatus(ctx, workflowID, WorkflowStatusSuccess, "")
	if err != nil || transition < 0 {
		return errors.Tag(err)
	}
	return m.expireWorkflowState(ctx, workflowID)
}

// failWorkflow 把工作流整体标记为失败，并记录错误摘要。
func (m *Manager) failWorkflow(ctx context.Context, workflowID, message string) error {
	transition, err := m.transitionWorkflowStatus(ctx, workflowID, WorkflowStatusFailed, message)
	if err != nil || transition < 0 {
		return errors.Tag(err)
	}
	return m.expireWorkflowState(ctx, workflowID)
}

// failWorkflowDispatch 收口编排重试耗尽的实例，并仅释放仍由当前实例持有的唯一键。
func (m *Manager) failWorkflowDispatch(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error) error {
	if strings.TrimSpace(meta.WorkflowID) == "" {
		return nil
	}
	spec, err := m.workflowSpecByID(ctx, meta.WorkflowID)
	if err == redis.Nil {
		spec = workflowStartSpecFromTriggerTask(task)
		spec.WorkflowID = meta.WorkflowID
		if definition, definitionErr := m.workflowDefinition(spec.Name); definitionErr == nil {
			if err = applyPeriodicScheduleUniqueKey(definition, &spec); err != nil {
				return errors.Wrapf(err, "还原编排失败工作流唯一键失败 workflow_id=%s", meta.WorkflowID)
			}
		}
	} else if err != nil {
		return errors.Wrapf(err, "读取编排失败工作流参数失败 workflow_id=%s", meta.WorkflowID)
	}
	triggerSpec := workflowStartSpecFromTriggerTask(task)
	if spec.Name == "" {
		spec.Name = triggerSpec.Name
	}
	if spec.UniqueKey == "" {
		spec.UniqueKey = triggerSpec.UniqueKey
	}
	exists, err := m.redis.Exists(ctx, m.workflowMetaKey(meta.WorkflowID)).Result()
	if err != nil {
		return errors.Wrapf(err, "检查编排失败工作流元数据失败 workflow_id=%s", meta.WorkflowID)
	}
	if exists > 0 {
		status, statusErr := m.redis.HGet(ctx, m.workflowMetaKey(meta.WorkflowID), "status").Result()
		if statusErr != nil {
			return errors.Wrapf(statusErr, "读取编排失败工作流状态失败 workflow_id=%s", meta.WorkflowID)
		}
		if status == WorkflowStatusSuccess {
			return nil
		}
		message := "工作流编排重试耗尽"
		if runErr != nil {
			message = runErr.Error()
		}
		if err = m.failWorkflow(ctx, meta.WorkflowID, message); err != nil {
			return errors.Wrapf(err, "标记编排失败工作流终态失败 workflow_id=%s", meta.WorkflowID)
		}
		status, err = m.redis.HGet(ctx, m.workflowMetaKey(meta.WorkflowID), "status").Result()
		if err != nil {
			return errors.Wrapf(err, "确认编排失败工作流终态失败 workflow_id=%s", meta.WorkflowID)
		}
		if status != WorkflowStatusFailed {
			return nil
		}
	}
	if spec.Name != "" && spec.UniqueKey != "" {
		if err = m.releaseWorkflowUniqueReservation(ctx, spec.Name, spec.UniqueKey, meta.WorkflowID); err != nil {
			return errors.Wrapf(err, "释放编排失败工作流唯一键失败 workflow_id=%s", meta.WorkflowID)
		}
	}
	return nil
}

// workflowStartSpecFromTriggerTask 从入口任务载荷提取唯一键等终态清理参数。
func workflowStartSpecFromTriggerTask(task *asynq.Task) WorkflowStartSpec {
	if task == nil || task.Type() != TypeWorkflowTrigger {
		return WorkflowStartSpec{}
	}
	var payload WorkflowTriggerPayload
	if json.Unmarshal(task.Payload(), &payload) != nil {
		return WorkflowStartSpec{}
	}
	return WorkflowStartSpec{
		WorkflowID:  payload.WorkflowID,
		Name:        payload.WorkflowName,
		Source:      payload.Source,
		ScheduledAt: strings.TrimSpace(task.Headers()[HeaderScheduledAt]),
		UniqueKey:   payload.UniqueKey,
		UniqueTTL:   time.Duration(payload.UniqueTTLSeconds) * time.Second,
	}
}

// transitionWorkflowStatus 原子写入工作流终态，返回 1 表示首次迁移、0 表示同终态重放、负数表示已处于相反终态。
func (m *Manager) transitionWorkflowStatus(ctx context.Context, workflowID, status, message string) (int, error) {
	now := time.Now().Format(time.RFC3339)
	result, err := transitionWorkflowStatusScript.Run(ctx, m.redis, []string{m.workflowMetaKey(workflowID)},
		status, now, message, taskCompletedRetention.Milliseconds()).Int()
	if err != nil {
		return 0, errors.Wrapf(err, "迁移工作流终态失败 workflow_id=%s status=%s", workflowID, status)
	}
	if result == -2 {
		return result, errors.Errorf("工作流元数据不存在 workflow_id=%s", workflowID)
	}
	return result, nil
}

// workflowStateKeys 返回单个工作流实例的全部精确状态 key，不使用通配扫描。
func (m *Manager) workflowStateKeys(ctx context.Context, workflowID string) ([]string, error) {
	nodeNames, err := m.redis.SMembers(ctx, m.workflowNodesKey(workflowID)).Result()
	if err != nil {
		return nil, errors.Wrapf(err, "读取工作流节点集合失败 workflow_id=%s", workflowID)
	}
	keys := []string{m.workflowMetaKey(workflowID), m.workflowNodesKey(workflowID)}
	for _, nodeName := range nodeNames {
		keys = append(keys, m.workflowNodeKey(workflowID, nodeName))
	}
	return keys, nil
}

// expireWorkflowState 在实例进入终态后为全部精确状态 key 设置统一保留时间。
func (m *Manager) expireWorkflowState(ctx context.Context, workflowID string) error {
	keys, err := m.workflowStateKeys(ctx, workflowID)
	if err != nil {
		return errors.Tag(err)
	}
	for _, key := range keys {
		if err = m.redis.Expire(ctx, key, taskCompletedRetention).Err(); err != nil {
			return errors.Wrapf(err, "设置工作流终态保留时间失败 workflow_id=%s key=%s", workflowID, key)
		}
	}
	return nil
}

// persistWorkflowState 在手工重开时清除全部精确状态 key 的终态 TTL。
func (m *Manager) persistWorkflowState(ctx context.Context, workflowID string) error {
	keys, err := m.workflowStateKeys(ctx, workflowID)
	if err != nil {
		return errors.Tag(err)
	}
	for _, key := range keys {
		if err = m.redis.Persist(ctx, key).Err(); err != nil {
			return errors.Wrapf(err, "清除运行态工作流过期时间失败 workflow_id=%s key=%s", workflowID, key)
		}
	}
	return nil
}

// persistWorkflowManualRerunState 清除运行态 TTL，并在并发终态覆盖时恢复统一保留时间。
func (m *Manager) persistWorkflowManualRerunState(ctx context.Context, workflowID string) error {
	if err := m.persistWorkflowState(ctx, workflowID); err != nil {
		return errors.Join(err, m.restoreWorkflowTerminalRetention(ctx, workflowID))
	}
	values, err := m.redis.HMGet(ctx, m.workflowMetaKey(workflowID), "status", "manualRerun").Result()
	if err != nil {
		return errors.Wrapf(err, "确认手工重跑工作流状态失败 workflow_id=%s", workflowID)
	}
	status, _ := values[0].(string)
	manualRerun, _ := values[1].(string)
	status = strings.TrimSpace(status)
	manualRerun = strings.TrimSpace(manualRerun)
	if status == WorkflowStatusSuccess || status == WorkflowStatusFailed {
		return errors.Join(
			errors.Errorf("手工重跑期间工作流已进入终态 workflow_id=%s status=%s", workflowID, status),
			m.expireWorkflowState(ctx, workflowID),
		)
	}
	if status != WorkflowStatusRunning || manualRerun != "1" {
		return errors.Errorf("手工重跑工作流状态无效 workflow_id=%s status=%s manual_rerun=%s", workflowID, status, manualRerun)
	}
	return nil
}

// restoreWorkflowTerminalRetention 在并发手工重跑错误路径恢复终态 key 的统一保留时间。
func (m *Manager) restoreWorkflowTerminalRetention(ctx context.Context, workflowID string) error {
	status, err := m.redis.HGet(ctx, m.workflowMetaKey(workflowID), "status").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "读取并发工作流终态失败 workflow_id=%s", workflowID)
	}
	if status != WorkflowStatusSuccess && status != WorkflowStatusFailed {
		return nil
	}
	return m.expireWorkflowState(ctx, workflowID)
}

// updateWorkflowMeta 局部更新运行态工作流主记录。
func (m *Manager) updateWorkflowMeta(ctx context.Context, workflowID string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	values["updatedAt"] = time.Now().Format(time.RFC3339)
	if err := m.redis.HSet(ctx, m.workflowMetaKey(workflowID), values).Err(); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// updateWorkflowNode 局部更新运行态节点状态。
func (m *Manager) updateWorkflowNode(ctx context.Context, workflowID, nodeName string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	if err := m.redis.HSet(ctx, m.workflowNodeKey(workflowID, nodeName), values).Err(); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// workflowDefinition 按名称读取已注册的工作流定义。
func (m *Manager) workflowDefinition(name string) (*WorkflowDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.workflows[name]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	return def, nil
}

// validateWorkflowDefinition 校验 DAG 定义是否合法，避免启动后运行期失败。
func validateWorkflowDefinition(def *WorkflowDefinition) error {
	if def == nil {
		return errors.Errorf("工作流定义不能为空")
	}
	def.Name = strings.TrimSpace(def.Name)
	if def.Name == "" {
		return errors.Errorf("工作流名称不能为空")
	}
	if len(def.Nodes) == 0 {
		return errors.Errorf("工作流 %s 至少需要定义一个节点", def.Name)
	}
	for nodeName, node := range def.Nodes {
		if strings.TrimSpace(nodeName) == "" {
			return errors.Errorf("工作流 %s 包含空节点名", def.Name)
		}
		if node == nil {
			return errors.Errorf("工作流 %s 的节点 %s 为空", def.Name, nodeName)
		}
		if node.Name == "" {
			node.Name = nodeName
		}
		if node.Name != nodeName {
			return errors.Errorf("工作流 %s 的节点名不一致: key=%s name=%s", def.Name, nodeName, node.Name)
		}
		node.DependsOn = normalizeStrings(node.DependsOn)
		if strings.TrimSpace(node.TaskType) == "" {
			return errors.Errorf("工作流 %s 的节点 %s 缺少任务类型", def.Name, nodeName)
		}
		if node.BuildPayload == nil {
			return errors.Errorf("工作流 %s 的节点 %s 缺少负载构造器", def.Name, nodeName)
		}
		for _, dep := range node.DependsOn {
			if dep == nodeName {
				return errors.Errorf("工作流 %s 的节点 %s 不能依赖自身", def.Name, nodeName)
			}
			if _, ok := def.Nodes[dep]; !ok {
				return errors.Errorf("工作流 %s 的节点 %s 依赖未知节点 %s", def.Name, nodeName, dep)
			}
		}
	}
	if len(rootNodeNames(def)) == 0 {
		return errors.Errorf("工作流 %s 至少需要一个根节点", def.Name)
	}
	visiting := make(map[string]bool, len(def.Nodes))
	visited := make(map[string]bool, len(def.Nodes))
	var walk func(string) error
	walk = func(nodeName string) error {
		if visited[nodeName] {
			return nil
		}
		if visiting[nodeName] {
			return errors.Errorf("工作流 %s 在节点 %s 存在循环依赖", def.Name, nodeName)
		}
		visiting[nodeName] = true
		for _, dep := range def.Nodes[nodeName].DependsOn {
			if err := walk(dep); err != nil {
				return errors.Tag(err)
			}
		}
		visiting[nodeName] = false
		visited[nodeName] = true
		return nil
	}
	for _, nodeName := range sortedNodeNames(def) {
		if err := walk(nodeName); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// sortedNodeNames 返回稳定排序后的节点名列表。
func sortedNodeNames(def *WorkflowDefinition) []string {
	names := make([]string, 0, len(def.Nodes))
	for name := range def.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// rootNodeNames 返回没有依赖的根节点列表。
func rootNodeNames(def *WorkflowDefinition) []string {
	roots := make([]string, 0)
	for name, node := range def.Nodes {
		if len(node.DependsOn) == 0 {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	return roots
}

// childNodeNames 返回直接依赖指定父节点的下游节点列表。
func childNodeNames(def *WorkflowDefinition, parent string) []string {
	children := make([]string, 0)
	for name, node := range def.Nodes {
		for _, dep := range node.DependsOn {
			if dep == parent {
				children = append(children, name)
				break
			}
		}
	}
	sort.Strings(children)
	return children
}

// effectiveShardTotal 返回节点本次实际使用的分片数。
func effectiveShardTotal(node *WorkflowNodeDefinition, spec WorkflowStartSpec) int {
	if node != nil && node.SupportsSharding && spec.ShardTotal > 1 {
		return spec.ShardTotal
	}
	return 1
}

// effectiveRetry 返回节点本次实际生效的重试次数。
func effectiveRetry(node *WorkflowNodeDefinition, spec WorkflowStartSpec) int {
	if node != nil && node.MaxRetry < 0 {
		// 负数重试次数用于 finalize 这类非幂等节点，避免全局 retry 覆盖后重复执行副作用。
		return 0
	}
	if spec.RetryOverride != nil {
		return *spec.RetryOverride
	}
	if node != nil && node.MaxRetry > 0 {
		return node.MaxRetry
	}
	return 0
}

// workflowTaskRetryBudget 在业务重试之外预留固定编排重试，避免投递故障挤占业务预算。
func workflowTaskRetryBudget(businessRetry int) int {
	return max(businessRetry, 0) + workflowRepairRetryCount
}

// effectiveTimeout 返回节点本次实际生效的超时时间。
func effectiveTimeout(node *WorkflowNodeDefinition, spec WorkflowStartSpec) time.Duration {
	if spec.TimeoutOverride > 0 {
		return spec.TimeoutOverride
	}
	if node != nil {
		return node.Timeout
	}
	return 0
}

// selectedShardIndexes 根据灰度百分比筛选本次需要执行的分片索引。
func selectedShardIndexes(workflowID, nodeName string, total, grayPercent int, supportsGray bool) []int {
	if total <= 0 {
		return nil
	}
	indexes := make([]int, 0, total)
	for i := 0; i < total; i++ {
		if !supportsGray || grayPercent >= 100 || grayPercent <= 0 {
			indexes = append(indexes, i)
			continue
		}
		if hashPercent(fmt.Sprintf("%s:%s:%d", workflowID, nodeName, i)) < grayPercent {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// SplitStrings 按分片总数和分片索引切分字符串列表，用于工作流分片和批量目标拆分。
func SplitStrings(items []string, shardTotal, shardIndex int) []string {
	if shardTotal <= 1 {
		return append([]string(nil), items...)
	}
	result := make([]string, 0)
	for i, item := range items {
		if i%shardTotal == shardIndex {
			result = append(result, item)
		}
	}
	return result
}

// readWorkflowStatus 从 Redis 读取工作流主记录和全部节点状态。
func (m *Manager) readWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowMetaRecord, []WorkflowNodeRecord, error) {
	metaMap, err := m.redis.HGetAll(ctx, m.workflowMetaKey(workflowID)).Result()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	if len(metaMap) == 0 {
		return nil, nil, redis.Nil
	}
	meta := &WorkflowMetaRecord{
		WorkflowID:   metaMap["workflowId"],
		WorkflowName: metaMap["workflowName"],
		Status:       metaMap["status"],
		Source:       metaMap["source"],
		Queue:        metaMap["queue"],
		Targets:      decodeStringList(metaMap["targets"]),
		ShardTotal:   toInt(metaMap["shardTotal"]),
		GrayPercent:  toInt(metaMap["grayPercent"]),
		ErrorMessage: metaMap["errorMessage"],
		CreatedAt:    metaMap["createdAt"],
		UpdatedAt:    metaMap["updatedAt"],
		FinishedAt:   metaMap["finishedAt"],
	}
	nodeNames, err := m.redis.SMembers(ctx, m.workflowNodesKey(workflowID)).Result()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	sort.Strings(nodeNames)
	nodes := make([]WorkflowNodeRecord, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		vals, readErr := m.redis.HGetAll(ctx, m.workflowNodeKey(workflowID, nodeName)).Result()
		if readErr != nil {
			return nil, nil, errors.Wrapf(readErr, "读取工作流节点失败 workflow_id=%s node=%s", workflowID, nodeName)
		}
		nodes = append(nodes, WorkflowNodeRecord{
			Name:         vals["name"],
			TaskType:     vals["taskType"],
			Queue:        vals["queue"],
			Status:       vals["status"],
			DependsOn:    decodeStringList(vals["dependsOn"]),
			Expected:     toInt(vals["expected"]),
			Succeeded:    toInt(vals["succeeded"]),
			Failed:       toInt(vals["failed"]),
			Skipped:      toInt(vals["skipped"]),
			ErrorMessage: vals["errorMessage"],
			StartedAt:    vals["startedAt"],
			FinishedAt:   vals["finishedAt"],
		})
		current := &nodes[len(nodes)-1]
		current.ShardTraces = decodeWorkflowNodeShardTraces(vals)
		current.ExecutionTrace = aggregateWorkflowNodeStats(current.Name, current.ShardTraces)
		current.Progress = workflowNodeProgress(*current, 0)
	}
	return meta, nodes, nil
}

// decodeWorkflowNodeShardTraces 从节点 hash 中解析所有已记录的分片处理量快照和分片终态。
func decodeWorkflowNodeShardTraces(values map[string]string) []WorkflowNodeShardTraceItem {
	if len(values) == 0 {
		return nil
	}
	shardMap := make(map[int]WorkflowNodeShardTraceItem)
	for key, raw := range values {
		shardValue := strings.TrimPrefix(key, workflowNodeTraceFieldPrefix)
		if shardValue == key {
			continue
		}
		shardIndex, err := strconv.Atoi(shardValue)
		if err != nil {
			continue
		}
		var shardTrace WorkflowNodeShardTraceItem
		if err = json.Unmarshal([]byte(raw), &shardTrace); err == nil && shardTrace.ExecutionTrace != nil && !shardTrace.ExecutionTrace.Empty() {
			if shardTrace.ShardTotal <= 0 {
				shardTrace.ShardTotal = 1
			}
			shardMap[shardIndex] = shardTrace
			continue
		}
		statsSnapshot := taskExecutionStatsFromJSON([]byte(raw))
		if statsSnapshot == nil {
			continue
		}
		shardMap[shardIndex] = WorkflowNodeShardTraceItem{
			ShardIndex:     shardIndex,
			ShardTotal:     1,
			ExecutionTrace: statsSnapshot,
		}
	}
	for key, raw := range values {
		shardValue := strings.TrimPrefix(key, workflowNodeInstanceFieldPrefix)
		if shardValue == key {
			continue
		}
		shardIndex, err := strconv.Atoi(shardValue)
		if err != nil {
			continue
		}
		shardTrace := shardMap[shardIndex]
		shardTrace.ShardIndex = shardIndex
		if shardTrace.ShardTotal <= 0 {
			shardTrace.ShardTotal = 1
		}
		shardTrace.Status = workflowShardStatus(raw, shardTrace.ExecutionTrace != nil)
		shardMap[shardIndex] = shardTrace
	}
	shards := make([]WorkflowNodeShardTraceItem, 0, len(shardMap))
	for _, shardTrace := range shardMap {
		if shardTrace.ShardTotal <= 0 {
			shardTrace.ShardTotal = 1
		}
		if shardTrace.Status == "" {
			shardTrace.Status = workflowShardStatus("", shardTrace.ExecutionTrace != nil)
		}
		shardTrace.Progress = workflowShardProgress(shardTrace.Status)
		shards = append(shards, shardTrace)
	}
	sort.Slice(shards, func(i, j int) bool {
		return shards[i].ShardIndex < shards[j].ShardIndex
	})
	return shards
}

// aggregateWorkflowNodeStats 聚合单个节点下所有分片处理量。
func aggregateWorkflowNodeStats(nodeName string, shards []WorkflowNodeShardTraceItem) *taskstats.Snapshot {
	if len(shards) == 0 {
		return nil
	}
	snapshots := make([]*taskstats.Snapshot, 0, len(shards))
	for i := range shards {
		snapshots = append(snapshots, shards[i].ExecutionTrace)
	}
	return taskstats.MergeSnapshots("workflow.node."+strings.TrimSpace(nodeName), snapshots...)
}

// workflowNodeProgress 计算单个节点的分片执行进度。
func workflowNodeProgress(node WorkflowNodeRecord, planned int) *taskstats.Progress {
	total := int64(planned)
	if total <= 0 {
		total = int64(node.Expected)
	}
	if total <= 0 && node.Skipped > 0 {
		total = int64(node.Skipped)
	}
	return taskstats.NewProgress(taskstats.ProgressUnitShard, node.Status, total, int64(node.Succeeded), int64(node.Failed), int64(node.Skipped))
}

// plannedWorkflowNodeInstances 根据工作流定义计算节点本次计划实例数。
func plannedWorkflowNodeInstances(def *WorkflowDefinition, meta *WorkflowMetaRecord, node WorkflowNodeRecord) int {
	if node.Expected > 0 {
		return node.Expected
	}
	if node.Skipped > 0 {
		return node.Skipped
	}
	if def == nil || meta == nil {
		return 0
	}
	nodeDef := def.Nodes[node.Name]
	if nodeDef == nil {
		return 0
	}
	shardTotal := effectiveShardTotal(nodeDef, WorkflowStartSpec{ShardTotal: meta.ShardTotal})
	return len(selectedShardIndexes(meta.WorkflowID, node.Name, shardTotal, meta.GrayPercent, nodeDef.SupportsGray))
}

// workflowShardStatus 根据分片终态和处理量快照推导分片状态。
func workflowShardStatus(raw string, hasTrace bool) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "succeeded", NodeStatusSuccess:
		return NodeStatusSuccess
	case NodeStatusFailed:
		return NodeStatusFailed
	case NodeStatusSkipped:
		return NodeStatusSkipped
	case NodeStatusRunning:
		return NodeStatusRunning
	case workflowNodeOutcomeRerunPrepared:
		return NodeStatusRunning
	case NodeStatusPending:
		return NodeStatusPending
	}
	if hasTrace {
		return NodeStatusRunning
	}
	return NodeStatusPending
}

// workflowShardProgress 生成单个分片的执行进度。
func workflowShardProgress(status string) *taskstats.Progress {
	switch workflowShardStatus(status, false) {
	case NodeStatusSuccess:
		return taskstats.NewProgress(taskstats.ProgressUnitShard, NodeStatusSuccess, 1, 1, 0, 0)
	case NodeStatusFailed:
		return taskstats.NewProgress(taskstats.ProgressUnitShard, NodeStatusFailed, 1, 0, 1, 0)
	case NodeStatusSkipped:
		return taskstats.NewProgress(taskstats.ProgressUnitShard, NodeStatusSkipped, 1, 0, 0, 1)
	case NodeStatusRunning:
		return taskstats.NewProgress(taskstats.ProgressUnitShard, NodeStatusRunning, 1, 0, 0, 0)
	default:
		return taskstats.NewProgress(taskstats.ProgressUnitShard, NodeStatusPending, 1, 0, 0, 0)
	}
}

// workflowSpecByID 根据工作流实例 ID 还原其启动参数摘要。
func (m *Manager) workflowSpecByID(ctx context.Context, workflowID string) (WorkflowStartSpec, error) {
	metaMap, err := m.redis.HGetAll(ctx, m.workflowMetaKey(workflowID)).Result()
	if err != nil {
		return WorkflowStartSpec{}, errors.Tag(err)
	}
	if len(metaMap) == 0 {
		return WorkflowStartSpec{}, redis.Nil
	}
	spec := WorkflowStartSpec{
		WorkflowID:   metaMap["workflowId"],
		Name:         metaMap["workflowName"],
		Queue:        metaMap["queue"],
		Targets:      decodeStringList(metaMap["targets"]),
		ShardTotal:   toInt(metaMap["shardTotal"]),
		GrayPercent:  toInt(metaMap["grayPercent"]),
		Source:       metaMap["source"],
		PeriodicName: metaMap["periodicName"],
		ScheduledAt:  metaMap["scheduledAt"],
		UniqueKey:    metaMap["uniqueKey"],
	}
	if uniqueTTLSeconds := toInt(metaMap["uniqueTTLSeconds"]); uniqueTTLSeconds > 0 {
		spec.UniqueTTL = time.Duration(uniqueTTLSeconds) * time.Second
	}
	if rawRetry, ok := metaMap["retryOverride"]; ok {
		retry := toInt(rawRetry)
		spec.RetryOverride = &retry
	}
	if timeoutSeconds := toInt(metaMap["timeoutOverrideSeconds"]); timeoutSeconds > 0 {
		spec.TimeoutOverride = time.Duration(timeoutSeconds) * time.Second
	}
	return spec, nil
}

// workflowMetadataComplete 校验工作流初始化元数据是否完整。
// 启动重试时必须确认 nodes 和 node hash 都存在。
func (m *Manager) workflowMetadataComplete(ctx context.Context, workflowID string, def *WorkflowDefinition) (bool, error) {
	if m == nil || def == nil || strings.TrimSpace(workflowID) == "" {
		return false, nil
	}
	metaMap, err := m.redis.HGetAll(ctx, m.workflowMetaKey(workflowID)).Result()
	if err != nil {
		return false, errors.Wrap(err, "读取工作流主元数据失败")
	}
	if strings.TrimSpace(metaMap["workflowId"]) == "" || strings.TrimSpace(metaMap["workflowName"]) == "" {
		return false, nil
	}
	nodeNames, err := m.redis.SMembers(ctx, m.workflowNodesKey(workflowID)).Result()
	if err != nil {
		return false, errors.Wrap(err, "读取工作流节点集合失败")
	}
	if len(nodeNames) != len(def.Nodes) {
		return false, nil
	}
	existingNodes := make(map[string]struct{}, len(nodeNames))
	for _, nodeName := range nodeNames {
		existingNodes[nodeName] = struct{}{}
	}
	for _, nodeName := range sortedNodeNames(def) {
		if _, ok := existingNodes[nodeName]; !ok {
			return false, nil
		}
		exists, existsErr := m.redis.HExists(ctx, m.workflowNodeKey(workflowID, nodeName), "status").Result()
		if existsErr != nil {
			return false, errors.Wrapf(existsErr, "读取工作流节点元数据失败 workflow_id=%s node=%s", workflowID, nodeName)
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

// cleanupIncompleteWorkflowMetadata 清理半初始化工作流元数据。
// 删除操作逐 key 执行，避免 Redis Cluster 多 key DEL 跨 slot。
func (m *Manager) cleanupIncompleteWorkflowMetadata(ctx context.Context, workflowID string, def *WorkflowDefinition) error {
	if m == nil || def == nil || strings.TrimSpace(workflowID) == "" {
		return nil
	}
	keys := []string{
		m.workflowMetaKey(workflowID),
		m.workflowNodesKey(workflowID),
	}
	for _, nodeName := range sortedNodeNames(def) {
		keys = append(keys,
			m.workflowNodeKey(workflowID, nodeName),
		)
	}
	for _, key := range keys {
		if err := m.redis.Del(ctx, key).Err(); err != nil {
			return errors.Wrapf(err, "清理半初始化工作流元数据失败 workflow_id=%s key=%s", workflowID, key)
		}
	}
	return nil
}

// hashPercent 把字符串稳定映射到 0~99，用于灰度分片判断。
func hashPercent(text string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(text))
	return int(h.Sum32() % 100)
}

// reserveWorkflowUnique 预占工作流唯一键，避免重复触发相同业务实例。
func (m *Manager) reserveWorkflowUnique(ctx context.Context, name, key, workflowID string, ttl time.Duration) (bool, error) {
	if m == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(workflowID) == "" || ttl <= 0 {
		return false, nil
	}
	return m.redis.SetNX(ctx, m.workflowUniqueKey(name, key), workflowID, ttl).Result()
}

// ensureWorkflowUnique 确认工作流唯一键是否可用；已由当前实例持有时仅刷新过期时间。
func (m *Manager) ensureWorkflowUnique(ctx context.Context, name, key, workflowID string, ttl time.Duration) (bool, error) {
	if m == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(workflowID) == "" || ttl <= 0 {
		return true, nil
	}
	ttlMilliseconds := ttl.Milliseconds()
	if ttlMilliseconds <= 0 {
		return false, errors.Errorf("工作流唯一键 TTL 必须不少于 1 毫秒")
	}
	result, err := ensureWorkflowUniqueScript.Run(ctx, m.redis, []string{m.workflowUniqueKey(name, key)}, workflowID, ttlMilliseconds).Int()
	if err != nil {
		return false, errors.Wrap(err, "确认工作流唯一键失败")
	}
	return result == 1, nil
}

// releaseWorkflowUniqueReservation 释放本次触发流程预占的唯一键记录。
func (m *Manager) releaseWorkflowUniqueReservation(ctx context.Context, name, key, workflowID string) error {
	if m == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(workflowID) == "" {
		return nil
	}
	_, err := releaseWorkflowUniqueScript.Run(ctx, m.redis, []string{m.workflowUniqueKey(name, key)}, workflowID).Result()
	return errors.Tag(err)
}

// decodeStringList 把 Redis 中保存的 JSON 字符串数组恢复为切片。
func decodeStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}
