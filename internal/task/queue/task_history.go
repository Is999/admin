package taskqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/audit"
	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"
	"admin/internal/task/stats"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// taskHistoryEventMaxBytes 限制单个完整终态事件，避免异常节点数据撑大 Redis 与 MySQL。
	taskHistoryEventMaxBytes = 128 << 10
	// taskHistoryWorkflowSnapshotMaxBytes 为事件元数据和落库状态预留空间。
	taskHistoryWorkflowSnapshotMaxBytes = 120 << 10
	// taskHistoryNodeDetailFloor 是保留分片指标时每个节点尽量保留的代表性明细数。
	taskHistoryNodeDetailFloor = 16
	// taskHistoryPendingMaxBytes 限制历史缓冲区总载荷，允许保留较完整快照但不放大 Redis 故障压力。
	taskHistoryPendingMaxBytes = 64 << 20
	// taskHistoryEnqueueTrimLimit 限制单次入队脚本最多裁剪的事件数，避免配置骤降时阻塞 Redis。
	taskHistoryEnqueueTrimLimit = 200
	// taskHistoryFlushBatchSize 限制单个 DB 事务和 Redis 确认批次。
	taskHistoryFlushBatchSize = 100
	// taskHistoryFlushMaxBatches 限制单次租约连续追赶批次，兼顾高频终态吞吐和数据库压力。
	taskHistoryFlushMaxBatches = 5
	// taskHistoryPersistTimeout 限制一次批量落库占用连接的时间。
	taskHistoryPersistTimeout = 3 * time.Second
	// taskHistoryCollectorLease 覆盖单轮最多五次短事务，避免多 Worker 在追赶期间重复刷新同一批事件。
	taskHistoryCollectorLease = 20 * time.Second
	// taskHistoryCleanupBatchSize 限制单表每次选取和删除的过期主键数。
	taskHistoryCleanupBatchSize = 500
	// taskHistoryCleanupMaxBatches 允许高频任务过期积压在三秒超时内受控追赶。
	taskHistoryCleanupMaxBatches = 20
	// taskHistoryObservationTimeout 限制观测接口单个 Redis/DB 数据源的等待时间。
	taskHistoryObservationTimeout = 2 * time.Second
)

// HistoryEvent 是 Redis 缓冲区和 DB 落库器之间的紧凑终态事件。
type HistoryEvent struct {
	EventID  string                        `json:"eventId"`            // 幂等事件 ID
	Kind     string                        `json:"kind"`               // 事件类型：task/workflow/failure
	Task     *types.TaskRunHistoryItem     `json:"task,omitempty"`     // 单个实际任务终态摘要
	Workflow *types.TaskWorkflowStatusResp `json:"workflow,omitempty"` // 工作流终态快照
	Failure  *types.TaskFailureItem        `json:"failure,omitempty"`  // 最终失败任务摘要
}

// HistoryEventValidationError 表示可安全隔离的确定性非法历史事件。
type HistoryEventValidationError struct {
	EventID string // EventID 是 Redis 缓冲区中的事件标识
	err     error  // err 是事件校验失败原因
}

// NewHistoryEventValidationError 创建确定性历史事件校验错误。
func NewHistoryEventValidationError(eventID string, err error) error {
	if err == nil {
		return nil
	}
	return &HistoryEventValidationError{EventID: strings.TrimSpace(eventID), err: err}
}

// Error 返回历史事件校验错误文本。
func (e *HistoryEventValidationError) Error() string {
	if e == nil || e.err == nil {
		return "任务历史事件校验失败"
	}
	return e.err.Error()
}

// Unwrap 返回原始校验错误。
func (e *HistoryEventValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// HistorySink 描述终态历史 DB 存储需要提供的最小能力。
type HistorySink interface {
	Persist(ctx context.Context, events []HistoryEvent) error
	GetTaskRun(ctx context.Context, id uint64) (*types.TaskRunHistoryItem, error)
	ListTaskRuns(ctx context.Context, req *types.ListTaskRunsReq) (*types.TaskRunHistoryListResp, error)
	GetWorkflow(ctx context.Context, workflowID string) (*types.TaskWorkflowStatusResp, error)
	ListWorkflows(ctx context.Context, req *types.ListTaskWorkflowsReq) (*types.TaskWorkflowHistoryListResp, error)
	ListFailures(ctx context.Context, req *types.ListTaskFailuresReq) (*types.TaskFailureListResp, error)
	WindowSummary(ctx context.Context, start time.Time, end time.Time) (types.TaskHistoryWindowSummary, error)
	Cleanup(ctx context.Context, taskBefore time.Time, workflowBefore time.Time, failureBefore time.Time, limit int) (int64, error)
}

// AttachHistorySink 绑定 DB 历史存储；API 实例只提供查询，Worker 启动后才运行收集器。
func (m *Manager) AttachHistorySink(sink HistorySink) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.historySink = sink
	m.mu.Unlock()
}

