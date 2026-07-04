package taskreport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"admin/common/i18n"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"
	"admin/internal/task/taskwire"
	"admin/internal/types"

	"github.com/hibiken/asynq"
)

// TestWindow 验证日报窗口按执行时间回看 24 小时。
func TestWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	tests := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "morning_schedule",
			now:       time.Date(2026, 7, 1, 9, 30, 0, 0, loc),
			wantStart: time.Date(2026, 6, 30, 9, 30, 0, 0, loc),
			wantEnd:   time.Date(2026, 7, 1, 9, 30, 0, 0, loc),
		},
		{
			name:      "afternoon_schedule",
			now:       time.Date(2026, 7, 1, 17, 0, 0, 0, loc),
			wantStart: time.Date(2026, 6, 30, 17, 0, 0, 0, loc),
			wantEnd:   time.Date(2026, 7, 1, 17, 0, 0, 0, loc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := Window(tt.now)
			if !gotStart.Equal(tt.wantStart) || !gotEnd.Equal(tt.wantEnd) {
				t.Fatalf("window=(%s,%s), want (%s,%s)", gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBuildReportAggregatesPeriodicWorkflow 验证周期来源任务会按周期任务、工作流和队列聚合。
func TestBuildReportAggregatesPeriodicWorkflow(t *testing.T) {
	req := ReportRequest{
		WindowStart: time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 6, 30, 10, 0, 5, 0, time.UTC),
	}
	items := []types.TaskItem{
		reportTestItem("node-1", "user_report:recalc_today", asynq.TaskStateArchived.String(), "2026-06-30T09:55:00Z", map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "user-report-recalc-today",
			taskwire.HeaderWorkflowID:   "wf-1",
			taskwire.HeaderWorkflowName: "user_report.recalc_today",
			taskwire.HeaderWorkflowNode: "recalc_today",
		}, 3500, "db timeout"),
		reportTestItem("trigger-1", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), "2026-06-30T09:50:00Z", map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "user-report-recalc-today",
			taskwire.HeaderWorkflowID:   "wf-1",
			taskwire.HeaderWorkflowName: "user_report.recalc_today",
		}, 120, ""),
		reportTestItem("api-1", "user_report:recalc_today", asynq.TaskStateCompleted.String(), "2026-06-30T09:40:00Z", map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourceAPI,
			taskwire.HeaderWorkflowName: "user_report.recalc_today",
		}, 100, ""),
	}
	items[0].ExecutionTrace = &taskstats.Snapshot{
		TotalCount:  12,
		ReadCount:   10,
		UpdateCount: 1,
		ErrorCount:  1,
		Details: []taskstats.Detail{{
			Action: taskstats.ActionError,
			Name:   "user_report.rows",
			Count:  1,
		}},
	}
	report := buildReport(req, []QueueSummary{{Name: taskwire.QueueMaintenance}}, items, nil, false, "")
	if report.TotalTaskExecutions != 2 || report.SuccessTaskExecutions != 1 || report.FailedTaskExecutions != 1 {
		t.Fatalf("unexpected task counts: %+v", report)
	}
	if report.PeriodicTriggerTotal != 1 || report.NodeTaskTotal != 1 {
		t.Fatalf("unexpected trigger/node counts: triggers=%d nodes=%d", report.PeriodicTriggerTotal, report.NodeTaskTotal)
	}
	if report.WorkflowTotal != 1 || report.WorkflowFailed != 1 || report.WorkflowSuccess != 0 {
		t.Fatalf("workflow status should prefer failed node, got success=%d failed=%d total=%d", report.WorkflowSuccess, report.WorkflowFailed, report.WorkflowTotal)
	}
	if len(report.PeriodicTasks) != 1 || report.PeriodicTasks[0].Failed != 1 || report.PeriodicTasks[0].Triggers != 1 {
		t.Fatalf("unexpected periodic summary: %+v", report.PeriodicTasks)
	}
	if len(report.FailureTasks) != 1 || report.FailureTasks[0].ID != "node-1" {
		t.Fatalf("unexpected failure tasks: %+v", report.FailureTasks)
	}
	if report.TraceTotalCount != 12 || report.TraceReadCount != 10 || report.TraceWriteCount != 1 || report.TraceErrorCount != 1 {
		t.Fatalf("unexpected trace counts: total=%d read=%d write=%d error=%d", report.TraceTotalCount, report.TraceReadCount, report.TraceWriteCount, report.TraceErrorCount)
	}
	if len(report.TraceErrorTasks) != 1 || report.TraceErrorTasks[0].TraceErrors != 1 || len(report.TraceErrorTasks[0].TraceDetails) != 1 {
		t.Fatalf("unexpected trace error tasks: %+v", report.TraceErrorTasks)
	}
	if len(report.TimeBuckets) != 1 {
		t.Fatalf("unexpected time buckets: %+v", report.TimeBuckets)
	}
	bucket := report.TimeBuckets[0]
	if bucket.TaskExecutions != 2 || bucket.Triggers != 1 || bucket.NodeTasks != 1 || bucket.TraceTotalCount != 12 || bucket.TraceErrorCount != 1 {
		t.Fatalf("unexpected time bucket summary: %+v", bucket)
	}
}

