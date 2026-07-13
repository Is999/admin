package taskqueue

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// taskRuntimeHeartbeatInterval 是任务执行中运行指标写入 Redis 的固定间隔。
	taskRuntimeHeartbeatInterval = 5 * time.Second
	// taskRuntimeHeartbeatWriteTimeout 限制单次心跳写入，避免任务收尾被异常 Redis 请求长期阻塞。
	taskRuntimeHeartbeatWriteTimeout = 2 * time.Second
	// taskRuntimeReportEntryOverheadBytes 为每条运行快照预留 Redis 回包和容器内存裕量。
	taskRuntimeReportEntryOverheadBytes = 256
	// maxTaskExecutionTraceBytes 限制单个任务写入 Redis 和 Asynq 结果的处理量快照大小。
	maxTaskExecutionTraceBytes = 64 << 10
	// maxTaskExecutionTraceDetails 限制单个任务保留的处理量明细数量。
	maxTaskExecutionTraceDetails = 128
	// maxTaskExecutionTraceTextBytes 限制处理量快照中单个文本字段的 UTF-8 字节数。
	maxTaskExecutionTraceTextBytes = 256
)

// taskRuntimeReadFields 是任务列表和日报读取运行快照所需的最小字段集合。
var taskRuntimeReadFields = []string{"startedAt", "durationMs", "executionTrace"}

// taskRuntimeFinalFields 是任务再次开始或完成时必须先清除的上一轮可选终态字段。
var taskRuntimeFinalFields = []string{
	"finishedAt",
	"finishedAtMs",
	"durationMs",
	"executionTrace",
	"lastErr",
}

// taskRuntimeRecord 表示任务运行时耗时快照。
type taskRuntimeRecord struct {
	StartedAt      string              // 开始执行时间
	DurationMS     int64               // 执行耗时，毫秒
	ExecutionTrace *taskstats.Snapshot // 本次任务处理量统计摘要
}

// taskRuntimeKey 返回按逻辑队列和任务 ID 隔离的运行耗时快照 Redis key。
func (m *Manager) taskRuntimeKey(queue string, taskID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskRuntimeKey(m.displayQueueName(queue), taskID)
}

// taskRuntimeIdentity 返回进程内批量结果使用的队列级任务标识。
func taskRuntimeIdentity(queue string, taskID string) string {
	return strings.TrimSpace(queue) + "\x00" + strings.TrimSpace(taskID)
}

// taskRuntimeNeedsWorkflowRetention 判断当前任务运行快照是否需要对齐工作流保留窗口。
func taskRuntimeNeedsWorkflowRetention(ctx context.Context) bool {
	meta := requestctx.FromContext(ctx)
	return meta != nil && strings.TrimSpace(meta.WorkflowID) != ""
}

// taskRuntimeActiveRetention 返回任务执行中快照保留时间。
// active 阶段尚不知道最终状态，先给足排查窗口；完成后会按最终状态收敛 TTL。
func (m *Manager) taskRuntimeActiveRetention() time.Duration {
	retention := taskCompletedRetention
	if archivedRetention := m.ArchivedRetention(); archivedRetention > retention {
		retention = archivedRetention
	}
	return retention + time.Hour
}