// enqueueWorkflowHistory 把终态有界快照幂等写入 Redis 缓冲区。
func (m *Manager) enqueueWorkflowHistory(ctx context.Context, workflowID string) {
	if !m.historyEnabled() {
		return
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	status, err := m.getWorkflowStatusFromRedis(writeCtx, workflowID)
	if err != nil {
		loggerx.Errorw(writeCtx, "工作流历史快照读取失败", err, logx.Field("workflow_id", workflowID))
		return
	}
	status.DataSource = "redis"
	status.HistoryStatus = "pending"
	boundWorkflowHistorySnapshot(status)
	event := HistoryEvent{
		EventID:  taskHistoryEventID("workflow", status.WorkflowID, status.Status, status.FinishedAt),
		Kind:     "workflow",
		Workflow: status,
	}
	if err = m.enqueueHistoryEvent(writeCtx, event); err != nil {
		m.recordHistoryFailure(writeCtx, err, logx.Field("workflow_id", workflowID))
	}
}

// boundWorkflowHistorySnapshot 按固定容量逐级裁剪历史明细，优先保留可观测信息。
func boundWorkflowHistorySnapshot(status *types.TaskWorkflowStatusResp) {
	if status == nil {
		return
	}
	status.Targets = nil
	status.DetailTruncated = false
	status.ExecutionTrace = historyTraceWithoutDetails(status.ExecutionTrace)
	status.DetailLevel = workflowHistoryDetailLevel(status.Nodes)
	for index := range status.Nodes {
		status.Nodes[index].ErrorMessage = truncateTaskText(audit.SanitizeText(status.Nodes[index].ErrorMessage, 4000), 1000)
	}
	status.ErrorMessage = truncateTaskText(audit.SanitizeText(status.ErrorMessage, 4000), 1000)
	if workflowHistorySnapshotFits(status) {
		return
	}

	status.DetailTruncated = true
	for nodeIndex := range status.Nodes {
		for shardIndex := range status.Nodes[nodeIndex].ShardTraces {
			compactWorkflowShardHistory(&status.Nodes[nodeIndex].ShardTraces[shardIndex])
		}
	}
	if workflowHistorySnapshotFits(status) {
		return
	}

	// 节点 details 通常由分片聚合而来，先缩减到代表性样本，尽量同时保留分片指标和节点明细。
	for !workflowHistorySnapshotFits(status) && trimWorkflowNodeTraceDetails(status.Nodes, taskHistoryNodeDetailFloor) {
	}
	if workflowHistorySnapshotFits(status) {
		return
	}

	for index := range status.Nodes {
		status.Nodes[index].ShardTraces = nil
	}
	status.DetailLevel = "node"
	// 极端节点明细在移除分片后继续有界裁剪，保证完整事件不会因单个异常快照被拒绝。
	for !workflowHistorySnapshotFits(status) && trimWorkflowNodeTraceDetails(status.Nodes, 0) {
	}
}

// compactWorkflowShardHistory 移除终态分片中可由状态推导或与节点重复的字段，保留处理量与耗时指标。
func compactWorkflowShardHistory(shard *types.TaskWorkflowShardTraceItem) {
	if shard == nil {
		return
	}
	shard.Progress = nil
	if shard.ExecutionTrace == nil {
		return
	}
	trace := *shard.ExecutionTrace
	trace.Name = ""
	trace.StartedAt = ""
	trace.FinishedAt = ""
	trace.Details = nil
	shard.ExecutionTrace = &trace
}

// workflowHistoryDetailLevel 根据快照中是否存在分片信息返回实际明细层级。
func workflowHistoryDetailLevel(nodes []types.TaskWorkflowNodeItem) string {
	for _, node := range nodes {
		if len(node.ShardTraces) > 0 {
			return "shard"
		}
	}
	return "node"
}

// workflowHistorySnapshotFits 校验工作流快照是否落在独立容量预算内。
func workflowHistorySnapshotFits(status *types.TaskWorkflowStatusResp) bool {
	raw, err := json.Marshal(status)
	return err == nil && len(raw) <= taskHistoryWorkflowSnapshotMaxBytes
}

// trimWorkflowNodeTraceDetails 每轮对半裁剪节点聚合明细，并保留指定数量的代表信息。
func trimWorkflowNodeTraceDetails(nodes []types.TaskWorkflowNodeItem, floor int) bool {
	trimmed := false
	for index := range nodes {
		trace := nodes[index].ExecutionTrace
		if trace == nil || len(trace.Details) <= floor {
			continue
		}
		limit := len(trace.Details) / 2
		if limit < floor {
			limit = floor
		}
		trace.Details = trace.Details[:limit]
		trimmed = true
	}
	return trimmed
}

// historyTraceWithoutDetails 复制聚合快照并移除重复的高基数明细。
func historyTraceWithoutDetails(snapshot *taskstats.Snapshot) *taskstats.Snapshot {
	if snapshot == nil {
		return nil
	}
	result := *snapshot
	result.Details = nil
	return &result
}

// enqueueTaskRunHistory 保存每个实际任务的终态摘要；工作流任务不重复保存大明细。
func (m *Manager) enqueueTaskRunHistory(ctx context.Context, task *asynq.Task, attemptToken string, startedAt time.Time, snapshot *taskstats.Snapshot, runErr error) {
	if !m.historyEnabled() || task == nil {
		return
	}
	workflowMeta := workflowTaskMetaFromTask(task)
	taskID, _ := asynq.GetTaskID(ctx)
	if strings.TrimSpace(workflowMeta.WorkflowID) == "" && task.Type() == TypeWorkflowTrigger {
		workflowMeta.WorkflowID = taskID
	}
	queue, _ := asynq.GetQueueName(ctx)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	if startedAt.IsZero() || snapshot == nil {
		runtime := m.readTaskRuntime(ctx, queue, taskID)
		if startedAt.IsZero() {
			startedAt, _ = time.Parse(time.RFC3339Nano, runtime.StartedAt)
		}
		if snapshot == nil {
			snapshot = runtime.ExecutionTrace
		}
	}
	finishedAt := time.Now()
	if startedAt.IsZero() || startedAt.After(finishedAt) {
		startedAt = finishedAt
	}
	status := taskExecutionStatusSuccess
	errorMessage := ""
	if runErr != nil {
		status = "failed"
		errorMessage = truncateTaskText(audit.SanitizeText(runErr.Error(), 4000), 1000)
	}
	traceID := ""
	durationMS := finishedAt.Sub(startedAt).Milliseconds()
	if requestMeta := requestctx.FromContext(ctx); requestMeta != nil {
		traceID = strings.TrimSpace(requestMeta.TraceID)
		if requestMeta.LatencyMS > 0 {
			durationMS = requestMeta.LatencyMS
		}
	}
	boundedSnapshot := boundedTaskStatsSnapshot(snapshot)
	detailSnapshot := boundedSnapshot
	if strings.TrimSpace(workflowMeta.WorkflowID) != "" {
		detailSnapshot = nil
	}
	item := &types.TaskRunHistoryItem{
		TaskID: taskID, TaskType: strings.TrimSpace(task.Type()), TaskName: taskNameFromTask(task),
		Queue: m.displayQueueName(queue), Source: strings.TrimSpace(task.Headers()[headerTaskSource]),
		PeriodicName: strings.TrimSpace(task.Headers()[HeaderPeriodicName]), Status: status,
		WorkflowID: workflowMeta.WorkflowID, WorkflowName: workflowMeta.WorkflowName,
		WorkflowNode: workflowMeta.WorkflowNode, ShardIndex: workflowMeta.ShardIndex, ShardTotal: workflowMeta.ShardTotal,
		Retried: retried, MaxRetry: maxRetry, TraceID: traceID, DurationMS: max(durationMS, 0),
		ErrorMessage: errorMessage, ExecutionTrace: detailSnapshot,
		StartedAt: startedAt.Format(time.RFC3339Nano), FinishedAt: finishedAt.Format(time.RFC3339Nano), DataSource: "redis",
	}
	if boundedSnapshot != nil {
		item.TraceTotal = boundedSnapshot.TotalCount
		item.TraceRead = boundedSnapshot.ReadCount
		item.TraceWrite = boundedSnapshot.InsertCount + boundedSnapshot.UpdateCount + boundedSnapshot.UpsertCount
		item.TraceDelete = boundedSnapshot.DeleteCount
		item.TraceError = boundedSnapshot.ErrorCount
	}
	if strings.TrimSpace(attemptToken) == "" {
		attemptToken = m.taskFailureAttemptIdentity(ctx, queue, taskID, retried, maxRetry)
	}
	event := HistoryEvent{
		EventID: taskHistoryEventID("task", item.Queue, item.TaskID, attemptToken, item.Status),
		Kind:    "task", Task: item,
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	if err := m.enqueueHistoryEvent(writeCtx, event); err != nil {
		m.recordHistoryFailure(writeCtx, err, logx.Field("task_id", taskID))
	}
}

// enqueueTaskFailureHistory 保存最终失败任务的最小排障信息，不复制原始 payload/result。
func (m *Manager) enqueueTaskFailureHistory(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error) {
	if !m.historyEnabled() || task == nil || runErr == nil {
		return
	}
	taskID, _ := asynq.GetTaskID(ctx)
	queue, _ := asynq.GetQueueName(ctx)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	failedAt := time.Now()
	traceID := ""
	if requestMeta := requestctx.FromContext(ctx); requestMeta != nil {
		traceID = requestMeta.TraceID
	}
	failure := &types.TaskFailureItem{
		TaskID:       strings.TrimSpace(taskID),
		TaskType:     strings.TrimSpace(task.Type()),
		TaskName:     taskNameFromTask(task),
		Queue:        m.displayQueueName(queue),
		Source:       strings.TrimSpace(task.Headers()[headerTaskSource]),
		PeriodicName: strings.TrimSpace(task.Headers()[HeaderPeriodicName]),
		WorkflowID:   meta.WorkflowID,
		WorkflowName: meta.WorkflowName,
		WorkflowNode: meta.WorkflowNode,
		Retried:      retried,
		MaxRetry:     maxRetry,
		ErrorMessage: truncateTaskText(audit.SanitizeText(runErr.Error(), 4000), 1000),
		TraceID:      strings.TrimSpace(traceID),
		FailedAt:     failedAt.Format(time.RFC3339Nano),
		DataSource:   "redis",
	}
	attemptIdentity := m.taskFailureAttemptIdentity(ctx, queue, taskID, retried, maxRetry)
	event := HistoryEvent{
		EventID: taskHistoryEventID("failure", failure.Queue, failure.TaskID, failure.WorkflowNode, attemptIdentity),
		Kind:    "failure",
		Failure: failure,
	}
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	if err := m.enqueueHistoryEvent(writeCtx, event); err != nil {
		m.recordHistoryFailure(writeCtx, err, logx.Field("task_id", taskID))
	}
}

// taskFailureAttemptIdentity 优先使用运行快照 attemptToken，保证同一次终态回调重复到达时幂等。
func (m *Manager) taskFailureAttemptIdentity(ctx context.Context, queue string, taskID string, retried int, maxRetry int) string {
	if m != nil && m.redis != nil {
		key := m.taskRuntimeKey(queue, taskID)
		if key != "" {
			if token := strings.TrimSpace(m.redis.HGet(ctx, key, "attemptToken").Val()); token != "" {
				return token
			}
		}
	}
	return strconv.Itoa(retried) + ":" + strconv.Itoa(maxRetry)
}

// enqueueHistoryEvent 原子写入事件并裁剪超出硬上限的最旧记录。
func (m *Manager) enqueueHistoryEvent(ctx context.Context, event HistoryEvent) error {
	if m == nil || m.redis == nil || strings.TrimSpace(event.EventID) == "" {
		return nil
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return errors.Wrap(err, "序列化任务历史事件失败")
	}
	if len(raw) > taskHistoryEventMaxBytes {
		return errors.Errorf("任务历史事件超过上限 event_id=%s bytes=%d max=%d", event.EventID, len(raw), taskHistoryEventMaxBytes)
	}
	_, err = taskHistoryEnqueueScript.Run(ctx, m.redis, []string{
		m.historyEventsKey(), m.historyOrderKey(), m.historyStatusKey(),
	}, event.EventID, raw, time.Now().UnixMilli(), m.historyPendingLimit(), taskHistoryPendingMaxBytes, taskHistoryEnqueueTrimLimit).Result()
	return errors.Tag(err)
}

// startHistoryCollectorLocked 启动有界终态历史收集器；调用方必须持有 lifecycleMu。
func (m *Manager) startHistoryCollectorLocked() {
	if m == nil || m.historyStop != nil || !m.historyEnabled() {
		return
	}
	m.mu.RLock()
	sink := m.historySink
	m.mu.RUnlock()
	if sink == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.historyStop = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runHistoryCollector(ctx, sink)
	}()
}

// runHistoryCollector 按短租约串行批量落库，DB 异常时保留 Redis 事件并退避。
func (m *Manager) runHistoryCollector(ctx context.Context, sink HistorySink) {
	flushTicker := time.NewTicker(m.historyFlushInterval())
	cleanupTicker := time.NewTicker(m.historyCleanupInterval())
	defer flushTicker.Stop()
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			m.flushHistory(ctx, sink)
		case <-cleanupTicker.C:
			m.cleanupHistory(ctx, sink)
		}
	}
}

