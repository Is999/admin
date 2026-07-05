package taskalert

import (
	"context"
	"strconv"
	"strings"
	"time"

	"admin/helper"
	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/infra/larkx"
	"admin/internal/requestctx"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const collectorTaskQueue = "collector" // collectorTaskQueue 是 Collector 告警归属的虚拟任务队列。

// RegisterLark 注册任务系统 Lark 告警钩子。
func RegisterLark(cfg config.Config, manager *taskqueue.Manager) error {
	if manager == nil {
		return nil
	}
	notifier, err := larkx.New(cfg.Alert.Lark)
	if err != nil {
		return errors.Wrap(err, "初始化 Lark 告警失败")
	}
	if notifier == nil {
		return nil
	}
	if err := manager.RegisterFinalFailureHook(func(ctx context.Context, task *asynq.Task, meta taskqueue.WorkflowTaskMeta, runErr error) error {
		if err := notifier.SendTaskFailure(alertSendContext(ctx), buildTaskFailureAlert(cfg, ctx, task, meta, runErr)); err != nil {
			return errors.Wrap(err, "发送任务失败 Lark 告警失败")
		}
		return nil
	}); err != nil {
		return errors.Tag(err)
	}
	if err := manager.RegisterPeriodicConfigInvalidHook(func(ctx context.Context, report taskqueue.PeriodicConfigInvalidReport) error {
		if err := notifier.SendPeriodicConfigInvalid(alertSendContext(ctx), buildPeriodicConfigAlert(cfg, report)); err != nil {
			return errors.Wrap(err, "发送周期任务配置异常 Lark 告警失败")
		}
		return nil
	}); err != nil {
		return errors.Tag(err)
	}
	return manager.RegisterRuntimeAlertHook(func(ctx context.Context, alert taskqueue.TaskRuntimeAlert) error {
		if err := notifier.SendTaskRuntimeAlert(alertSendContext(ctx), buildTaskRuntimeAlert(cfg, alert)); err != nil {
			return errors.Wrap(err, "发送任务运行异常 Lark 告警失败")
		}
		return nil
	})
}

// CollectorRuntimeAlert 将 Collector 内部告警转换为任务系统统一运行异常。
func CollectorRuntimeAlert(alert collectorx.RuntimeAlert) svc.TaskRuntimeAlert {
	reason := strings.TrimSpace(alert.Reason)
	if alert.Count > 0 {
		countText := "影响数量=" + strconv.Itoa(alert.Count)
		if reason != "" {
			reason += "；" + countText
		} else {
			reason = countText
		}
	}
	kind := strings.TrimSpace(alert.Kind)
	switch kind {
	case collectorx.RuntimeAlertKindEnqueueFailed:
		kind = svc.TaskRuntimeAlertKindCollectorEnqueueFailed
	case collectorx.RuntimeAlertKindInvalidEvent:
		kind = svc.TaskRuntimeAlertKindCollectorInvalidEvent
	case collectorx.RuntimeAlertKindDeadEvent:
		kind = svc.TaskRuntimeAlertKindCollectorDeadEvent
	default:
		kind = svc.TaskRuntimeAlertKindCollectorWorkerFailed
	}
	taskType := ""
	if channel := strings.TrimSpace(alert.Channel); channel != "" {
		taskType = "collector:" + channel
	}
	uniqueKey := collectorAlertUniqueKey(kind, alert)
	return svc.TaskRuntimeAlert{
		Kind:       kind,
		Title:      strings.TrimSpace(alert.Title),
		Status:     strings.TrimSpace(alert.Status),
		Component:  helper.FirstNonEmptyString(strings.TrimSpace(alert.Component), collectorTaskQueue),
		Operation:  strings.TrimSpace(alert.Operation),
		TaskName:   strings.TrimSpace(alert.BizType),
		TaskType:   taskType,
		TaskQueue:  collectorTaskQueue,
		UniqueKey:  uniqueKey,
		Reason:     reason,
		Advice:     strings.TrimSpace(alert.Advice),
		OccurredAt: alert.OccurredAt,
	}
}

// collectorAlertUniqueKey 生成低基数告警指纹，避免事件 ID 或消息 ID 导致 Lark 刷屏。
func collectorAlertUniqueKey(kind string, alert collectorx.RuntimeAlert) string {
	bizType := strings.TrimSpace(alert.BizType)
	if bizType != "" {
		return bizType
	}
	operation := strings.TrimSpace(alert.Operation)
	channel := strings.TrimSpace(alert.Channel)
	if operation != "" && channel != "" {
		return operation + ":" + channel
	}
	if operation != "" {
		return operation
	}
	if channel != "" {
		return channel
	}
	return strings.TrimSpace(kind)
}

// alertSendContext 让外部告警使用自身 HTTP 超时，不继承任务收尾写入 deadline。
func alertSendContext(parent context.Context) context.Context {
	ctx := context.Background()
	if meta := requestctx.FromContext(parent); meta != nil {
		metaCopy := *meta
		ctx = requestctx.WithMeta(ctx, &metaCopy)
	}
	return ctx
}

// buildTaskFailureAlert 从任务上下文和工作流元数据中提取 Lark 告警字段。
func buildTaskFailureAlert(cfg config.Config, ctx context.Context, task *asynq.Task, workflowMeta taskqueue.WorkflowTaskMeta, runErr error) larkx.TaskFailureAlert {
	meta := requestctx.FromContext(ctx)
	alert := larkx.TaskFailureAlert{
		ServiceName:  strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:  strings.TrimSpace(cfg.Mode),
		AppID:        strings.TrimSpace(cfg.AppID),
		TaskType:     taskTypeOf(task),
		TaskName:     taskqueue.TaskDisplayName(task),
		TaskSource:   taskqueue.TaskSource(task),
		WorkflowID:   strings.TrimSpace(workflowMeta.WorkflowID),
		WorkflowName: strings.TrimSpace(workflowMeta.WorkflowName),
		WorkflowNode: strings.TrimSpace(workflowMeta.WorkflowNode),
		ShardIndex:   workflowMeta.ShardIndex,
		ShardTotal:   workflowMeta.ShardTotal,
		OccurredAt:   time.Now(),
		Err:          runErr,
	}
	if alert.ServiceName == "" {
		alert.ServiceName = strings.TrimSpace(cfg.Name)
	}
	if meta != nil {
		alert.TaskID = strings.TrimSpace(meta.TaskID)
		alert.TaskQueue = strings.TrimSpace(meta.TaskQueue)
		alert.Mode = strings.TrimSpace(meta.Mode)
		alert.TraceID = strings.TrimSpace(meta.TraceID)
		if alert.TaskName == "" {
			alert.TaskName = strings.TrimSpace(meta.TaskName)
		}
		if alert.WorkflowID == "" {
			alert.WorkflowID = strings.TrimSpace(meta.WorkflowID)
		}
		if alert.WorkflowName == "" {
			alert.WorkflowName = strings.TrimSpace(meta.WorkflowName)
		}
		if alert.WorkflowNode == "" {
			alert.WorkflowNode = strings.TrimSpace(meta.WorkflowNode)
		}
		if alert.ShardTotal == 0 {
			alert.ShardIndex = meta.ShardIndex
			alert.ShardTotal = meta.ShardTotal
		}
	}
	return alert
}

// buildPeriodicConfigAlert 从周期任务配置异常中提取 Lark 告警字段。
func buildPeriodicConfigAlert(cfg config.Config, report taskqueue.PeriodicConfigInvalidReport) larkx.PeriodicConfigAlert {
	item := report.Task
	alert := larkx.PeriodicConfigAlert{
		ServiceName:  strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:  strings.TrimSpace(cfg.Mode),
		AppID:        strings.TrimSpace(cfg.AppID),
		TaskIndex:    report.Index,
		TaskName:     strings.TrimSpace(item.Name),
		WorkflowName: strings.TrimSpace(item.Workflow),
		Cron:         strings.TrimSpace(item.Cron),
		EverySeconds: item.EverySeconds,
		TaskQueue:    strings.TrimSpace(item.Queue),
		UniqueKey:    strings.TrimSpace(item.UniqueKey),
		OccurredAt:   report.OccurredAt,
		Reason:       strings.TrimSpace(report.Reason),
		TriggerCount: report.TriggerCount,
	}
	if alert.ServiceName == "" {
		alert.ServiceName = strings.TrimSpace(cfg.Name)
	}
	return alert
}

// buildTaskRuntimeAlert 从任务运行异常中提取 Lark 告警字段。
func buildTaskRuntimeAlert(cfg config.Config, report taskqueue.TaskRuntimeAlert) larkx.TaskRuntimeAlert {
	alert := larkx.TaskRuntimeAlert{
		ServiceName:  strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:  strings.TrimSpace(cfg.Mode),
		AppID:        strings.TrimSpace(cfg.AppID),
		Kind:         strings.TrimSpace(report.Kind),
		Title:        strings.TrimSpace(report.Title),
		Status:       strings.TrimSpace(report.Status),
		Component:    strings.TrimSpace(report.Component),
		Operation:    strings.TrimSpace(report.Operation),
		TaskName:     strings.TrimSpace(report.TaskName),
		TaskType:     strings.TrimSpace(report.TaskType),
		WorkflowName: strings.TrimSpace(report.WorkflowName),
		Cron:         strings.TrimSpace(report.Cron),
		TaskQueue:    strings.TrimSpace(report.TaskQueue),
		UniqueKey:    strings.TrimSpace(report.UniqueKey),
		OccurredAt:   report.OccurredAt,
		Reason:       strings.TrimSpace(report.Reason),
		Advice:       strings.TrimSpace(report.Advice),
		TriggerCount: report.TriggerCount,
	}
	if alert.ServiceName == "" {
		alert.ServiceName = strings.TrimSpace(cfg.Name)
	}
	return alert
}

// taskTypeOf 安全读取 Asynq 任务类型，兼容空任务指针。
func taskTypeOf(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Type())
}
