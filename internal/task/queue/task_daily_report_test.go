package taskqueue

import (
	"testing"
	"time"

	taskstats "admin/internal/task/stats"
	"admin/internal/types"

	"github.com/hibiken/asynq"
)

// TestTaskDailyReportWindow 验证日报窗口固定按 10 点切分。
func TestTaskDailyReportWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	tests := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "before_ten",
			now:       time.Date(2026, 6, 30, 9, 59, 0, 0, loc),
			wantStart: time.Date(2026, 6, 28, 10, 0, 0, 0, loc),
			wantEnd:   time.Date(2026, 6, 29, 10, 0, 0, 0, loc),
		},
		{
			name:      "after_ten",
			now:       time.Date(2026, 6, 30, 10, 1, 0, 0, loc),
			wantStart: time.Date(2026, 6, 29, 10, 0, 0, 0, loc),
			wantEnd:   time.Date(2026, 6, 30, 10, 0, 0, 0, loc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := TaskDailyReportWindow(tt.now)
			if !gotStart.Equal(tt.wantStart) || !gotEnd.Equal(tt.wantEnd) {
				t.Fatalf("window=(%s,%s), want (%s,%s)", gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBuildTaskDailyReportAggregatesPeriodicWorkflow 验证周期来源任务会按周期任务、工作流和队列聚合。
func TestBuildTaskDailyReportAggregatesPeriodicWorkflow(t *testing.T) {
	req := TaskDailyReportRequest{
		WindowStart: time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 6, 30, 10, 0, 5, 0, time.UTC),
	}
	items := []types.TaskItem{
		dailyReportTestItem("node-1", "user_report:recalc_today", asynq.TaskStateArchived.String(), "2026-06-30T09:55:00Z", map[string]string{
			headerTaskSource:   WorkflowSourcePeriodic,
			HeaderPeriodicName: "user-report-recalc-today",
			headerWorkflowID:   "wf-1",
			headerWorkflowName: "user_report.recalc_today",
			headerWorkflowNode: "recalc_today",
		}, 3500, "db timeout"),
		dailyReportTestItem("trigger-1", TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), "2026-06-30T09:50:00Z", map[string]string{
			headerTaskSource:   WorkflowSourcePeriodic,
			HeaderPeriodicName: "user-report-recalc-today",
			headerWorkflowID:   "wf-1",
			headerWorkflowName: "user_report.recalc_today",
		}, 120, ""),
		dailyReportTestItem("api-1", "user_report:recalc_today", asynq.TaskStateCompleted.String(), "2026-06-30T09:40:00Z", map[string]string{
			headerTaskSource:   WorkflowSourceAPI,
			headerWorkflowName: "user_report.recalc_today",
		}, 100, ""),
	}
	items[0].ExecutionTrace = &taskstats.Snapshot{
		TotalCount:  12,
		ReadCount:   10,
		UpdateCount: 1,
		ErrorCount:  1,
	}
	report := buildTaskDailyReport(req, []TaskDailyReportQueueSummary{{Name: QueueMaintenance}}, items, nil, false, "")
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

// TestTaskDailyReportContainsUsesExclusiveEnd 验证日报统计窗口右边界不重复计入。
func TestTaskDailyReportContainsUsesExclusiveEnd(t *testing.T) {
	start := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	timeRange := taskListTimeRange{start: start, end: end, hasRange: true}
	inside := dailyReportTestItem("inside", "demo:task", asynq.TaskStateCompleted.String(), end.Add(-time.Second).Format(time.RFC3339), map[string]string{
		headerTaskSource: WorkflowSourcePeriodic,
	}, 100, "")
	boundary := dailyReportTestItem("boundary", "demo:task", asynq.TaskStateCompleted.String(), end.Format(time.RFC3339), map[string]string{
		headerTaskSource: WorkflowSourcePeriodic,
	}, 100, "")
	if !taskDailyReportContains(timeRange, inside) {
		t.Fatalf("expected task before end to be included")
	}
	if taskDailyReportContains(timeRange, boundary) {
		t.Fatalf("expected task at exclusive end to be excluded")
	}
}

func dailyReportTestItem(id string, taskType string, state string, at string, headers map[string]string, durationMS int64, lastErr string) types.TaskItem {
	return types.TaskItem{
		ID:           id,
		Queue:        QueueMaintenance,
		TaskType:     taskType,
		TaskName:     taskType,
		State:        state,
		LastErr:      lastErr,
		StartedAt:    at,
		CompletedAt:  at,
		LastFailedAt: at,
		DurationMS:   durationMS,
		Headers:      headers,
	}
}
