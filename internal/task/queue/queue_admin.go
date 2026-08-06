package taskqueue

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	tasklimits "admin/internal/task/limits"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// taskQueueGroupReadLimit 限制单次聚合任务统计读取的组数，避免异常高基数组拖慢调度和日报。
	taskQueueGroupReadLimit int64 = 100
	// taskQueueMemorySampleSize 限制每种状态参与队列内存估算的任务数量。
	taskQueueMemorySampleSize int64 = 20
	// taskQueueMemoryGroupSampleSize 限制参与内存估算的聚合组数量。
	taskQueueMemoryGroupSampleSize = 5
)

// ListQueues 返回当前队列和在线 worker 的运行概览。
func (m *Manager) ListQueues(ctx context.Context) (*types.TaskQueueListResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 只展示 Worker 实际监听的配置队列，禁止调用 Inspector.Queues 全量读取共享 Redis 队列集合。
	queueNames := normalizedQueueNames(m.effectiveQueueNames())
	resp := &types.TaskQueueListResp{
		Queues: make([]types.TaskQueueItem, 0, len(queueNames)),
	}
	resp.Scheduler = m.schedulerStatusSnapshot(ctx)
	for _, queue := range queueNames {
		item, metricsLimited, statsErr := m.boundedQueueStats(ctx, queue)
		if statsErr != nil {
			return nil, errors.Tag(statsErr)
		}
		memoryUsage, memoryLimited, memoryErr := m.boundedQueueMemoryUsage(ctx, m.namespacedQueueName(queue), item)
		if memoryErr == nil {
			item.MemoryUsage = memoryUsage
		}
		resp.MetricsLimited = resp.MetricsLimited || metricsLimited || memoryLimited || memoryErr != nil
		resp.Queues = append(resp.Queues, item)
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

// queueMemorySample 描述一种任务状态的数量与有界样本。
type queueMemorySample struct {
	count int64                 // 当前状态总任务数
	ids   *redis.StringSliceCmd // 当前状态的有界任务 ID 样本
}

// boundedQueueMemoryUsage 复刻 Asynq 的抽样口径，但对状态、聚合组和任务数都设置硬上限。
func (m *Manager) boundedQueueMemoryUsage(ctx context.Context, internalQueue string, item types.TaskQueueItem) (int64, bool, error) {
	retryKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateRetry.String())
	if err != nil {
		return 0, false, errors.Tag(err)
	}
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateArchived.String())
	if err != nil {
		return 0, false, errors.Tag(err)
	}
	completedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateCompleted.String())
	if err != nil {
		return 0, false, errors.Tag(err)
	}
	stateKeys := []string{
		keys.TaskAsynqActiveKey(internalQueue),
		keys.TaskAsynqPendingKey(internalQueue),
		keys.TaskAsynqScheduledKey(internalQueue),
		retryKey,
		archivedKey,
		completedKey,
	}
	pipe := m.redis.Pipeline()
	samples := []queueMemorySample{
		{count: int64(item.Active), ids: pipe.LRange(ctx, stateKeys[0], 0, taskQueueMemorySampleSize-1)},
		{count: int64(item.Pending), ids: pipe.LRange(ctx, stateKeys[1], 0, taskQueueMemorySampleSize-1)},
		{count: int64(item.Scheduled), ids: pipe.ZRange(ctx, stateKeys[2], 0, taskQueueMemorySampleSize-1)},
		{count: int64(item.Retry), ids: pipe.ZRange(ctx, stateKeys[3], 0, taskQueueMemorySampleSize-1)},
		{count: int64(item.Archived), ids: pipe.ZRange(ctx, stateKeys[4], 0, taskQueueMemorySampleSize-1)},
		{count: int64(item.Completed), ids: pipe.ZRange(ctx, stateKeys[5], 0, taskQueueMemorySampleSize-1)},
	}
	baseMemory := make([]*redis.IntCmd, 0, len(stateKeys)+1)
	for _, key := range stateKeys {
		baseMemory = append(baseMemory, pipe.MemoryUsage(ctx, key))
	}
	groupsKey := keys.TaskAsynqGroupsKey(internalQueue)
	baseMemory = append(baseMemory, pipe.MemoryUsage(ctx, groupsKey))
	groupsCount := pipe.SCard(ctx, groupsKey)
	groupsCmd := pipe.SRandMemberN(ctx, groupsKey, taskQueueGroupReadLimit)
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, false, errors.Wrap(err, "读取队列内存样本失败")
	}
	limited := groupsCount.Val() > taskQueueGroupReadLimit
	groups := groupsCmd.Val()
	if limited {
		groups = nil
	}
	groupSampleCount := min(len(groups), taskQueueMemoryGroupSampleSize)
	groupMemoryPipe := m.redis.Pipeline()
	groupMemory := make([]*redis.IntCmd, 0, groupSampleCount)
	for index := 0; index < groupSampleCount; index++ {
		groupKey := keys.TaskAsynqGroupKey(internalQueue, groups[index])
		groupMemory = append(groupMemory, groupMemoryPipe.MemoryUsage(ctx, groupKey))
		samples = append(samples, queueMemorySample{
			count: int64(item.Aggregating),
			ids:   groupMemoryPipe.ZRange(ctx, groupKey, 0, taskQueueMemorySampleSize-1),
		})
	}
	if groupSampleCount > 0 {
		if _, err = groupMemoryPipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return 0, limited, errors.Wrap(err, "读取聚合组内存样本失败")
		}
	}
	taskMemoryPipe := m.redis.Pipeline()
	taskMemory := make(map[string]*redis.IntCmd)
	for _, sample := range samples {
		for _, taskID := range sample.ids.Val() {
			if _, exists := taskMemory[taskID]; exists {
				continue
			}
			taskMemory[taskID] = taskMemoryPipe.MemoryUsage(ctx, keys.TaskAsynqTaskHashKey(internalQueue, taskID))
		}
	}
	if len(taskMemory) > 0 {
		if _, err = taskMemoryPipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return 0, limited, errors.Wrap(err, "读取任务详情内存样本失败")
		}
	}
	var total int64
	for _, command := range baseMemory {
		total += max(command.Val(), 0)
	}
	if groupSampleCount > 0 {
		var sampledGroupMemory int64
		for _, command := range groupMemory {
			sampledGroupMemory += max(command.Val(), 0)
		}
		total += sampledGroupMemory * int64(len(groups)) / int64(groupSampleCount)
	}
	for index, sample := range samples {
		ids := sample.ids.Val()
		if sample.count <= 0 || len(ids) == 0 {
			continue
		}
		var sampledTaskMemory int64
		valid := int64(0)
		for _, taskID := range ids {
			command := taskMemory[taskID]
			if command == nil || command.Err() != nil {
				limited = true
				continue
			}
			sampledTaskMemory += max(command.Val(), 0)
			valid++
		}
		if valid == 0 {
			continue
		}
		count := sample.count
		if index >= 6 && groupSampleCount > 0 {
			count = int64(item.Aggregating) / int64(groupSampleCount)
		}
		total += sampledTaskMemory * count / valid
	}
	return total, limited, nil
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
		item, metricsLimited, err := m.boundedQueueStats(ctx, queue)
		if err != nil {
			return nil, limited, errors.Tag(err)
		}
		limited = limited || metricsLimited
		resp.Queues = append(resp.Queues, item)
	}
	resp.MetricsLimited = limited
	return resp, limited, nil
}

