package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"admin_cron/helper"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// WorkflowNameCacheRefresh 是内置的缓存刷新工作流名称。
	WorkflowNameCacheRefresh = "cache.refresh"

	// WorkflowSourceAPI 表示由管理接口手动触发。
	WorkflowSourceAPI = "api"
	// WorkflowSourcePeriodic 表示由周期调度触发。
	WorkflowSourcePeriodic = "periodic"
	// WorkflowSourceInternal 表示由应用内部逻辑触发。
	WorkflowSourceInternal = "internal"

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

	// workflowNodeInstanceFieldPrefix 是节点 hash 内部的分片终态去重字段前缀。
	// 去重标记与成功/失败计数放在同一个 Redis hash 中，避免 Redis Cluster Lua 跨 slot 写多 key。
	workflowNodeInstanceFieldPrefix = "__instance_done:"
)

// workflowNodeInstanceField 返回节点 hash 中单个分片的终态去重字段名。
func workflowNodeInstanceField(shardIndex int) string {
	return fmt.Sprintf("%s%d", workflowNodeInstanceFieldPrefix, shardIndex)
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
	Name          string                             // 工作流名称
	Description   string                             // 工作流中文说明
	DefaultQueue  string                             // 默认执行队列
	MaxShardTotal int                                // 允许的最大分片数，0 表示不限制
	Nodes         map[string]*WorkflowNodeDefinition // 节点定义集合，key 为节点名
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
	UniqueTTL        time.Duration                                                                                          // 节点任务去重窗口，避免短时间重复投递
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
	Name         string   `json:"name"`                   // 节点名称
	TaskType     string   `json:"taskType"`               // 节点任务类型
	Queue        string   `json:"queue"`                  // 节点队列
	Status       string   `json:"status"`                 // 当前节点状态
	DependsOn    []string `json:"dependsOn,omitempty"`    // 依赖节点列表
	Expected     int      `json:"expected"`               // 预期执行实例数
	Succeeded    int      `json:"succeeded"`              // 已成功实例数
	Failed       int      `json:"failed"`                 // 已失败实例数
	Skipped      int      `json:"skipped"`                // 已跳过实例数
	ErrorMessage string   `json:"errorMessage,omitempty"` // 节点错误摘要
	StartedAt    string   `json:"startedAt,omitempty"`    // 首次开始时间
	FinishedAt   string   `json:"finishedAt,omitempty"`   // 完成时间
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
	if spec.UniqueKey != "" && spec.UniqueTTL > 0 {
		locked, lockErr := m.ensureWorkflowUnique(ctx, spec.Name, spec.UniqueKey, spec.WorkflowID, spec.UniqueTTL)
		if lockErr != nil {
			return "", errors.Wrap(lockErr, "锁定工作流唯一键失败")
		}
		if !locked {
			return spec.WorkflowID, ErrWorkflowAlreadyExists
		}
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
			return spec.WorkflowID, nil
		}
		// 旧版本或 Redis 异常可能留下只有 meta、缺少 nodes/node hash 的半初始化数据。
		// 这里先按精确 key 清理，再重建完整工作流元数据，避免后续误判工作流已存在但无法推进。
		if err = m.cleanupIncompleteWorkflowMetadata(ctx, spec.WorkflowID, def); err != nil {
			return "", errors.Tag(err)
		}
	}

	nodeNames := sortedNodeNames(def)
	now := time.Now().Format(time.RFC3339)
	// Redis Cluster 下 MULTI/EXEC 不能跨 slot；工作流 meta、nodes、node hash key 不保证同 slot。
	// 初始化写入使用普通 pipeline，由完整性检查负责清理并重建极端情况下的半初始化数据。
	pipe := m.redis.Pipeline()
	pipe.HSet(ctx, metaKey,
		"workflowId", spec.WorkflowID,
		"workflowName", spec.Name,
		"status", WorkflowStatusPending,
		"source", spec.Source,
		"queue", spec.Queue,
		"targets", mustJSON(spec.Targets),
		"shardTotal", spec.ShardTotal,
		"grayPercent", spec.GrayPercent,
		"errorMessage", "",
		"createdAt", now,
		"updatedAt", now,
		"finishedAt", "",
	)
	pipe.Expire(ctx, metaKey, m.workflowRetention())
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
		pipe.Expire(ctx, nodeKey, m.workflowRetention())
	}
	pipe.Expire(ctx, m.workflowNodesKey(spec.WorkflowID), m.workflowRetention())
	if _, err = pipe.Exec(ctx); err != nil {
		return "", errors.Wrap(err, "保存工作流元数据失败")
	}

	if err = m.updateWorkflowMeta(ctx, spec.WorkflowID, map[string]any{"status": WorkflowStatusRunning, "updatedAt": now}); err != nil {
		return "", errors.Tag(err)
	}
	for _, nodeName := range rootNodeNames(def) {
		if err = m.scheduleNode(ctx, def, spec, nodeName); err != nil {
			_ = m.failWorkflow(ctx, spec.WorkflowID, fmt.Sprintf("调度根节点 %s 失败: %v", nodeName, err))
			return "", errors.Tag(err)
		}
	}
	return spec.WorkflowID, nil
}

