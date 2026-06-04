package taskqueue

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"

	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// taskRuntimeRecord 表示任务运行时耗时快照。
type taskRuntimeRecord struct {
	StartedAt      string              // 开始执行时间
	FinishedAt     string              // 完成执行时间
	DurationMS     int64               // 执行耗时，毫秒
	ExecutionTrace *taskstats.Snapshot // 本次任务处理量统计摘要
}

// taskRuntimeKey 返回任务运行耗时快照 Redis key。
func (m *Manager) taskRuntimeKey(taskID string) string {
	if !m.useRedisNamespace() {
		return ""
	}
	return keys.TaskRuntimeKey(taskID)
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
