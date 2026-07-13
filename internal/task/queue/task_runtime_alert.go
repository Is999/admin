package taskqueue

import (
	"context"
	"strings"
	"time"

	"admin/internal/infra/loggerx"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	taskRuntimeAlertKindPeriodicSyncFailed     = "periodic_sync_failed"     // 周期任务配置同步失败
	taskRuntimeAlertKindPeriodicScheduleFailed = "periodic_schedule_failed" // 周期任务注册到 cron 调度器失败
	taskRuntimeAlertKindPeriodicLeaderFailed   = "periodic_leader_failed"   // 周期任务投递前 leader 校验失败
	taskRuntimeAlertKindPeriodicQueueFailed    = "periodic_queue_failed"    // 周期任务投递前队列巡检失败
	taskRuntimeAlertKindPeriodicEnqueueFailed  = "periodic_enqueue_failed"  // 周期任务投递到 Asynq 失败
	taskRuntimeAlertKindArchiveCleanupFailed   = "archive_cleanup_failed"   // 归档失败任务过期清理失败
)

// NotifyRuntimeAlert 接收业务模块上报的任务系统运行异常，并复用统一限频和通知链路。
func (m *Manager) NotifyRuntimeAlert(ctx context.Context, alert svc.TaskRuntimeAlert) {
	if m == nil {
		return
	}
	m.notifyTaskRuntimeAlert(ctx, TaskRuntimeAlert{
		Kind:         alert.Kind,
		Title:        alert.Title,
		Status:       alert.Status,
		Component:    alert.Component,
		Operation:    alert.Operation,
		TaskName:     alert.TaskName,
		TaskType:     alert.TaskType,
		WorkflowName: alert.WorkflowName,
		Cron:         alert.Cron,
		TaskQueue:    alert.TaskQueue,
		UniqueKey:    alert.UniqueKey,
		Reason:       alert.Reason,
		Advice:       alert.Advice,
		OccurredAt:   alert.OccurredAt,
	})
}

// notifyTaskRuntimeAlert 按异常指纹限频触发任务系统运行异常告警。
func (m *Manager) notifyTaskRuntimeAlert(ctx context.Context, alert TaskRuntimeAlert) {
	if m == nil {
		return
	}
	alert = normalizeTaskRuntimeAlert(alert)
	if alert.Kind == "" && alert.Operation == "" && alert.Reason == "" {
		return
	}
	key := taskRuntimeAlertKey(alert)
	now := time.Now()

	m.mu.Lock()
	if len(m.runtimeAlertHooks) == 0 {
		m.mu.Unlock()
		return
	}
	if m.runtimeAlertedMap == nil {
		m.runtimeAlertedMap = make(map[string]taskRuntimeAlertState)
	}
	state := m.runtimeAlertedMap[key]
	if !state.LastSentAt.IsZero() && now.Sub(state.LastSentAt) < taskRuntimeAlertTTL {
		state.SuppressedCount++
		m.runtimeAlertedMap[key] = state
		m.mu.Unlock()
		return
	}
	alert.TriggerCount = state.SuppressedCount + 1
	m.runtimeAlertedMap[key] = taskRuntimeAlertState{LastSentAt: now}
	hooks := append([]TaskRuntimeAlertHook(nil), m.runtimeAlertHooks...)
	m.mu.Unlock()

	if len(hooks) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fields := taskRuntimeAlertLogFields(alert)
	hookFailed := false
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hookCtx, cancel := m.taskFinalWriteContext(ctx)
		if err := hook(hookCtx, alert); err != nil {
			hookFailed = true
			loggerx.Errorw(hookCtx, "任务运行异常告警钩子执行失败", err, fields...)
		}
		cancel()
	}
	if hookFailed {
		m.mu.Lock()
		current := m.runtimeAlertedMap[key]
		if current.LastSentAt.Equal(now) {
			state.SuppressedCount += current.SuppressedCount
			if state.LastSentAt.IsZero() && state.SuppressedCount == 0 {
				delete(m.runtimeAlertedMap, key)
			} else {
				m.runtimeAlertedMap[key] = state
			}
		}
		m.mu.Unlock()
	}
}

// normalizeTaskRuntimeAlert 清理告警字段，保证指纹和通知文本稳定。
func normalizeTaskRuntimeAlert(alert TaskRuntimeAlert) TaskRuntimeAlert {
	alert.Kind = strings.TrimSpace(alert.Kind)
	alert.Title = strings.TrimSpace(alert.Title)
	alert.Status = strings.TrimSpace(alert.Status)
	alert.Component = strings.TrimSpace(alert.Component)
	alert.Operation = strings.TrimSpace(alert.Operation)
	alert.TaskName = strings.TrimSpace(alert.TaskName)
	alert.TaskType = strings.TrimSpace(alert.TaskType)
	alert.WorkflowName = strings.TrimSpace(alert.WorkflowName)
	alert.Cron = strings.TrimSpace(alert.Cron)
	alert.TaskQueue = strings.TrimSpace(alert.TaskQueue)
	alert.UniqueKey = strings.TrimSpace(alert.UniqueKey)
	alert.Reason = strings.TrimSpace(alert.Reason)
	alert.Advice = strings.TrimSpace(alert.Advice)
	if alert.OccurredAt.IsZero() {
		alert.OccurredAt = time.Now()
	}
	return alert
}

// taskRuntimeAlertKey 生成运行异常限频指纹，错误文本不入指纹以降低动态错误刷屏风险。
func taskRuntimeAlertKey(alert TaskRuntimeAlert) string {
	alert = normalizeTaskRuntimeAlert(alert)
	return strings.Join([]string{
		alert.Kind,
		alert.Component,
		alert.Operation,
		alert.TaskName,
		alert.TaskType,
		alert.WorkflowName,
		alert.Cron,
		alert.TaskQueue,
		alert.UniqueKey,
	}, "\x00")
}

// taskRuntimeAlertLogFields 返回告警钩子失败时的排障字段。
func taskRuntimeAlertLogFields(alert TaskRuntimeAlert) []logx.LogField {
	alert = normalizeTaskRuntimeAlert(alert)
	fields := make([]logx.LogField, 0, 8)
	appendField := func(key string, value string) {
		if value != "" {
			fields = append(fields, logx.Field(key, value))
		}
	}
	appendField("alert_kind", alert.Kind)
	appendField("component", alert.Component)
	appendField("operation", alert.Operation)
	appendField("task_name", alert.TaskName)
	appendField("task_type", alert.TaskType)
	appendField("workflow", alert.WorkflowName)
	appendField("queue", alert.TaskQueue)
	appendField("unique_key", alert.UniqueKey)
	return fields
}