// flushHistory 每次租约最多连续持久化五个有界批次，及时追平高频终态且不无界占用数据库。
func (m *Manager) flushHistory(ctx context.Context, sink HistorySink) {
	if !m.acquireHistoryLease(ctx) {
		return
	}
	defer m.releaseHistoryLease()
	for range taskHistoryFlushMaxBatches {
		events, ids, readErr := m.readPendingHistoryBatch(ctx, taskHistoryFlushBatchSize)
		if readErr != nil {
			m.recordHistoryFailure(ctx, readErr)
			return
		}
		if len(events) == 0 {
			return
		}
		persistCtx, cancel := context.WithTimeout(ctx, taskHistoryPersistTimeout)
		persistErr := sink.Persist(persistCtx, events)
		cancel()
		if persistErr != nil {
			var validationErr *HistoryEventValidationError
			if errors.As(persistErr, &validationErr) && validationErr.EventID != "" && slices.Contains(ids, validationErr.EventID) {
				if ackErr := m.acknowledgeHistoryEvents(ctx, []string{validationErr.EventID}, 1); ackErr != nil {
					m.recordHistoryFailure(ctx, ackErr, logx.Field("event_id", validationErr.EventID))
					return
				}
				m.recordHistoryDrop(ctx, persistErr, logx.Field("event_id", validationErr.EventID))
				continue
			}
			m.recordHistoryFailure(ctx, persistErr)
			return
		}
		if ackErr := m.acknowledgeHistoryEvents(ctx, ids, len(ids)-len(events)); ackErr != nil {
			m.recordHistoryFailure(ctx, ackErr)
			return
		}
		_ = m.redis.HSet(ctx, m.historyStatusKey(), "lastPersistedAtMs", time.Now().UnixMilli(), "lastError", "").Err()
		if len(ids) < taskHistoryFlushBatchSize {
			return
		}
	}
}