// taskRuntimeFinalRetention 返回任务完成后运行耗时快照保留时间。
func (m *Manager) taskRuntimeFinalRetention(ctx context.Context, runErr error) time.Duration {
	if taskWillArchive(ctx, runErr) {
		retention := m.ArchivedRetention()
		if taskRuntimeNeedsWorkflowRetention(ctx) && taskCompletedRetention > retention {
			retention = taskCompletedRetention
		}
		return retention + time.Hour
	}
	return taskCompletedRetention + time.Hour
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
func (m *Manager) recordTaskRuntimeStart(ctx context.Context, queue string, taskID string, begin time.Time) string {
	taskID = strings.TrimSpace(taskID)
	if m == nil || m.redis == nil || taskID == "" {
		return ""
	}
	attemptToken := newID()
	nowText := begin.Format(time.RFC3339)
	values := map[string]any{
		"attemptToken": attemptToken,
		"status":       "running",
		"startedAt":    nowText,
		"startedAtMs":  begin.UnixMilli(),
		"updatedAt":    nowText,
	}
	if err := m.writeTaskRuntime(ctx, queue, taskID, values, m.taskRuntimeActiveRetention()); err != nil {
		loggerx.Errorw(ctx, "任务耗时记录开始失败", err, logx.Field("task_id", taskID))
		return ""
	}
	return attemptToken
}

// startTaskRuntimeHeartbeat 启动任务运行指标心跳，并返回可重复调用的停止函数。
// 每个执行中任务最多创建一个心跳协程，其数量受 Worker 并发上限约束。
func (m *Manager) startTaskRuntimeHeartbeat(
	ctx context.Context,
	queue string,
	taskID string,
	attemptToken string,
	begin time.Time,
	tracker *taskstats.Tracker,
	workflowMeta WorkflowTaskMeta,
) func() {
	taskID = strings.TrimSpace(taskID)
	attemptToken = strings.TrimSpace(attemptToken)
	if m == nil || m.redis == nil || taskID == "" || attemptToken == "" || tracker == nil {
		return func() {}
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(taskRuntimeHeartbeatInterval)
		defer ticker.Stop()
		var logErrorOnce sync.Once
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case now := <-ticker.C:
				snapshot := tracker.Snapshot()
				statsSnapshot := boundedTaskStatsSnapshot(&snapshot)
				writeCtx, writeCancel := context.WithTimeout(heartbeatCtx, taskRuntimeHeartbeatWriteTimeout)
				written, err := m.recordTaskRuntimeHeartbeat(
					writeCtx,
					queue,
					taskID,
					attemptToken,
					begin,
					now,
					statsSnapshot,
				)
				if err == nil && written {
					err = m.recordWorkflowTaskStats(writeCtx, workflowMeta, attemptToken, statsSnapshot)
				}
				writeCancel()
				if err != nil {
					logErrorOnce.Do(func() {
						loggerx.Errorw(ctx, "任务运行指标心跳写入失败", err, logx.Field("task_id", taskID))
					})
					continue
				}
				if !written {
					return
				}
			}
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			cancel()
			<-done
		})
	}
}

// recordTaskRuntimeHeartbeat 原子更新当前 attempt 的执行中耗时和处理量快照。
func (m *Manager) recordTaskRuntimeHeartbeat(
	ctx context.Context,
	queue string,
	taskID string,
	attemptToken string,
	begin time.Time,
	now time.Time,
	statsSnapshot *taskstats.Snapshot,
) (bool, error) {
	durationMS := now.Sub(begin).Milliseconds()
	if durationMS <= 0 {
		durationMS = 1
	}
	values := map[string]any{
		"status":     "running",
		"durationMs": durationMS,
		"updatedAt":  now.Format(time.RFC3339),
	}
	addTaskRuntimeStatsValues(values, statsSnapshot)
	return m.writeTaskRuntimeAttempt(
		ctx,
		queue,
		taskID,
		attemptToken,
		nil,
		values,
		m.taskRuntimeActiveRetention(),
	)
}

