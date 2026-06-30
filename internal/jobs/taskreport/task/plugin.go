package task

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"admin/internal/config"
	"admin/internal/infra/larkx"
	"admin/internal/jobs/taskreport"
	"admin/internal/svc"
	queue "admin/internal/task/queue"
	taskstats "admin/internal/task/stats"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// PluginName 是任务运行日报插件名称，供 taskruntime 包装注册时保持唯一。
	PluginName = "task_report"
	// maxReportWindow 限制手动补跑窗口，避免误扫过大的 Asynq 历史集合。
	maxReportWindow = 72 * time.Hour
)

// Runtime 描述任务运行日报注册所需的最小运行时能力。
// 该接口让 taskreport/task 包承载任务细节，同时避免反向依赖 taskruntime 造成包循环。
type Runtime interface {
	// ServiceContext 返回任务处理器共享的服务上下文。
	ServiceContext() *svc.ServiceContext
	// Manager 返回任务队列管理器，用于聚合队列和工作流运行数据。
	Manager() *queue.Manager
	// RegisterHandler 注册 Asynq 任务处理器。
	RegisterHandler(pattern string, handler asynq.Handler) error
	// RegisterWorkflow 注册任务运行日报工作流定义。
	RegisterWorkflow(def *queue.WorkflowDefinition) error
}

// Setup 注册任务运行日报 handler 和工作流定义。
func Setup(runtime Runtime) error {
	if runtime == nil || runtime.ServiceContext() == nil || runtime.Manager() == nil {
		return nil
	}
	if err := runtime.RegisterHandler(taskreport.TaskTypeDailySummary, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodePayload(task)
		if err != nil {
			return errors.Tag(err)
		}
		start, end, err := resolveReportWindow(payload)
		if err != nil {
			return errors.Tag(err)
		}
		report, err := runtime.Manager().BuildTaskDailyReport(ctx, queue.TaskDailyReportRequest{
			WindowStart: start,
			WindowEnd:   end,
			GeneratedAt: time.Now(),
		})
		if err != nil {
			taskstats.RecordError(ctx, taskstats.JoinDetailName(taskreport.TraceNameDailySummary, taskstats.DetailPartRows), 1)
			return errors.Wrap(err, "生成任务运行日报失败")
		}
		recordReportTrace(ctx, report)
		if err := sendReport(ctx, runtime.ServiceContext(), report); err != nil {
			taskstats.RecordError(ctx, taskstats.JoinDetailName(taskreport.TraceNameDailySummary, "lark"), 1)
			return errors.Tag(err)
		}
		return nil
	})); err != nil {
		return errors.Tag(err)
	}
	return runtime.RegisterWorkflow(dailySummaryWorkflow())
}

func decodePayload(task *asynq.Task) (taskreport.TaskPayload, error) {
	var payload taskreport.TaskPayload
	if task == nil || len(task.Payload()) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return payload, errors.Wrap(err, "解析任务运行日报任务载荷失败")
	}
	return payload, nil
}

func resolveReportWindow(payload taskreport.TaskPayload) (time.Time, time.Time, error) {
	now := time.Now()
	start, end := queue.TaskDailyReportWindow(now)
	var err error
	if strings.TrimSpace(payload.WindowStart) != "" {
		start, err = time.Parse(time.RFC3339, strings.TrimSpace(payload.WindowStart))
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "解析日报窗口开始时间失败")
		}
	}
	if strings.TrimSpace(payload.WindowEnd) != "" {
		end, err = time.Parse(time.RFC3339, strings.TrimSpace(payload.WindowEnd))
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "解析日报窗口结束时间失败")
		}
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口非法: start=%s end=%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	if end.Sub(start) > maxReportWindow {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口超过上限: window=%s max=%s", end.Sub(start), maxReportWindow)
	}
	return start, end, nil
}

func sendReport(ctx context.Context, svcCtx *svc.ServiceContext, report queue.TaskDailyReport) error {
	if svcCtx == nil {
		return nil
	}
	cfg := svcCtx.CurrentConfig()
	notifier, err := larkx.New(cfg.Alert.Lark)
	if err != nil {
		return errors.Wrap(err, "初始化 Lark 日报通知失败")
	}
	if notifier == nil {
		logx.WithContext(ctx).Info("Lark 日报通知未启用，跳过任务运行日报发送")
		return nil
	}
	// 发送外部通知使用独立背景上下文，避免任务收尾 deadline 截断 HTTP 请求。
	if err := notifier.SendTaskDailyReport(context.Background(), toLarkReport(cfg, report)); err != nil {
		return errors.Wrap(err, "发送任务运行日报 Lark 通知失败")
	}
	return nil
}

func toLarkReport(cfg config.Config, report queue.TaskDailyReport) larkx.TaskDailyReport {
	result := larkx.TaskDailyReport{
		ServiceName:           strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:           strings.TrimSpace(cfg.Observability.Environment),
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
	if result.Environment == "" {
		result.Environment = strings.TrimSpace(cfg.Mode)
	}
	result.Queues = make([]larkx.TaskDailyReportQueue, 0, len(report.QueueSummaries))
	for _, item := range report.QueueSummaries {
		result.Queues = append(result.Queues, larkx.TaskDailyReportQueue{
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
	result.PeriodicTasks = make([]larkx.TaskDailyReportItem, 0, len(report.PeriodicTasks))
	for _, item := range report.PeriodicTasks {
		result.PeriodicTasks = append(result.PeriodicTasks, larkx.TaskDailyReportItem{
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
	result.Workflows = make([]larkx.TaskDailyReportItem, 0, len(report.Workflows))
	for _, item := range report.Workflows {
		result.Workflows = append(result.Workflows, larkx.TaskDailyReportItem{
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
	result.FailureTasks = toLarkReportTasks(report.FailureTasks)
	result.SlowTasks = toLarkReportTasks(report.SlowTasks)
	return result
}

func toLarkReportTasks(items []queue.TaskDailyReportTask) []larkx.TaskDailyReportTask {
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

func recordReportTrace(ctx context.Context, report queue.TaskDailyReport) {
	taskstats.RecordRead(ctx, taskstats.JoinDetailName(taskreport.TraceNameDailySummary, "task_executions"), int64(report.TotalTaskExecutions))
	taskstats.RecordRead(ctx, taskstats.JoinDetailName(taskreport.TraceNameDailySummary, "workflow_instances"), int64(report.WorkflowTotal))
	taskstats.RecordError(ctx, taskstats.JoinDetailName(taskreport.TraceNameDailySummary, "failed_tasks"), int64(report.FailedTaskExecutions))
}

func dailySummaryWorkflow() *queue.WorkflowDefinition {
	return &queue.WorkflowDefinition{
		Name:         taskreport.WorkflowNameDailySummary,
		Description:  "周期任务与工作流运行日报工作流，按固定窗口聚合任务执行、失败、慢任务和队列状态",
		DefaultQueue: queue.QueueMaintenance,
		Nodes: map[string]*queue.WorkflowNodeDefinition{
			taskreport.NodeDailySummary: {
				Name:     taskreport.NodeDailySummary,
				TaskType: taskreport.TaskTypeDailySummary,
				Queue:    queue.QueueMaintenance,
				MaxRetry: 1,
				Timeout:  5 * time.Minute,
				BuildPayload: func(spec queue.WorkflowStartSpec, node *queue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
					return json.Marshal(taskreport.TaskPayload{
						WorkflowID:   spec.WorkflowID,
						WorkflowName: spec.Name,
						WorkflowNode: node.Name,
						ShardIndex:   shardIndex,
						ShardTotal:   shardTotal,
					})
				},
			},
		},
	}
}