// readPendingHistoryBatch 批量读取最老事件；缺失 payload 会被确认脚本顺带清理。
func (m *Manager) readPendingHistoryBatch(ctx context.Context, limit int64) ([]HistoryEvent, []string, error) {
	ids, err := m.redis.ZRange(ctx, m.historyOrderKey(), 0, limit-1).Result()
	if err != nil || len(ids) == 0 {
		return nil, nil, errors.Tag(err)
	}
	values, err := m.redis.HMGet(ctx, m.historyEventsKey(), ids...).Result()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	events := make([]HistoryEvent, 0, len(ids))
	ackIDs := make([]string, 0, len(ids))
	for index, value := range values {
		raw, ok := value.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			ackIDs = append(ackIDs, ids[index])
			continue
		}
		var event HistoryEvent
		if json.Unmarshal([]byte(raw), &event) != nil || event.EventID != ids[index] {
			ackIDs = append(ackIDs, ids[index])
			continue
		}
		events = append(events, event)
		ackIDs = append(ackIDs, ids[index])
	}
	if len(events) == 0 && len(ackIDs) > 0 {
		if err = m.acknowledgeHistoryEvents(ctx, ackIDs, len(ackIDs)); err != nil {
			return nil, nil, errors.Tag(err)
		}
	}
	return events, ackIDs, nil
}