// boundedQueueStats 通过固定状态计数和有界聚合组计数读取队列状态。
// 同一队列的 Asynq key 共享 hash tag，Pipeline 可同时用于 Redis 单机和 Cluster。
func (m *Manager) boundedQueueStats(ctx context.Context, queue string) (types.TaskQueueItem, bool, error) {
	internalQueue := m.namespacedQueueName(queue)
	if internalQueue == "" {
		return types.TaskQueueItem{}, false, errors.Errorf("任务队列名称非法 queue=%s", queue)
	}
	retryKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateRetry.String())
	if err != nil {
		return types.TaskQueueItem{}, false, errors.Tag(err)
	}
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateArchived.String())
	if err != nil {
		return types.TaskQueueItem{}, false, errors.Tag(err)
	}
	completedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateCompleted.String())
	if err != nil {
		return types.TaskQueueItem{}, false, errors.Tag(err)
	}

	now := time.Now()
	pipe := m.redis.Pipeline()
	pending := pipe.LLen(ctx, keys.TaskAsynqPendingKey(internalQueue))
	active := pipe.LLen(ctx, keys.TaskAsynqActiveKey(internalQueue))
	scheduled := pipe.ZCard(ctx, keys.TaskAsynqScheduledKey(internalQueue))
	retry := pipe.ZCard(ctx, retryKey)
	archived := pipe.ZCard(ctx, archivedKey)
	completed := pipe.ZCard(ctx, completedKey)
	paused := pipe.Exists(ctx, keys.TaskAsynqPausedKey(internalQueue))
	processed := pipe.Get(ctx, keys.TaskAsynqDailyProcessedKey(internalQueue, now))
	failed := pipe.Get(ctx, keys.TaskAsynqDailyFailedKey(internalQueue, now))
	oldestPending := pipe.LIndex(ctx, keys.TaskAsynqPendingKey(internalQueue), -1)
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return types.TaskQueueItem{}, false, errors.Wrapf(err, "查询任务队列计数失败 queue=%s", queue)
	}
	aggregating, metricsLimited, err := m.boundedAggregatingTaskCount(ctx, internalQueue, taskQueueGroupReadLimit)
	if err != nil {
		return types.TaskQueueItem{}, false, errors.Wrapf(err, "查询任务聚合计数失败 queue=%s", queue)
	}
	latencyMS := int64(0)
	if taskID := strings.TrimSpace(oldestPending.Val()); taskID != "" {
		pendingSince, pendingErr := m.redis.HGet(ctx, keys.TaskAsynqTaskHashKey(internalQueue, taskID), "pending_since").Int64()
		if pendingErr != nil && !errors.Is(pendingErr, redis.Nil) {
			return types.TaskQueueItem{}, false, errors.Wrapf(pendingErr, "查询最老待执行任务时间失败 queue=%s", queue)
		}
		if pendingSince > 0 {
			latencyMS = max(now.Sub(time.Unix(0, pendingSince)).Milliseconds(), 0)
		}
	}
	item := types.TaskQueueItem{
		Name:        queue,
		Paused:      paused.Val() > 0,
		Pending:     int(pending.Val()),
		Active:      int(active.Val()),
		Scheduled:   int(scheduled.Val()),
		Retry:       int(retry.Val()),
		Archived:    int(archived.Val()),
		Completed:   int(completed.Val()),
		Aggregating: int(aggregating),
		Processed:   int(toInt64(processed.Val())),
		Failed:      int(toInt64(failed.Val())),
		LatencyMS:   latencyMS,
	}
	item.Size = item.Pending + item.Active + item.Scheduled + item.Retry + item.Archived + item.Completed + item.Aggregating
	return item, metricsLimited, nil
}