// scheduleNode 在依赖满足后调度指定节点，并按分片策略拆分为多个任务实例。
func (m *Manager) scheduleNode(ctx context.Context, def *WorkflowDefinition, spec WorkflowStartSpec, nodeName string) error {
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

	scheduled, err := m.redis.SetNX(ctx, m.workflowNodeScheduledKey(spec.WorkflowID, nodeName), "1", m.workflowRetention()).Result()
	if err != nil {
		return errors.Wrap(err, "标记节点已调度失败")
	}
	if !scheduled {
		return nil
	}

	queue := helper.FirstNonEmptyString(node.Queue, spec.Queue, m.defaultWorkflowQueue())
	selectedShards := selectedShardIndexes(spec.WorkflowID, nodeName, effectiveShardTotal(node, spec), spec.GrayPercent, node.SupportsGray)
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

	for _, shardIndex := range selectedShards {
		payload, buildErr := node.BuildPayload(spec, node, shardIndex, effectiveShardTotal(node, spec))
		if buildErr != nil {
			return errors.Wrapf(buildErr, "构建节点负载失败 node=%s shard=%d", nodeName, shardIndex)
		}
		task := m.newTask(ctx, node.TaskType, payload, map[string]string{
			headerTaskName:     workflowNodeTaskName(spec.Name, nodeName, node.TaskType),
			headerWorkflowID:   spec.WorkflowID,
			headerWorkflowName: spec.Name,
			headerWorkflowNode: nodeName,
			headerShardIndex:   fmt.Sprintf("%d", shardIndex),
			headerShardTotal:   fmt.Sprintf("%d", effectiveShardTotal(node, spec)),
		})
		opts := []asynq.Option{
			asynq.Queue(m.namespacedQueueName(queue)),
			asynq.MaxRetry(effectiveRetry(node, spec)),
		}
		if retention := m.completedRetention(); retention > 0 {
			opts = append(opts, asynq.Retention(retention))
		}
		if timeout := effectiveTimeout(node, spec); timeout > 0 {
			opts = append(opts, asynq.Timeout(timeout))
		}
		if node.UniqueTTL > 0 {
			opts = append(opts, asynq.Unique(node.UniqueTTL))
		}
		if _, err = m.client.EnqueueContext(ctx, task, opts...); err != nil {
			return errors.Wrapf(err, "投递节点任务失败 node=%s shard=%d", nodeName, shardIndex)
		}
	}
	return nil
}

// markTaskSuccess 记录单个节点分片任务成功，并在达到预期实例数后推进 DAG。
func (m *Manager) markTaskSuccess(ctx context.Context, meta WorkflowTaskMeta) error {
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return nil
	}
	// 单 key Lua 原子记录节点结果，并在重跑成功时修正失败计数。
	_, err := recordNodeOutcomeScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), m.workflowRetention().Milliseconds(), "succeeded").Result()
	if err != nil {
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
	if err = m.redis.Del(ctx, m.workflowFailedKey(meta.WorkflowID)).Err(); err != nil {
		return errors.Wrapf(err, "清理工作流失败终态标记失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	return m.onNodeCompleted(ctx, def, spec, meta.WorkflowNode)
}

// markTaskFailure 记录单个节点分片任务失败，并把工作流整体标记为失败。
func (m *Manager) markTaskFailure(ctx context.Context, meta WorkflowTaskMeta, err error) error {
	if meta.WorkflowID == "" || meta.WorkflowNode == "" {
		return nil
	}
	result, setErr := recordNodeOutcomeScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), m.workflowRetention().Milliseconds(), "failed").Result()
	if setErr != nil {
		return setErr
	}
	// 分片已成功后的迟到失败回执会被 Lua 判定为重复噪声，避免已完成节点被回滚为失败。
	if toInt(result) == 0 {
		return nil
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
		return incErr
	}
	return m.failWorkflow(ctx, meta.WorkflowID, fmt.Sprintf("节点执行失败 node=%s shard=%d err=%s", meta.WorkflowNode, meta.ShardIndex, message))
}