// recordTaskRuntimeFinish 记录任务完成耗时，成功和失败都会保留。
func (m *Manager) recordTaskRuntimeFinish(ctx context.Context, queue string, taskID string, attemptToken string, begin time.Time, runErr error, statsSnapshot *taskstats.Snapshot) {
	taskID = strings.TrimSpace(taskID)
	attemptToken = strings.TrimSpace(attemptToken)
	if m == nil || m.redis == nil || taskID == "" || attemptToken == "" {
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
	statsSnapshot = boundedTaskStatsSnapshot(statsSnapshot)
	addTaskRuntimeStatsValues(values, statsSnapshot)
	if runErr != nil {
		values["lastErr"] = runErr.Error()
	}
	written, err := m.finishTaskRuntime(writeCtx, queue, taskID, attemptToken, values, m.taskRuntimeFinalRetention(ctx, runErr))
	if err != nil {
		loggerx.Errorw(writeCtx, "任务耗时记录完成失败", err, logx.Field("task_id", taskID))
	} else if !written {
		loggerx.Infow(writeCtx, "任务耗时忽略过期实例回写", logx.Field("task_id", taskID), logx.Field("queue", m.displayQueueName(queue)))
	}
}

// addTaskRuntimeStatsValues 把处理量快照转换为任务运行 hash 字段。
func addTaskRuntimeStatsValues(values map[string]any, statsSnapshot *taskstats.Snapshot) {
	if len(values) == 0 || statsSnapshot == nil || statsSnapshot.Empty() {
		return
	}
	rawStats, err := json.Marshal(statsSnapshot)
	if err != nil {
		return
	}
	values["executionTrace"] = string(rawStats)
}

// writeTaskRuntime 在单 key 事务内清理上轮终态字段、写入本轮快照并设置 TTL。
func (m *Manager) writeTaskRuntime(ctx context.Context, queue string, taskID string, values map[string]any, retention time.Duration) error {
	key := m.taskRuntimeKey(queue, taskID)
	if key == "" {
		return errors.Errorf("任务运行快照 key 不能为空")
	}
	if retention <= 0 {
		return errors.Errorf("任务运行快照保留时间必须大于 0")
	}
	_, err := m.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HDel(ctx, key, taskRuntimeFinalFields...)
		pipe.HSet(ctx, key, values)
		pipe.PExpire(ctx, key, retention)
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "原子写入任务运行快照失败")
	}
	return nil
}

// finishTaskRuntime 仅在 attempt token 仍匹配时原子写入终态，迟到实例返回 false。
func (m *Manager) finishTaskRuntime(ctx context.Context, queue string, taskID string, attemptToken string, values map[string]any, retention time.Duration) (bool, error) {
	return m.writeTaskRuntimeAttempt(ctx, queue, taskID, attemptToken, taskRuntimeFinalFields, values, retention)
}

// writeTaskRuntimeAttempt 仅在 attempt token 匹配时原子清理指定字段、写入快照并续期。
func (m *Manager) writeTaskRuntimeAttempt(
	ctx context.Context,
	queue string,
	taskID string,
	attemptToken string,
	clearFields []string,
	values map[string]any,
	retention time.Duration,
) (bool, error) {
	key := m.taskRuntimeKey(queue, taskID)
	if key == "" || strings.TrimSpace(attemptToken) == "" {
		return false, errors.Errorf("任务运行快照 key 和 attempt token 不能为空")
	}
	if retention <= 0 {
		return false, errors.Errorf("任务运行快照保留时间必须大于 0")
	}
	args := make([]any, 0, 3+len(clearFields)+len(values)*2)
	args = append(args, attemptToken, retention.Milliseconds(), len(clearFields))
	for _, field := range clearFields {
		args = append(args, field)
	}
	for field, value := range values {
		args = append(args, field, value)
	}
	result, err := writeTaskRuntimeAttemptScript.Run(ctx, m.redis, []string{key}, args...).Int()
	if err != nil {
		return false, errors.Wrap(err, "原子写入任务运行快照失败")
	}
	if result != 0 && result != 1 {
		return false, errors.Errorf("任务运行快照脚本返回非法结果: %d", result)
	}
	return result == 1, nil
}

// readTaskRuntime 读取任务运行耗时快照。
func (m *Manager) readTaskRuntime(ctx context.Context, queue string, taskID string) taskRuntimeRecord {
	taskID = strings.TrimSpace(taskID)
	if m == nil || m.redis == nil || taskID == "" {
		return taskRuntimeRecord{}
	}
	values, err := m.redis.HMGet(ctx, m.taskRuntimeKey(queue, taskID), taskRuntimeReadFields...).Result()
	if err != nil {
		return taskRuntimeRecord{}
	}
	record, _ := taskRuntimeRecordFromFieldValues(values)
	return record
}