// aggregatingTaskCount 按固定组数上限累计 aggregating 数量，不调用 Inspector 的无界遍历。
func (m *Manager) aggregatingTaskCount(ctx context.Context, internalQueue string, groupLimit int64) (int64, error) {
	total, limited, err := m.boundedAggregatingTaskCount(ctx, internalQueue, groupLimit)
	if err != nil {
		return 0, errors.Tag(err)
	}
	if limited {
		return 0, errors.Errorf("任务聚合组数量超过读取上限 queue=%s limit=%d", internalQueue, groupLimit)
	}
	return total, nil
}

// boundedAggregatingTaskCount 在组数超限时返回降级标记，调用方不得继续无界遍历。
func (m *Manager) boundedAggregatingTaskCount(ctx context.Context, internalQueue string, groupLimit int64) (int64, bool, error) {
	if groupLimit <= 0 {
		return 0, false, errors.Errorf("任务聚合组读取上限必须大于 0")
	}
	groupsKey := keys.TaskAsynqGroupsKey(internalQueue)
	pipe := m.redis.Pipeline()
	groupsCount := pipe.SCard(ctx, groupsKey)
	groupsCmd := pipe.SRandMemberN(ctx, groupsKey, groupLimit)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, false, errors.Wrap(err, "查询任务聚合组失败")
	}
	if groupsCount.Val() > groupLimit {
		return 0, true, nil
	}
	groups := groupsCmd.Val()
	if len(groups) == 0 {
		return 0, false, nil
	}
	groupPipe := m.redis.Pipeline()
	commands := make([]*redis.IntCmd, 0, len(groups))
	for _, group := range groups {
		commands = append(commands, groupPipe.ZCard(ctx, keys.TaskAsynqGroupKey(internalQueue, group)))
	}
	if _, err := groupPipe.Exec(ctx); err != nil {
		return 0, false, errors.Wrap(err, "查询任务聚合任务数失败")
	}
	var total int64
	for _, command := range commands {
		total += command.Val()
	}
	return total, false, nil
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
	if !m.isQueueConfigured(req.Queue) {
		return errors.Errorf("任务队列未配置消费: %s", strings.TrimSpace(req.Queue))
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
	}, now, m.workflowManualRerunRetention().Milliseconds()).Int()
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
	if err = m.refreshWorkflowManualRerunRetention(ctx, meta.WorkflowID); err != nil {
		return errors.Wrapf(err, "刷新手工重跑工作流状态失败 workflow_id=%s node=%s shard=%d", meta.WorkflowID, meta.WorkflowNode, meta.ShardIndex)
	}
	repairResult, err := workflowArchivedTaskRerunRepairScript.Run(ctx, m.redis, []string{
		m.workflowNodeKey(meta.WorkflowID, meta.WorkflowNode),
	}, workflowNodeInstanceField(meta.ShardIndex), workflowNodeBusinessFailureField(meta.ShardIndex), now, m.workflowManualRerunRetention().Milliseconds()).Int()
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
	if err = m.refreshWorkflowManualRerunRetention(ctx, meta.WorkflowID); err != nil {
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
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
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
	if !m.isQueueConfigured(QueueMaintenance) {
		return errors.Errorf("缓存刷新队列未配置消费: %s", QueueMaintenance)
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
	if len(body) > tasklimits.MaxPayloadBytes {
		return errors.Errorf("缓存刷新任务负载超过上限 bytes=%d max=%d", len(body), tasklimits.MaxPayloadBytes)
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
		asynq.Retention(m.CompletedRetention()),
		asynq.Unique(15*time.Second),
	)
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return errors.Tag(err)
}