// acknowledgeHistoryEvents 原子确认事件并记录其中被隔离的坏数据数量。
func (m *Manager) acknowledgeHistoryEvents(ctx context.Context, ids []string, dropped int) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, max(dropped, 0))
	args = append(args, stringSliceToAny(ids)...)
	_, err := taskHistoryAckScript.Run(ctx, m.redis, []string{
		m.historyEventsKey(), m.historyOrderKey(), m.historyStatusKey(),
	}, args...).Result()
	return errors.Tag(err)
}

// cleanupHistory 小批量清理超出保留期的 DB 历史，避免形成新的长期大表。
func (m *Manager) cleanupHistory(ctx context.Context, sink HistorySink) {
	if !m.acquireHistoryLease(ctx) {
		return
	}
	defer m.releaseHistoryLease()
	now := time.Now()
	cleanupCtx, cancel := context.WithTimeout(ctx, taskHistoryPersistTimeout)
	defer cancel()
	for range taskHistoryCleanupMaxBatches {
		deleted, err := sink.Cleanup(cleanupCtx, now.Add(-m.historyTaskRetention()), now.Add(-m.historyWorkflowRetention()), now.Add(-m.historyFailureRetention()), taskHistoryCleanupBatchSize)
		if err != nil {
			m.recordHistoryFailure(ctx, err)
			return
		}
		if deleted < taskHistoryCleanupBatchSize {
			return
		}
	}
}

