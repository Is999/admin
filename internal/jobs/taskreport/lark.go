package taskreport

import (
	"context"
	"strings"

	"admin/internal/config"
	"admin/internal/infra/larkx"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// SendLarkReport 将任务运行日报发送到 Lark；未启用通知时静默跳过。
func SendLarkReport(ctx context.Context, svcCtx *svc.ServiceContext, report Report) error {
	if svcCtx == nil {
		return nil
	}
	cfg := svcCtx.CurrentConfig()
	notifier, err := larkx.New(cfg.Alert.Lark)
	if err != nil {
		RecordLarkError(ctx)
		return errors.Wrap(err, "初始化 Lark 日报通知失败")
	}
	if notifier == nil {
		logx.WithContext(ctx).Info("Lark 日报通知未启用，跳过任务运行日报发送")
		return nil
	}
	// 发送外部通知使用独立背景上下文，避免任务收尾 deadline 截断 HTTP 请求。
	if err := notifier.SendTaskDailyReport(context.Background(), ToLarkReport(cfg, report)); err != nil {
		RecordLarkError(ctx)
		return errors.Wrap(err, "发送任务运行日报 Lark 通知失败")
	}
	return nil
}

// ToLarkReport 将任务运行日报领域对象转换为 Lark 卡片契约。
func ToLarkReport(cfg config.Config, report Report) larkx.TaskDailyReport {
	result := larkx.TaskDailyReport{
		ServiceName:           strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:           strings.TrimSpace(cfg.Mode),
		AppID:                 strings.TrimSpace(cfg.AppID),
		WindowStart:           report.WindowStart,
		WindowEnd:             report.WindowEnd,
		GeneratedAt:           report.GeneratedAt,
		TotalTaskExecutions:   report.TotalTaskExecutions,
		SuccessTaskExecutions: report.SuccessTaskExecutions,
		FailedTaskExecutions:  report.FailedTaskExecutions,
		PeriodicTriggerTotal:  report.PeriodicTriggerTotal,
		PeriodicTriggerOK:     report.PeriodicTriggerOK,
		PeriodicTriggerFailed: report.PeriodicTriggerFailed,
		NodeTaskTotal:         report.NodeTaskTotal,
		WorkflowTotal:         report.WorkflowTotal,
		WorkflowSuccess:       report.WorkflowSuccess,
		WorkflowFailed:        report.WorkflowFailed,
		WorkflowRunning:       report.WorkflowRunning,
		WorkflowUnknown:       report.WorkflowUnknown,
		TraceTotalCount:       report.TraceTotalCount,
		TraceReadCount:        report.TraceReadCount,
		TraceWriteCount:       report.TraceWriteCount,
		TraceDeleteCount:      report.TraceDeleteCount,
		TraceErrorCount:       report.TraceErrorCount,
		AverageDurationMS:     report.AverageDurationMS,
		MaxDurationMS:         report.MaxDurationMS,
		Truncated:             report.Truncated,
		RetentionWarning:      strings.TrimSpace(report.RetentionWarning),
	}
	if result.ServiceName == "" {
		result.ServiceName = strings.TrimSpace(cfg.Name)
	}
	result.Queues = toLarkQueues(report.QueueSummaries)
	result.PeriodicTasks = toLarkPeriodicItems(report.PeriodicTasks)
	result.Workflows = toLarkWorkflowItems(report.Workflows)
	result.FailureTasks = toLarkTasks(report.FailureTasks)
	result.SlowTasks = toLarkTasks(report.SlowTasks)
	return result
}

// toLarkQueues 转换队列摘要为 Lark 日报队列字段。
func toLarkQueues(items []QueueSummary) []larkx.TaskDailyReportQueue {
	result := make([]larkx.TaskDailyReportQueue, 0, len(items))
	for _, item := range items {
		result = append(result, larkx.TaskDailyReportQueue{
			Name:           item.Name,
			TaskExecutions: item.TaskExecutions,
			Success:        item.Success,
			Failed:         item.Failed,
			Triggers:       item.Triggers,
			NodeTasks:      item.NodeTasks,
			Pending:        item.Pending,
			Active:         item.Active,
			Scheduled:      item.Scheduled,
			Retry:          item.Retry,
			Archived:       item.Archived,
		})
	}
	return result
}

// toLarkPeriodicItems 转换周期任务摘要为 Lark 日报 Top 项。
func toLarkPeriodicItems(items []PeriodicSummary) []larkx.TaskDailyReportItem {
	result := make([]larkx.TaskDailyReportItem, 0, len(items))
	for _, item := range items {
		result = append(result, larkx.TaskDailyReportItem{
			Name:           item.Name,
			Related:        item.WorkflowName,
			Queue:          item.Queue,
			TaskExecutions: item.TaskExecutions,
			Triggers:       item.Triggers,
			NodeTasks:      item.NodeTasks,
			Success:        item.Success,
			Failed:         item.Failed,
			AverageMS:      item.AverageMS,
			MaxMS:          item.MaxMS,
			LastAt:         item.LastAt,
		})
	}
	return result
}

// toLarkWorkflowItems 转换工作流摘要为 Lark 日报 Top 项。
func toLarkWorkflowItems(items []WorkflowSummary) []larkx.TaskDailyReportItem {
	result := make([]larkx.TaskDailyReportItem, 0, len(items))
	for _, item := range items {
		result = append(result, larkx.TaskDailyReportItem{
			Name:           item.Name,
			Related:        item.Periodic,
			Queue:          item.Queue,
			TaskExecutions: item.Total,
			NodeTasks:      item.NodeTasks,
			Success:        item.Success,
			Failed:         item.Failed,
			Running:        item.Running,
			Unknown:        item.Unknown,
			AverageMS:      item.AverageMS,
			MaxMS:          item.MaxMS,
			LastAt:         item.LastAt,
		})
	}
	return result
}

// toLarkTasks 转换任务明细为 Lark 日报明细项。
func toLarkTasks(items []TaskSummary) []larkx.TaskDailyReportTask {
	result := make([]larkx.TaskDailyReportTask, 0, len(items))
	for _, item := range items {
		result = append(result, larkx.TaskDailyReportTask{
			ID:           item.ID,
			Name:         item.Name,
			Type:         item.Type,
			State:        item.State,
			Queue:        item.Queue,
			PeriodicName: item.PeriodicName,
			WorkflowID:   item.WorkflowID,
			WorkflowName: item.WorkflowName,
			WorkflowNode: item.WorkflowNode,
			StartedAt:    item.StartedAt,
			FinishedAt:   item.FinishedAt,
			DurationMS:   item.DurationMS,
			Error:        item.Error,
		})
	}
	return result
}