// onNodeCompleted 在节点全部完成后推进下游节点，并判断工作流是否整体结束。
func (m *Manager) onNodeCompleted(ctx context.Context, def *WorkflowDefinition, spec WorkflowStartSpec, nodeName string) error {
	finalized, err := m.redis.SetNX(ctx, m.workflowNodeFinalizedKey(spec.WorkflowID, nodeName), "1", m.workflowRetention()).Result()
	if err != nil || !finalized {
		return errors.Tag(err)
	}
	nodeKey := m.workflowNodeKey(spec.WorkflowID, nodeName)
	nodeStatus, err := m.redis.HGet(ctx, nodeKey, "status").Result()
	if err != nil && err != redis.Nil {
		return errors.Tag(err)
	}
	if nodeStatus != NodeStatusSkipped {
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
	ok, err := m.redis.SetNX(ctx, m.workflowCompletedKey(workflowID), "1", m.workflowRetention()).Result()
	if err != nil || !ok {
		return errors.Tag(err)
	}
	return m.updateWorkflowMeta(ctx, workflowID, map[string]any{
		"status":     WorkflowStatusSuccess,
		"updatedAt":  time.Now().Format(time.RFC3339),
		"finishedAt": time.Now().Format(time.RFC3339),
	})
}

// failWorkflow 把工作流整体标记为失败，并记录错误摘要。
func (m *Manager) failWorkflow(ctx context.Context, workflowID, message string) error {
	ok, err := m.redis.SetNX(ctx, m.workflowFailedKey(workflowID), "1", m.workflowRetention()).Result()
	if err != nil || !ok {
		return errors.Tag(err)
	}
	return m.updateWorkflowMeta(ctx, workflowID, map[string]any{
		"status":       WorkflowStatusFailed,
		"updatedAt":    time.Now().Format(time.RFC3339),
		"finishedAt":   time.Now().Format(time.RFC3339),
		"errorMessage": message,
	})
}

// updateWorkflowMeta 局部更新工作流主记录，并刷新其过期时间。
func (m *Manager) updateWorkflowMeta(ctx context.Context, workflowID string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	values["updatedAt"] = time.Now().Format(time.RFC3339)
	if err := m.redis.HSet(ctx, m.workflowMetaKey(workflowID), values).Err(); err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(m.redis.Expire(ctx, m.workflowMetaKey(workflowID), m.workflowRetention()).Err())
}

// updateWorkflowNode 局部更新单个节点状态记录，并刷新其过期时间。
func (m *Manager) updateWorkflowNode(ctx context.Context, workflowID, nodeName string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	if err := m.redis.HSet(ctx, m.workflowNodeKey(workflowID, nodeName), values).Err(); err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(m.redis.Expire(ctx, m.workflowNodeKey(workflowID, nodeName), m.workflowRetention()).Err())
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
			return nil, nil, readErr
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
	}
	return meta, nodes, nil
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
	return WorkflowStartSpec{
		WorkflowID:  metaMap["workflowId"],
		Name:        metaMap["workflowName"],
		Queue:       metaMap["queue"],
		Targets:     decodeStringList(metaMap["targets"]),
		ShardTotal:  toInt(metaMap["shardTotal"]),
		GrayPercent: toInt(metaMap["grayPercent"]),
		Source:      metaMap["source"],
	}, nil
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
// 删除操作逐 key 执行，避免 Redis Cluster 多 key DEL 跨 slot；所有 key 都有 TTL，这里只是加速恢复。
func (m *Manager) cleanupIncompleteWorkflowMetadata(ctx context.Context, workflowID string, def *WorkflowDefinition) error {
	if m == nil || def == nil || strings.TrimSpace(workflowID) == "" {
		return nil
	}
	keys := []string{
		m.workflowMetaKey(workflowID),
		m.workflowNodesKey(workflowID),
		m.workflowCompletedKey(workflowID),
		m.workflowFailedKey(workflowID),
	}
	for _, nodeName := range sortedNodeNames(def) {
		keys = append(keys,
			m.workflowNodeKey(workflowID, nodeName),
			m.workflowNodeScheduledKey(workflowID, nodeName),
			m.workflowNodeFinalizedKey(workflowID, nodeName),
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
	lockKey := m.workflowUniqueKey(name, key)
	currentID, err := m.redis.Get(ctx, lockKey).Result()
	switch {
	case err == nil:
		if currentID != workflowID {
			return false, nil
		}
		return true, errors.Tag(m.redis.Expire(ctx, lockKey, ttl).Err())
	case err != redis.Nil:
		return false, errors.Tag(err)
	}
	return m.reserveWorkflowUnique(ctx, name, key, workflowID, ttl)
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