// TestBuildScansFullWindowBeyondLegacyLimit 验证日报完整扫描 24 小时窗口，不再按 2000 条截断。
func TestBuildScansFullWindowBeyondLegacyLimit(t *testing.T) {
	req := ReportRequest{
		WindowStart: time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 7, 4, 10, 0, 5, 0, time.UTC),
		PageSize:    1000,
	}
	const taskTotal = 2005
	tasks := make([]types.TaskItem, 0, taskTotal)
	for idx := 0; idx < taskTotal; idx++ {
		tasks = append(tasks, reportTestItem(fmt.Sprintf("task-%04d", idx), "demo:periodic", asynq.TaskStateCompleted.String(), req.WindowEnd.Add(-time.Duration(idx%3600+1)*time.Second).Format(time.RFC3339), map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "demo-periodic",
		}, 0, ""))
	}
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): tasks,
		},
		retention: 48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), req)
	if err != nil {
		t.Fatalf("构建日报失败: %v", err)
	}
	if report.Truncated {
		t.Fatalf("日报不应再按 2000 条截断")
	}
	if report.TotalTaskExecutions != taskTotal || report.SuccessTaskExecutions != taskTotal {
		t.Fatalf("任务统计未覆盖完整窗口: total=%d success=%d", report.TotalTaskExecutions, report.SuccessTaskExecutions)
	}
	if len(report.PeriodicTasks) != 1 || report.PeriodicTasks[0].TaskExecutions != taskTotal {
		t.Fatalf("周期任务聚合未覆盖完整窗口: %+v", report.PeriodicTasks)
	}
}

// TestReportContainsUsesExclusiveEnd 验证日报统计窗口右边界不重复计入。
func TestReportContainsUsesExclusiveEnd(t *testing.T) {
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	window := reportWindow{start: start, end: end, hasRange: true}
	inside := reportTestItem("inside", "demo:task", asynq.TaskStateCompleted.String(), end.Add(-time.Second).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
	}, 100, "")
	boundary := reportTestItem("boundary", "demo:task", asynq.TaskStateCompleted.String(), end.Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
	}, 100, "")
	if !reportContains(window, inside) {
		t.Fatalf("expected task before end to be included")
	}
	if reportContains(window, boundary) {
		t.Fatalf("expected task at exclusive end to be excluded")
	}
}

// TestRetentionWarningUsesRequestLocale 验证日报可见提示通过统一语言包按请求语言返回。
func TestRetentionWarningUsesRequestLocale(t *testing.T) {
	svc := NewService(&reportFakeManager{retention: time.Hour})
	start := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleENUS)

	got := svc.retentionWarning(ctx, start, end)
	want := i18n.MessageByKey(i18n.MsgKeyTaskReportRetentionWarning, i18n.LocaleENUS)
	if got != want {
		t.Fatalf("retentionWarning()=%q, want %q", got, want)
	}
}

func reportTestItem(id string, taskType string, state string, at string, headers map[string]string, durationMS int64, lastErr string) types.TaskItem {
	return types.TaskItem{
		ID:           id,
		Queue:        taskwire.QueueMaintenance,
		TaskType:     taskType,
		TaskName:     taskType,
		WorkflowID:   headers[taskwire.HeaderWorkflowID],
		State:        state,
		LastErr:      lastErr,
		StartedAt:    at,
		CompletedAt:  at,
		LastFailedAt: at,
		DurationMS:   durationMS,
		Headers:      headers,
	}
}

type reportFakeManager struct {
	queues    []types.TaskQueueItem
	tasks     map[string][]types.TaskItem
	retention time.Duration
}

func (m *reportFakeManager) ListQueues(context.Context) (*types.TaskQueueListResp, error) {
	return &types.TaskQueueListResp{Queues: append([]types.TaskQueueItem(nil), m.queues...)}, nil
}

func (m *reportFakeManager) ListTasks(_ context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error) {
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = reportPageSize
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	items := m.tasks[req.State]
	start := (page - 1) * pageSize
	if start >= len(items) {
		return &types.TaskListResp{Queue: req.Queue, State: req.State, Page: page, PageSize: pageSize, Total: int64(len(items)), Tasks: []types.TaskItem{}}, nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return &types.TaskListResp{
		Queue:    req.Queue,
		State:    req.State,
		Page:     page,
		PageSize: pageSize,
		Total:    int64(len(items)),
		Tasks:    append([]types.TaskItem(nil), items[start:end]...),
	}, nil
}

func (m *reportFakeManager) GetWorkflowStatus(context.Context, string) (*types.TaskWorkflowStatusResp, error) {
	return nil, nil
}

func (m *reportFakeManager) CompletedRetention() time.Duration {
	if m.retention <= 0 {
		return 24 * time.Hour
	}
	return m.retention
}
