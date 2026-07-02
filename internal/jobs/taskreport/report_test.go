package taskreport

import (
	"context"
	"testing"
	"time"

	"admin/common/i18n"
	"admin/internal/config"
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

// TestToLarkReportUsesModeEnvironment 确保 Lark 日报环境复用顶层 Mode。
func TestToLarkReportUsesModeEnvironment(t *testing.T) {
	cfg := config.Config{
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
			Environment: "custom-env",
		},
	}
	cfg.Mode = "pro"

	got := ToLarkReport(cfg, Report{})

	if got.Environment != "pro" {
		t.Fatalf("期望 Lark 日报环境复用 Mode，实际为 %q", got.Environment)
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
	svc := NewService(reportTestManager{completedRetention: time.Hour})
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

type reportTestManager struct {
	completedRetention time.Duration // completed 任务保留期
}

// ListQueues 表示测试辅助逻辑。
func (m reportTestManager) ListQueues(context.Context) (*types.TaskQueueListResp, error) {
	return &types.TaskQueueListResp{}, nil
}

// ListTasks 表示测试辅助逻辑。
func (m reportTestManager) ListTasks(context.Context, *types.ListTaskItemsReq) (*types.TaskListResp, error) {
	return &types.TaskListResp{}, nil
}

// GetWorkflowStatus 表示测试辅助逻辑。
func (m reportTestManager) GetWorkflowStatus(context.Context, string) (*types.TaskWorkflowStatusResp, error) {
	return nil, nil
}

// CompletedRetention 表示测试辅助逻辑。
func (m reportTestManager) CompletedRetention() time.Duration {
	return m.completedRetention
}