// historyTaskRetention 返回全部任务终态摘要的短保留期。
func (m *Manager) historyTaskRetention() time.Duration {
	if days := m.CurrentConfig().History.TaskRetentionDays; days > 0 {
		return time.Duration(days) * 24 * time.Hour
	}
	return 24 * time.Hour
}

// acquireHistoryLease 防止多 Worker 同时落库或清理同一批历史。
func (m *Manager) acquireHistoryLease(ctx context.Context) bool {
	locked, err := m.redis.SetNX(ctx, m.historyLockKey(), m.instance, taskHistoryCollectorLease).Result()
	return err == nil && locked
}

// releaseHistoryLease 仅释放当前实例仍持有的历史短租约。
func (m *Manager) releaseHistoryLease() {
	ctx, cancel := context.WithTimeout(context.Background(), taskFinalWriteTimeout)
	defer cancel()
	_, _ = releaseLeaderScript.Run(ctx, m.redis, []string{m.historyLockKey()}, m.instance).Result()
}

// recordHistoryFailure 记录脱敏后的缓冲或落库异常，不把历史链路错误带回业务任务。
func (m *Manager) recordHistoryFailure(ctx context.Context, runErr error, fields ...logx.LogField) {
	message := taskHistoryErrorText(runErr)
	_ = m.redis.HSet(ctx, m.historyStatusKey(), "lastFailureAtMs", time.Now().UnixMilli(), "lastError", message).Err()
	loggerx.Errorw(ctx, "任务历史异步落库失败", runErr, fields...)
}

// recordHistoryDrop 记录已成功隔离的非法事件；它不污染链路健康状态，也不掩盖既有基础设施故障。
func (m *Manager) recordHistoryDrop(ctx context.Context, runErr error, fields ...logx.LogField) {
	_ = m.redis.HSet(ctx, m.historyStatusKey(), "lastFailureAtMs", time.Now().UnixMilli()).Err()
	loggerx.Errorw(ctx, "任务历史非法事件已隔离", runErr, fields...)
}

// taskHistoryErrorText 脱敏并截断落库链路异常，避免运维接口泄露连接信息。
func taskHistoryErrorText(runErr error) string {
	if runErr == nil {
		return ""
	}
	return truncateTaskText(audit.SanitizeText(runErr.Error(), 2000), 500)
}

// historyEnabled 判断历史落库是否同时具备配置和存储实现。
func (m *Manager) historyEnabled() bool {
	if m == nil || !m.CurrentConfig().History.EnabledOrDefault() {
		return false
	}
	m.mu.RLock()
	enabled := m.historySink != nil
	m.mu.RUnlock()
	return enabled
}

// historyPendingLimit 返回 Redis 缓冲区事件硬上限。
func (m *Manager) historyPendingLimit() int {
	if limit := m.CurrentConfig().History.PendingLimit; limit > 0 {
		return limit
	}
	return 2000
}

// historyFlushInterval 返回异步落库刷新间隔。
func (m *Manager) historyFlushInterval() time.Duration {
	if seconds := m.CurrentConfig().History.FlushIntervalSeconds; seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 5 * time.Second
}

// historyCleanupInterval 返回 DB 历史清理间隔。
func (m *Manager) historyCleanupInterval() time.Duration {
	if seconds := m.CurrentConfig().History.CleanupIntervalSeconds; seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Minute
}

