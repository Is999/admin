package taskqueue

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// ListQueues 返回当前队列和在线 worker 的运行概览。
func (m *Manager) ListQueues(ctx context.Context) (*types.TaskQueueListResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	rawQueueNames, err := m.inspector.Queues()
	if err != nil {
		// 某些 Redis 托管环境会限制 Asynq `Queues()` 内部脚本命令，
		// 这里降级回配置中的静态队列名，保证运维页至少能看到核心队列概览。
		rawQueueNames = m.effectiveQueueNames()
		if len(rawQueueNames) == 0 {
			return nil, errors.Tag(err)
		}
	}
	queueNames := m.visibleQueueNames(rawQueueNames)
	sort.Strings(queueNames)
	resp := &types.TaskQueueListResp{
		Queues: make([]types.TaskQueueItem, 0, len(queueNames)),
	}
	resp.Scheduler = m.schedulerStatusSnapshot(ctx)
	for _, queue := range queueNames {
		info, infoErr := m.inspector.GetQueueInfo(m.namespacedQueueName(queue))
		if infoErr != nil {
			return nil, errors.Wrapf(infoErr, "查询任务队列概览失败 queue=%s", queue)
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

// ListReportQueues 返回日报所需的有界队列计数，不读取 scheduler 和 worker 详情。
func (m *Manager) ListReportQueues(ctx context.Context, limit int) (*types.TaskQueueListResp, bool, error) {
	if !m.IsEnabled() {
		return nil, false, ErrTaskQueueDisabled
	}
	if limit <= 0 {
		return nil, false, errors.Errorf("日报队列读取上限必须大于 0")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 日报只读取本站配置中允许投递的队列。禁止调用 Inspector.Queues/GetQueueInfo：
	// 后者会全量扫描共享 Redis 的队列、聚合组和任务内存，无法受日报 context 约束。
	queueNames := normalizedQueueNames(m.effectiveQueueNames())
	limited := len(queueNames) > limit
	if limited {
		queueNames = queueNames[:limit]
	}
	resp := &types.TaskQueueListResp{Queues: make([]types.TaskQueueItem, 0, len(queueNames))}
	for _, queue := range queueNames {
		if err := ctx.Err(); err != nil {
			return nil, limited, errors.Tag(err)
		}
		item, err := m.reportQueueStats(ctx, queue)
		if err != nil {
			return nil, limited, errors.Tag(err)
		}
		resp.Queues = append(resp.Queues, item)
	}
	return resp, limited, nil
}

// reportQueueStats 通过固定 6 条 O(1) 计数命令读取日报使用的队列状态。
// 同一队列的 Asynq key 共享 hash tag，Pipeline 可同时用于 Redis 单机和 Cluster。
func (m *Manager) reportQueueStats(ctx context.Context, queue string) (types.TaskQueueItem, error) {
	internalQueue := m.namespacedQueueName(queue)
	if internalQueue == "" {
		return types.TaskQueueItem{}, errors.Errorf("日报任务队列名称非法 queue=%s", queue)
	}
	retryKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateRetry.String())
	if err != nil {
		return types.TaskQueueItem{}, errors.Tag(err)
	}
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateArchived.String())
	if err != nil {
		return types.TaskQueueItem{}, errors.Tag(err)
	}
	completedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateCompleted.String())
	if err != nil {
		return types.TaskQueueItem{}, errors.Tag(err)
	}

	pipe := m.redis.Pipeline()
	pending := pipe.LLen(ctx, keys.TaskAsynqPendingKey(internalQueue))
	active := pipe.LLen(ctx, keys.TaskAsynqActiveKey(internalQueue))
	scheduled := pipe.ZCard(ctx, keys.TaskAsynqScheduledKey(internalQueue))
	retry := pipe.ZCard(ctx, retryKey)
	archived := pipe.ZCard(ctx, archivedKey)
	completed := pipe.ZCard(ctx, completedKey)
	if _, err = pipe.Exec(ctx); err != nil {
		return types.TaskQueueItem{}, errors.Wrapf(err, "查询日报任务队列计数失败 queue=%s", queue)
	}
	return types.TaskQueueItem{
		Name:      queue,
		Pending:   int(pending.Val()),
		Active:    int(active.Val()),
		Scheduled: int(scheduled.Val()),
		Retry:     int(retry.Val()),
		Archived:  int(archived.Val()),
		Completed: int(completed.Val()),
	}, nil
}

// normalizedQueueNames 去重并按核心队列优先级和名称稳定排序。
func normalizedQueueNames(queueNames []string) []string {
	seen := make(map[string]struct{}, len(queueNames))
	result := make([]string, 0, len(queueNames))
	for _, name := range queueNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	priority := map[string]int{
		QueueCritical:    0,
		QueueDefault:     1,
		QueueMaintenance: 2,
	}
	sort.Slice(result, func(i, j int) bool {
		leftPriority, leftCore := priority[result[i]]
		rightPriority, rightCore := priority[result[j]]
		if leftCore != rightCore {
			return leftCore
		}
		if leftCore && leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return result[i] < result[j]
	})
	return result
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

// effectiveQueueNames 从 Worker 有效权重中提取实际监听的逻辑队列名。
func (m *Manager) effectiveQueueNames() []string {
	weights := m.queueWeights()
	queueNames := make([]string, 0, len(weights))
	for queueName := range weights {
		queueName = m.displayQueueName(queueName)
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
	return m.effectiveQueueNames()
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
	if err = m.runInspectorTask(internalQueue, taskID); err != nil {
		return errors.Tag(err)
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
		return errors.Errorf("工作流节点记录不存在 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if err != nil {
		return errors.Wrapf(err, "读取工作流节点状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if status == NodeStatusSuccess || status == NodeStatusSkipped {
		return errors.Errorf("成功工作流节点不允许手工重跑 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	marker, err := m.redis.HGet(ctx, m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode), workflowNodeInstanceField(meta.ShardIndex)).Result()
	if err == redis.Nil {
		return errors.Errorf("工作流分片没有可修复的失败状态 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if err != nil {
		return errors.Wrapf(err, "读取工作流分片失败状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if marker != "failed" && marker != workflowNodeOutcomeRerunPrepared {
		return errors.Errorf("工作流分片状态不允许手工重跑 workflow_id=%s node=%s shard=%d status=%s", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex, marker)
	}
	now := time.Now().Format(time.RFC3339)
	reopenResult, err := reopenWorkflowForManualRerunScript.Run(ctx, m.redis, []string{
		m.workflowMetaKey(meta.WorkflowID),
	}, now).Int()
	if err != nil {
		return errors.Wrapf(err, "重开归档工作流失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	switch reopenResult {
	case -3:
		return errors.Errorf("工作流主记录不存在 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	case -2:
		return errors.Errorf("工作流不允许手工重跑 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	case -1:
		return errors.Errorf("成功工作流不允许手工重跑 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	if err = m.persistWorkflowManualRerunState(ctx, meta.WorkflowID); err != nil {
		return errors.Wrapf(err, "持久化手工重跑工作流状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	repairResult, err := workflowArchivedTaskRerunRepairScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), workflowNodeBusinessFailureField(meta.ShardIndex), now).Int()
	if err != nil {
		return errors.Wrapf(err, "修复归档工作流任务重跑状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	switch repairResult {
	case -2:
		return errors.Join(
			errors.Errorf("工作流分片没有可修复的失败状态 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex),
			m.restoreWorkflowTerminalRetention(ctx, meta.WorkflowID),
		)
	case -1:
		return errors.Join(
			errors.Errorf("成功工作流节点不允许回滚 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex),
			m.restoreWorkflowTerminalRetention(ctx, meta.WorkflowID),
		)
	}
	if err = m.persistWorkflowManualRerunState(ctx, meta.WorkflowID); err != nil {
		return errors.Tag(err)
	}
	def, err := m.workflowDefinition(meta.WorkflowName)
	if err != nil {
		return errors.Wrapf(err, "读取手工重跑工作流定义失败 workflow_id=%s workflow=%s", meta.WorkflowID, meta.WorkflowName)
	}
	spec, err := m.workflowSpecByID(ctx, meta.WorkflowID)
	if err != nil {
		return errors.Wrapf(err, "读取手工重跑工作流参数失败 workflow_id=%s workflow=%s", meta.WorkflowID, meta.WorkflowName)
	}
	if err = m.scheduleNode(ctx, def, spec, meta.WorkflowNode); err != nil {
		return errors.Wrapf(err, "补齐手工重跑工作流分片失败 workflow_id=%s node=%s", meta.WorkflowID, meta.WorkflowNode)
	}
	return nil
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
		asynq.Retention(taskCompletedRetention),
		asynq.Unique(15*time.Second),
	)
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return errors.Tag(err)
}