// readTaskRuntimeBatch 批量读取任务列表所需的运行快照，避免页面轮询逐行访问 Redis。
func (m *Manager) readTaskRuntimeBatch(ctx context.Context, infos []*asynq.TaskInfo) map[string]taskRuntimeRecord {
	result := make(map[string]taskRuntimeRecord, len(infos))
	if m == nil || m.redis == nil || len(infos) == 0 {
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}
	commands := make(map[string]*redis.SliceCmd, len(infos))
	pipe := m.redis.Pipeline()
	for _, info := range infos {
		if info == nil || strings.TrimSpace(info.ID) == "" {
			continue
		}
		identity := taskRuntimeIdentity(info.Queue, info.ID)
		if _, exists := commands[identity]; exists {
			continue
		}
		key := m.taskRuntimeKey(info.Queue, info.ID)
		if key == "" {
			continue
		}
		commands[identity] = pipe.HMGet(ctx, key, taskRuntimeReadFields...)
	}
	if len(commands) == 0 {
		pipe.Discard()
		return result
	}
	// 单个 key 读取失败沿用原单条查询的空快照降级，其他 key 仍可正常回显。
	_, _ = pipe.Exec(ctx)
	pipe.Discard()
	for identity, command := range commands {
		values, err := command.Result()
		if err != nil {
			continue
		}
		if record, ok := taskRuntimeRecordFromFieldValues(values); ok {
			result[identity] = record
		}
	}
	return result
}

// taskRuntimeRecordFromFieldValues 把固定顺序的 Redis 字段值还原为运行快照。
func taskRuntimeRecordFromFieldValues(values []any) (taskRuntimeRecord, bool) {
	if len(values) != len(taskRuntimeReadFields) {
		return taskRuntimeRecord{}, false
	}
	hasValue := false
	for _, value := range values {
		if value != nil {
			hasValue = true
			break
		}
	}
	if !hasValue {
		return taskRuntimeRecord{}, false
	}
	return taskRuntimeRecord{
		StartedAt:      strings.TrimSpace(anyToString(values[0])),
		DurationMS:     toInt64(values[1]),
		ExecutionTrace: taskExecutionStatsFromJSON([]byte(strings.TrimSpace(anyToString(values[2])))),
	}, true
}

