package task

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"admin/internal/jobs/taskreport"
	"admin/internal/svc"
	queue "admin/internal/task/queue"
	"admin/internal/task/taskwire"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// PluginName 是任务运行日报插件名称，供 taskruntime 包装注册时保持唯一。
	PluginName = "task_report"
	// maxReportWindow 限制手动补跑窗口，避免误扫过大的 Asynq 历史集合。
	maxReportWindow = 72 * time.Hour
	// maxReportFutureSkew 容忍调度节点与 worker 的极小时间偏差，同时拒绝未来统计窗口。
	maxReportFutureSkew = 2 * time.Second
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
		start, end, err := resolveReportWindow(payload, task.Headers()[taskwire.HeaderScheduledAt])
		if err != nil {
			return errors.Tag(err)
		}
		report, err := taskreport.NewService(runtime.Manager()).Build(ctx, taskreport.ReportRequest{
			WindowStart:       start,
			WindowEnd:         end,
			GeneratedAt:       time.Now(),
			ExcludeWorkflowID: payload.WorkflowID,
		})
		if err != nil {
			taskreport.RecordBuildError(ctx)
			return errors.Wrap(err, "生成任务运行日报失败")
		}
		taskreport.RecordReportTrace(ctx, report)
		if err := taskreport.SendLarkReport(ctx, runtime.ServiceContext(), report); err != nil {
			return errors.Tag(err)
		}
		return nil
	})); err != nil {
		return errors.Tag(err)
	}
	return runtime.RegisterWorkflow(dailySummaryWorkflow())
}

// decodePayload 解析任务载荷；空载荷使用默认日报窗口。
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

// resolveReportWindow 解析手动窗口，并限制最大回看范围。
func resolveReportWindow(payload taskreport.TaskPayload, scheduledAt string) (time.Time, time.Time, error) {
	now := time.Now()
	windowEnd := now
	if scheduledAt = strings.TrimSpace(scheduledAt); scheduledAt != "" {
		parsed, err := time.Parse(time.RFC3339, scheduledAt)
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "解析日报计划触发时间失败")
		}
		windowEnd = parsed
	}
	start, end := taskreport.Window(windowEnd)
	windowStartText := strings.TrimSpace(payload.WindowStart)
	windowEndText := strings.TrimSpace(payload.WindowEnd)
	if (windowStartText == "") != (windowEndText == "") {
		return time.Time{}, time.Time{}, errors.Errorf("日报手动统计窗口必须同时提供开始和结束时间")
	}
	var err error
	if windowStartText != "" {
		start, err = time.Parse(time.RFC3339, windowStartText)
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "解析日报窗口开始时间失败")
		}
		end, err = time.Parse(time.RFC3339, windowEndText)
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "解析日报窗口结束时间失败")
		}
	}
	if start.Nanosecond() != 0 || end.Nanosecond() != 0 {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口必须按整秒对齐: start=%s end=%s", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口非法: start=%s end=%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	if end.Sub(start) > maxReportWindow {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口超过上限: window=%s max=%s", end.Sub(start), maxReportWindow)
	}
	if end.After(now.Add(maxReportFutureSkew)) {
		return time.Time{}, time.Time{}, errors.Errorf("日报统计窗口结束时间不能位于未来: end=%s now=%s", end.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	return start, end, nil
}

// dailySummaryWorkflow 构造任务运行日报工作流定义。
func dailySummaryWorkflow() *queue.WorkflowDefinition {
	return &queue.WorkflowDefinition{
		Name:                     taskreport.WorkflowNameDailySummary,
		Description:              "周期任务与工作流运行日报工作流，按统计窗口聚合任务执行、失败、慢任务和队列状态",
		DefaultQueue:             queue.QueueMaintenance,
		PeriodicUniqueBySchedule: true,
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