// historyWorkflowRetention 返回工作流历史短保留期。
func (m *Manager) historyWorkflowRetention() time.Duration {
	if days := m.CurrentConfig().History.WorkflowRetentionDays; days > 0 {
		return time.Duration(days) * 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// historyFailureRetention 返回失败历史短保留期。
func (m *Manager) historyFailureRetention() time.Duration {
	if days := m.CurrentConfig().History.FailureRetentionDays; days > 0 {
		return time.Duration(days) * 24 * time.Hour
	}
	return 14 * 24 * time.Hour
}

// historyEventsKey 返回待落库事件 Hash key。
func (m *Manager) historyEventsKey() string { return keys.TaskHistoryPendingEventsKey() }

// historyOrderKey 返回待落库事件顺序 ZSet key。
func (m *Manager) historyOrderKey() string { return keys.TaskHistoryPendingOrderKey() }

// historyStatusKey 返回收集器状态 Hash key。
func (m *Manager) historyStatusKey() string { return keys.TaskHistoryStatusKey() }

// historyLockKey 返回收集器短租约 key。
func (m *Manager) historyLockKey() string { return keys.TaskHistoryCollectorLockKey() }

// taskHistoryEventID 生成固定长度幂等键，避免业务 ID 组合超过索引长度。
func taskHistoryEventID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// truncateTaskText 按 rune 截断任务错误摘要，避免 Redis、DB 和 API 输出无界增长。
func truncateTaskText(text string, limit int) string {
	items := []rune(strings.TrimSpace(text))
	if len(items) <= limit {
		return string(items)
	}
	return string(items[:limit])
}

// stringSliceToAny 把 Redis 脚本参数转换为可变参数切片。
func stringSliceToAny(items []string) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}

// historySinkSnapshot 返回当前 DB 历史存储实现。
func (m *Manager) historySinkSnapshot() HistorySink {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.historySink
}

// GetTaskRunHistory 查询任务终态历史详情。
func (m *Manager) GetTaskRunHistory(ctx context.Context, id uint64) (*types.TaskRunHistoryItem, error) {
	sink := m.historySinkSnapshot()
	if !m.historyEnabled() || sink == nil {
		return nil, ErrTaskQueueDisabled
	}
	queryCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
	defer cancel()
	return sink.GetTaskRun(queryCtx, id)
}

// ListTaskRuns 查询 DB 中全部实际任务的短期终态历史。
func (m *Manager) ListTaskRuns(ctx context.Context, req *types.ListTaskRunsReq) (*types.TaskRunHistoryListResp, error) {
	if req == nil {
		return nil, errors.Errorf("任务历史查询不能为空")
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
	}
	sink := m.historySinkSnapshot()
	if !m.historyEnabled() || sink == nil {
		return nil, ErrTaskQueueDisabled
	}
	queryCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
	defer cancel()
	return sink.ListTaskRuns(queryCtx, req)
}

// historyPendingHealth 读取 O(1) 缓冲区健康状态。
func (m *Manager) historyPendingHealth(ctx context.Context) (types.TaskHistoryHealth, error) {
	health := types.TaskHistoryHealth{Enabled: m.historyEnabled(), Status: "disabled"}
	if !health.Enabled {
		return health, nil
	}
	pipe := m.redis.Pipeline()
	pendingCmd := pipe.ZCard(ctx, m.historyOrderKey())
	oldestCmd := pipe.ZRangeWithScores(ctx, m.historyOrderKey(), 0, 0)
	statusCmd := pipe.HGetAll(ctx, m.historyStatusKey())
	_, err := pipe.Exec(ctx)
	pipe.Discard()
	if err != nil && !errors.Is(err, redis.Nil) {
		return health, errors.Tag(err)
	}
	health.Pending = pendingCmd.Val()
	status := statusCmd.Val()
	health.PendingBytes = toInt64(status["pendingBytes"])
	health.PendingMaxBytes = taskHistoryPendingMaxBytes
	health.Dropped = toInt64(status["dropped"])
	health.LastPersistedAt = formatHistoryUnixMS(status["lastPersistedAtMs"])
	health.LastFailureAt = formatHistoryUnixMS(status["lastFailureAtMs"])
	health.LastError = status["lastError"]
	health.Status = "healthy"
	if oldest := oldestCmd.Val(); len(oldest) > 0 {
		oldestAt := time.UnixMilli(int64(oldest[0].Score))
		health.OldestPendingAt = oldestAt.Format(time.RFC3339)
		health.DelaySeconds = max(int64(time.Since(oldestAt).Seconds()), 0)
		if health.DelaySeconds > int64((3 * m.historyFlushInterval()).Seconds()) {
			health.Status = "delayed"
		}
	}
	if health.LastError != "" {
		health.Status = "error"
	}
	return health, nil
}

// formatHistoryUnixMS 格式化 Redis 中的毫秒时间戳。
func formatHistoryUnixMS(raw string) string {
	value := toInt64(raw)
	if value <= 0 {
		return ""
	}
	return time.UnixMilli(value).Format(time.RFC3339)
}

// ListTaskWorkflows 查询 DB 中的短期工作流历史。
func (m *Manager) ListTaskWorkflows(ctx context.Context, req *types.ListTaskWorkflowsReq) (*types.TaskWorkflowHistoryListResp, error) {
	sink := m.historySinkSnapshot()
	if sink == nil {
		return nil, errors.Errorf("任务历史落库未启用")
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
	defer cancel()
	return sink.ListWorkflows(queryCtx, req)
}

// ListTaskFailures 查询 DB 失败历史并用 Redis archived 精确校验重跑能力。
func (m *Manager) ListTaskFailures(ctx context.Context, req *types.ListTaskFailuresReq) (*types.TaskFailureListResp, error) {
	sink := m.historySinkSnapshot()
	if sink == nil {
		return nil, errors.Errorf("任务历史落库未启用")
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
	resp, err := sink.ListFailures(queryCtx, req)
	cancel()
	if err != nil || resp == nil || len(resp.Items) == 0 {
		return resp, errors.Tag(err)
	}
	redisCtx, redisCancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
	defer redisCancel()
	commands := make([]*redis.FloatCmd, len(resp.Items))
	pipe := m.redis.Pipeline()
	for index := range resp.Items {
		item := &resp.Items[index]
		internalQueue := m.namespacedQueueName(item.Queue)
		archivedKey, keyErr := keys.TaskAsynqStateZSetKey(internalQueue, asynq.TaskStateArchived.String())
		if keyErr != nil {
			continue
		}
		commands[index] = pipe.ZScore(redisCtx, archivedKey, item.TaskID)
	}
	_, pipelineErr := pipe.Exec(redisCtx)
	pipe.Discard()
	if pipelineErr != nil && !errors.Is(pipelineErr, redis.Nil) {
		resp.RerunCheckError = taskHistoryErrorText(pipelineErr)
		return resp, nil
	}
	for index := range resp.Items {
		if commands[index] == nil || commands[index].Err() != nil {
			continue
		}
		resp.Items[index].Rerunnable = true
		expireAt := time.Unix(int64(commands[index].Val()), 0).Add(m.ArchivedRetention())
		resp.Items[index].RerunExpireAt = expireAt.Format(time.RFC3339)
	}
	return resp, nil
}

// TaskObservability 返回 Redis 实时态和 DB 历史态的独立降级观测摘要。
func (m *Manager) TaskObservability(ctx context.Context) (*types.TaskObservabilityResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	resp := &types.TaskObservabilityResp{
		GeneratedAt: now.Format(time.RFC3339),
		Redis: types.TaskRedisMemory{
			CompletedTTL: int(m.CompletedRetention().Seconds()),
			ArchivedTTL:  int(m.ArchivedRetention().Seconds()),
		},
		History: types.TaskHistoryHealth{Enabled: m.historyEnabled(), Status: "disabled"},
		Last24Hours: types.TaskHistoryWindowSummary{
			WindowStart: now.Add(-24 * time.Hour).Format(time.RFC3339),
			WindowEnd:   now.Format(time.RFC3339),
		},
	}
	var (
		memoryInfo string
		memoryErr  error
		health     types.TaskHistoryHealth
		healthErr  error
		summary    types.TaskHistoryWindowSummary
		summaryErr error
	)
	sink := m.historySinkSnapshot()
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		memoryCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
		defer cancel()
		memoryInfo, memoryErr = m.redis.Info(memoryCtx, "memory").Result()
	}()
	go func() {
		defer wait.Done()
		healthCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
		defer cancel()
		health, healthErr = m.historyPendingHealth(healthCtx)
	}()
	if sink != nil {
		wait.Add(1)
		go func() {
			defer wait.Done()
			databaseCtx, cancel := context.WithTimeout(ctx, taskHistoryObservationTimeout)
			defer cancel()
			summary, summaryErr = sink.WindowSummary(databaseCtx, now.Add(-24*time.Hour), now)
		}()
	}
	wait.Wait()

	if memoryErr != nil {
		if resp.RedisError == "" {
			resp.RedisError = taskHistoryErrorText(memoryErr)
		}
	} else {
		resp.Redis.UsedBytes = redisInfoInt64(memoryInfo, "used_memory")
		resp.Redis.MaxBytes = redisInfoInt64(memoryInfo, "maxmemory")
		if resp.Redis.MaxBytes > 0 {
			resp.Redis.UsagePercent = float64(resp.Redis.UsedBytes) * 100 / float64(resp.Redis.MaxBytes)
		}
	}
	if healthErr != nil {
		resp.HistoryError = taskHistoryErrorText(healthErr)
	} else {
		resp.History = health
	}
	if sink != nil {
		if summaryErr != nil {
			resp.HistoryError = taskHistoryErrorText(summaryErr)
		} else {
			resp.Last24Hours = summary
		}
	}
	return resp, nil
}

// redisInfoInt64 从 INFO 文本读取整数指标。
func redisInfoInt64(info string, name string) int64 {
	prefix := strings.TrimSpace(name) + ":"
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
		return value
	}
	return 0
}