// readTaskRuntimes 批量读取日报必需的运行快照字段，并在取值前执行字节预算预检。
func (m *Manager) readTaskRuntimes(ctx context.Context, infos []*asynq.TaskInfo, byteBudget int64) (map[string]taskRuntimeRecord, int64, bool, error) {
	result := make(map[string]taskRuntimeRecord, len(infos))
	if m == nil || m.redis == nil || len(infos) == 0 {
		return result, 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if byteBudget <= 0 {
		return result, 0, true, nil
	}
	type runtimeSizeCommands struct {
		identity string          // identity 是队列级任务标识
		key      string          // key 是任务运行快照 Redis key
		fields   []*redis.IntCmd // fields 是各日报字段的 HSTRLEN 命令
	}
	sizeCommands := make([]runtimeSizeCommands, 0, len(infos))
	pipe := m.redis.Pipeline()
	for _, info := range infos {
		if info == nil || strings.TrimSpace(info.ID) == "" {
			continue
		}
		identity := taskRuntimeIdentity(info.Queue, info.ID)
		key := m.taskRuntimeKey(info.Queue, info.ID)
		if key == "" {
			continue
		}
		commands := runtimeSizeCommands{identity: identity, key: key, fields: make([]*redis.IntCmd, 0, len(taskRuntimeReadFields))}
		for _, field := range taskRuntimeReadFields {
			commands.fields = append(commands.fields, pipe.HStrLen(ctx, key, field))
		}
		sizeCommands = append(sizeCommands, commands)
	}
	_, execErr := pipe.Exec(ctx)
	pipe.Discard()
	if execErr != nil {
		return nil, 0, false, errors.Wrap(execErr, "预检日报任务运行快照大小失败")
	}
	totalBytes := int64(0)
	for _, commands := range sizeCommands {
		entryBytes := int64(taskRuntimeReportEntryOverheadBytes)
		for _, command := range commands.fields {
			fieldBytes, err := command.Result()
			if err != nil {
				return nil, 0, false, errors.Wrapf(err, "读取日报任务运行快照大小失败 task=%s", commands.identity)
			}
			if fieldBytes < 0 || entryBytes > math.MaxInt64-fieldBytes {
				return nil, 0, false, errors.Errorf("日报任务运行快照大小非法 task=%s", commands.identity)
			}
			entryBytes += fieldBytes
		}
		if totalBytes > math.MaxInt64-entryBytes {
			return nil, 0, false, errors.Errorf("日报任务运行快照总大小溢出")
		}
		totalBytes += entryBytes
		if totalBytes > byteBudget {
			return result, 0, true, nil
		}
	}
	pipe = m.redis.Pipeline()
	valueCommands := make(map[string]*redis.SliceCmd, len(sizeCommands))
	for _, commands := range sizeCommands {
		valueCommands[commands.identity] = pipe.HMGet(ctx, commands.key, taskRuntimeReadFields...)
	}
	_, execErr = pipe.Exec(ctx)
	pipe.Discard()
	if execErr != nil {
		return nil, 0, false, errors.Wrap(execErr, "批量读取日报任务运行快照失败")
	}
	for identity, command := range valueCommands {
		values, err := command.Result()
		if err != nil {
			return nil, 0, false, errors.Wrapf(err, "读取日报任务运行快照失败 task=%s", identity)
		}
		if len(values) != len(taskRuntimeReadFields) {
			return nil, 0, false, errors.Errorf("日报任务运行快照字段数量异常 task=%s got=%d want=%d", identity, len(values), len(taskRuntimeReadFields))
		}
		if record, ok := taskRuntimeRecordFromFieldValues(values); ok {
			result[identity] = record
		}
	}
	return result, totalBytes, false, nil
}

// boundedTaskStatsSnapshot 保留聚合计数并限制持久化明细大小，避免任务结果放大日报内存。
func boundedTaskStatsSnapshot(snapshot *taskstats.Snapshot) *taskstats.Snapshot {
	if snapshot == nil || snapshot.Empty() {
		return nil
	}
	bounded := *snapshot
	bounded.Name = truncateUTF8Bytes(bounded.Name, maxTaskExecutionTraceTextBytes)
	bounded.StartedAt = truncateUTF8Bytes(bounded.StartedAt, maxTaskExecutionTraceTextBytes)
	bounded.FinishedAt = truncateUTF8Bytes(bounded.FinishedAt, maxTaskExecutionTraceTextBytes)
	detailCount := min(len(snapshot.Details), maxTaskExecutionTraceDetails)
	bounded.Details = make([]taskstats.Detail, 0, detailCount)
	for index := 0; index < detailCount; index++ {
		detail := snapshot.Details[index]
		detail.Action = truncateUTF8Bytes(detail.Action, maxTaskExecutionTraceTextBytes)
		detail.Name = truncateUTF8Bytes(detail.Name, maxTaskExecutionTraceTextBytes)
		bounded.Details = append(bounded.Details, detail)
	}
	for {
		raw, err := json.Marshal(&bounded)
		if err == nil && len(raw) <= maxTaskExecutionTraceBytes {
			return &bounded
		}
		if len(bounded.Details) == 0 {
			bounded.Name = ""
			return &bounded
		}
		bounded.Details = bounded.Details[:len(bounded.Details)/2]
	}
}

// truncateUTF8Bytes 在不切断 UTF-8 编码的前提下限制字符串字节数。
func truncateUTF8Bytes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit]
}

// taskDurationFromResult 从成功任务结果中提取耗时。
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

// taskExecutionStatsFromJSON 从 JSON 字节中读取任务处理量快照，空值或无效 JSON 返回 nil。
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
		ExecutionTrace *taskstats.Snapshot `json:"executionTrace"` // ExecutionTrace 表示任务结果中的处理量统计摘要。
	}
	if err := json.Unmarshal(result, &payload); err != nil || payload.ExecutionTrace == nil || payload.ExecutionTrace.Empty() {
		return nil
	}
	return payload.ExecutionTrace
}
