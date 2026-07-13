package taskreport

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"admin/common/i18n"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"
	"admin/internal/task/taskwire"
	"admin/internal/types"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// TestWindow 验证日报窗口按指定基准时间回看 24 小时。
func TestWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	tests := []struct {
		name      string    // 测试场景名称
		now       time.Time // 日报生成时间
		wantStart time.Time // 期望窗口起点
		wantEnd   time.Time // 期望窗口终点
	}{
		{
			name:      "morning_schedule",
			now:       time.Date(2026, 7, 1, 9, 30, 0, 0, loc),
			wantStart: time.Date(2026, 6, 30, 9, 30, 0, 0, loc),
			wantEnd:   time.Date(2026, 7, 1, 9, 30, 0, 0, loc),
		},
		{
			name:      "afternoon_schedule",
			now:       time.Date(2026, 7, 1, 17, 0, 0, 987654321, loc),
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

// TestBuildRejectsSubsecondWindow 验证 Asynq 秒级时间无法表达的窗口边界会失败关闭。
func TestBuildRejectsSubsecondWindow(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 1, time.UTC)
	_, err := NewService(&reportFakeManager{}).Build(context.Background(), ReportRequest{
		WindowStart: end.Add(-24 * time.Hour),
		WindowEnd:   end,
		GeneratedAt: end,
	})
	if err == nil || !strings.Contains(err.Error(), "整秒对齐") {
		t.Fatalf("Build() error=%v, want second-aligned window error", err)
	}
}

// TestBuildRejectsInvalidWindow 验证服务入口本身会拒绝空区间或倒置区间。
func TestBuildRejectsInvalidWindow(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	for _, start := range []time.Time{end, end.Add(time.Second)} {
		_, err := NewService(&reportFakeManager{}).Build(context.Background(), ReportRequest{
			WindowStart: start,
			WindowEnd:   end,
			GeneratedAt: end,
		})
		if err == nil || !strings.Contains(err.Error(), "统计窗口非法") {
			t.Fatalf("Build() error=%v, want invalid window", err)
		}
	}
}

// TestBuildUsesWorkflowIdentityAndExcludesSelf 验证日报按工作流实例聚合，并排除当前日报实例。
func TestBuildUsesWorkflowIdentityAndExcludesSelf(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	archiveTrigger := reportTestItem("archive-trigger", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "archive-admin-log-hourly",
		taskwire.HeaderWorkflowName: "archive.run",
	}, 10, "")
	archiveTrigger.WorkflowID = "wf-archive"
	selfTrigger := reportTestItem("daily-trigger", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), end.Add(-time.Second).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "task-report-daily-summary",
		taskwire.HeaderWorkflowName: "task_report.daily_summary",
	}, 10, "")
	selfTrigger.WorkflowID = "wf-daily"
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {archiveTrigger, selfTrigger},
		},
		workflows: map[string]*types.TaskWorkflowStatusResp{
			"wf-archive": {WorkflowName: "archive.run", Status: "running"},
		},
		retention: 48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart:       start,
		WindowEnd:         end,
		GeneratedAt:       end,
		ExcludeWorkflowID: "wf-daily",
	})
	if err != nil {
		t.Fatalf("构建日报失败: %v", err)
	}
	if report.PeriodicTriggerTotal != 1 || report.WorkflowTotal != 1 || report.WorkflowRunning != 1 {
		t.Fatalf("日报未正确识别运行工作流或排除自身: triggers=%d workflows=%d running=%d", report.PeriodicTriggerTotal, report.WorkflowTotal, report.WorkflowRunning)
	}
	if len(report.Workflows) != 1 || report.Workflows[0].Name != "archive.run" {
		t.Fatalf("工作流摘要异常: %+v", report.Workflows)
	}
	completedRequests := 0
	for _, req := range manager.requests {
		if req.State == asynq.TaskStateCompleted.String() {
			completedRequests++
			if req.StartTime != start.Format(time.RFC3339) || req.EndTime != end.Add(-time.Second).Format(time.RFC3339) {
				t.Fatalf("completed 查询窗口异常: %+v", req)
			}
		}
	}
	if completedRequests != 1 {
		t.Fatalf("completed 应只查询一次最终 retention 窗口，实际=%d", completedRequests)
	}
	if len(manager.workflowRequests) != 1 || !reflect.DeepEqual(manager.workflowRequests[0], []string{"wf-archive"}) {
		t.Fatalf("工作流状态应单次批量读取并稳定排序: %+v", manager.workflowRequests)
	}
}

// TestBuildMarksMissingWorkflowStatusUnknown 验证工作流快照缺失时不会把入口任务成功误报为实例成功。
func TestBuildMarksMissingWorkflowStatusUnknown(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	trigger := reportTestItem("missing-workflow", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "missing-workflow",
		taskwire.HeaderWorkflowName: "archive.run",
	}, 10, "")
	trigger.WorkflowID = "wf-missing"
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {trigger},
		},
		workflowErrors: map[string]error{"wf-missing": redis.Nil},
		retention:      48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart: start,
		WindowEnd:   end,
		GeneratedAt: end,
	})
	if err != nil {
		t.Fatalf("构建日报失败: %v", err)
	}
	if report.WorkflowUnknown != 1 || report.WorkflowSuccess != 0 {
		t.Fatalf("工作流快照缺失时状态异常: success=%d unknown=%d", report.WorkflowSuccess, report.WorkflowUnknown)
	}
}

// TestBuildMarksInvalidWorkflowStatusUnknown 验证损坏的工作流状态不会按 completed 入口误报成功。
func TestBuildMarksInvalidWorkflowStatusUnknown(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	trigger := reportTestItem("invalid-workflow", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "invalid-workflow",
		taskwire.HeaderWorkflowName: "archive.run",
	}, 10, "")
	trigger.WorkflowID = "wf-invalid"
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {trigger},
		},
		workflows: map[string]*types.TaskWorkflowStatusResp{
			"wf-invalid": {WorkflowName: "archive.run", Status: "corrupt"},
		},
		retention: 48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart: start,
		WindowEnd:   end,
		GeneratedAt: end,
	})
	if err != nil {
		t.Fatalf("构建日报失败: %v", err)
	}
	if report.WorkflowUnknown != 1 || report.WorkflowSuccess != 0 {
		t.Fatalf("非法工作流状态不应误报成功: success=%d unknown=%d", report.WorkflowSuccess, report.WorkflowUnknown)
	}
}

// TestBuildReturnsWorkflowStatusReadError 验证基础设施读取失败会触发任务重试，而不是发送不完整日报。
func TestBuildReturnsWorkflowStatusReadError(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	trigger := reportTestItem("error-workflow", taskwire.TypeWorkflowTrigger, asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "error-workflow",
		taskwire.HeaderWorkflowName: "archive.run",
	}, 10, "")
	trigger.WorkflowID = "wf-error"
	wantErr := stderrors.New("redis unavailable")
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {trigger},
		},
		workflowErrors: map[string]error{"wf-error": wantErr},
		retention:      48 * time.Hour,
	}
	_, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart: start,
		WindowEnd:   end,
		GeneratedAt: end,
	})
	if !stderrors.Is(err, wantErr) {
		t.Fatalf("工作流状态读取错误=%v，期望包含=%v", err, wantErr)
	}
}

// TestBuildCountsArchivedTriggerAsFailedWorkflow 验证入口终态失败会同时计入任务和工作流失败。
func TestBuildCountsArchivedTriggerAsFailedWorkflow(t *testing.T) {
	req := ReportRequest{
		WindowStart: time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 7, 18, 10, 0, 1, 0, time.UTC),
	}
	trigger := reportTestItem("failed-trigger", taskwire.TypeWorkflowTrigger, asynq.TaskStateArchived.String(), req.WindowEnd.Add(-time.Hour).Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "failed-trigger",
		taskwire.HeaderWorkflowName: "archive.run",
	}, 10, "dispatch failed")
	trigger.WorkflowID = "wf-failed-trigger"
	report := buildReport(req, nil, []types.TaskItem{trigger}, map[string]workflowStatus{
		"wf-failed-trigger": {Name: "archive.run", Status: "running"},
	}, nil)
	if report.FailedTaskExecutions != 1 || report.WorkflowTotal != 1 || report.WorkflowFailed != 1 {
		t.Fatalf("入口归档失败不应被旧 running 快照覆盖: tasks=%d workflows=%d failed=%d", report.FailedTaskExecutions, report.WorkflowTotal, report.WorkflowFailed)
	}
}

// TestWorkflowStatusValueEvidencePrecedence 验证工作流终态、任务归档和非终态快照的证据优先级。
func TestWorkflowStatusValueEvidencePrecedence(t *testing.T) {
	tests := []struct {
		name      string // 测试场景名称
		taskState string // Asynq 任务状态
		status    string // 工作流快照状态
		want      string // 期望日报状态
	}{
		{name: "workflow_success_over_archived_task", taskState: asynq.TaskStateArchived.String(), status: "success", want: "success"},
		{name: "workflow_failed_over_completed_task", taskState: asynq.TaskStateCompleted.String(), status: "failed", want: "failed"},
		{name: "archived_over_stale_running", taskState: asynq.TaskStateArchived.String(), status: "running", want: "failed"},
		{name: "archived_over_unknown", taskState: asynq.TaskStateArchived.String(), status: "unknown", want: "failed"},
		{name: "completed_with_running", taskState: asynq.TaskStateCompleted.String(), status: "running", want: "running"},
		{name: "completed_with_unknown", taskState: asynq.TaskStateCompleted.String(), status: "unknown", want: "unknown"},
		{name: "completed_without_snapshot", taskState: asynq.TaskStateCompleted.String(), want: "success"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statuses := map[string]workflowStatus{}
			if tt.status != "" {
				statuses["wf-evidence"] = workflowStatus{Status: tt.status}
			}
			if got := workflowStatusValue("wf-evidence", tt.taskState, statuses); got != tt.want {
				t.Fatalf("状态证据优先级异常: got=%s want=%s", got, tt.want)
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
	report := buildReport(req, []QueueSummary{{Name: taskwire.QueueMaintenance}}, items, nil, nil)
	if report.TotalTaskExecutions != 2 || report.SuccessTaskExecutions != 1 || report.FailedTaskExecutions != 1 {
		t.Fatalf("unexpected task counts: %+v", report)
	}
	if report.PeriodicTriggerTotal != 1 || report.NodeTaskTotal != 1 {
		t.Fatalf("unexpected trigger/node counts: triggers=%d nodes=%d", report.PeriodicTriggerTotal, report.NodeTaskTotal)
	}
	if report.WorkflowTotal != 1 || report.WorkflowFailed != 1 || report.WorkflowSuccess != 0 {
		t.Fatalf("workflow status should prefer failed node, got success=%d failed=%d total=%d", report.WorkflowSuccess, report.WorkflowFailed, report.WorkflowTotal)
	}
	if len(report.Workflows) != 1 || report.Workflows[0].NodeTasks != 1 || report.Workflows[0].AverageMS != 3500 || report.Workflows[0].MaxMS != 3500 {
		t.Fatalf("工作流耗时应只统计节点任务: %+v", report.Workflows)
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

// TestBuildScansFullWindow 验证日报完整扫描 24 小时窗口。
func TestBuildScansFullWindow(t *testing.T) {
	req := ReportRequest{
		WindowStart: time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 7, 4, 10, 0, 5, 0, time.UTC),
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
	if report.TotalTaskExecutions != taskTotal || report.SuccessTaskExecutions != taskTotal {
		t.Fatalf("任务统计未覆盖完整窗口: total=%d success=%d", report.TotalTaskExecutions, report.SuccessTaskExecutions)
	}
	if len(report.PeriodicTasks) != 1 || report.PeriodicTasks[0].TaskExecutions != taskTotal {
		t.Fatalf("周期任务聚合未覆盖完整窗口: %+v", report.PeriodicTasks)
	}
}

// TestBuildRejectsPartialWindow 验证调用方不能只传一侧边界后被静默改成默认窗口。
func TestBuildRejectsPartialWindow(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	_, err := NewService(&reportFakeManager{}).Build(context.Background(), ReportRequest{WindowStart: start})
	if err == nil || !strings.Contains(err.Error(), "同时提供") {
		t.Fatalf("部分窗口边界应被拒绝，实际 err=%v", err)
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

// TestBuildAdjacentWindowsDoNotOverlap 验证相邻日报只在一个窗口计入边界任务。
func TestBuildAdjacentWindowsDoNotOverlap(t *testing.T) {
	boundary := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	task := reportTestItem("boundary-task", "demo:task", asynq.TaskStateCompleted.String(), boundary.Format(time.RFC3339), map[string]string{
		taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
		taskwire.HeaderPeriodicName: "boundary-periodic",
	}, 10, "")
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {task},
		},
		retention:         48 * time.Hour,
		archivedRetention: 7 * 24 * time.Hour,
	}
	previous, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart: boundary.Add(-24 * time.Hour),
		WindowEnd:   boundary,
		GeneratedAt: boundary,
	})
	if err != nil {
		t.Fatalf("构建前一窗口失败: %v", err)
	}
	next, err := NewService(manager).Build(context.Background(), ReportRequest{
		WindowStart: boundary,
		WindowEnd:   boundary.Add(24 * time.Hour),
		GeneratedAt: boundary.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("构建后一窗口失败: %v", err)
	}
	if previous.TotalTaskExecutions != 0 || next.TotalTaskExecutions != 1 {
		t.Fatalf("边界任务重复或遗漏: previous=%d next=%d", previous.TotalTaskExecutions, next.TotalTaskExecutions)
	}
}

// TestDedupeReportItemsPrefersLatestState 验证任务扫描期间跨状态迁移不会重复计数。
func TestDedupeReportItemsPrefersLatestState(t *testing.T) {
	archived := reportTestItem("same-task", "demo:task", asynq.TaskStateArchived.String(), "2026-07-18T09:59:00Z", map[string]string{
		taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
	}, 100, "failed")
	completed := reportTestItem("same-task", "demo:task", asynq.TaskStateCompleted.String(), "2026-07-18T09:59:30Z", map[string]string{
		taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
	}, 120, "")
	items := dedupeReportItems([]types.TaskItem{archived, completed})
	if len(items) != 1 || items[0].State != asynq.TaskStateCompleted.String() {
		t.Fatalf("跨状态任务去重结果异常: %+v", items)
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

// TestRetentionWarningAccountsForReportDelay 验证固定两天保留期同时覆盖窗口长度和生成延迟。
func TestRetentionWarningAccountsForReportDelay(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := NewService(&reportFakeManager{retention: 48 * time.Hour, archivedRetention: 7 * 24 * time.Hour})
	if got := svc.retentionWarning(context.Background(), start, start.Add(47*time.Hour)); got != "" {
		t.Fatalf("数据仍在保留期内时不应告警: %q", got)
	}
	if got := svc.retentionWarning(context.Background(), start, start.Add(48*time.Hour)); got == "" {
		t.Fatal("窗口最早数据达到保留期时应告警")
	}
}

// TestRetentionWarningUsesShorterArchivedRetention 验证失败归档保留期较短时也会提示数据不完整。
func TestRetentionWarningUsesShorterArchivedRetention(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := NewService(&reportFakeManager{retention: 48 * time.Hour, archivedRetention: time.Hour})
	if got := svc.retentionWarning(context.Background(), start, start.Add(2*time.Hour)); got == "" {
		t.Fatal("失败归档数据超过保留期时应告警")
	}
}

// TestBuildWarnsWhenArchivedWindowReachesAsynqTrimThreshold 验证失败风暴达到裁剪阈值时不会静默输出完整统计结论。
func TestBuildWarnsWhenArchivedWindowReachesAsynqTrimThreshold(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	items := make([]types.TaskItem, 0, asynqArchivedTrimThreshold)
	for index := 0; index < asynqArchivedTrimThreshold; index++ {
		items = append(items, reportTestItem(fmt.Sprintf("failed-%04d", index), "demo:periodic", asynq.TaskStateArchived.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "demo-periodic",
		}, 10, "boom"))
	}
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateArchived.String(): items,
		},
		retention:         48 * time.Hour,
		archivedRetention: 7 * 24 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	warnings := strings.Join(report.IntegrityWarnings, "\n")
	if len(report.IntegrityWarnings) != 2 || !strings.Contains(warnings, "9,999") || !strings.Contains(warnings, taskwire.QueueMaintenance) || !strings.Contains(warnings, "10000") || !strings.Contains(warnings, "completed") {
		t.Fatalf("archived 裁剪完整性提示异常: %+v", report.IntegrityWarnings)
	}
}

// TestBuildWarnsWhenCurrentArchivedQueueReachesTrimThreshold 验证窗口计数较少时仍使用当前队列水位识别裁剪风险。
func TestBuildWarnsWhenCurrentArchivedQueueReachesTrimThreshold(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	manager := &reportFakeManager{
		queues:            []types.TaskQueueItem{{Name: taskwire.QueueMaintenance, Archived: asynqArchivedTrimThreshold}},
		retention:         48 * time.Hour,
		archivedRetention: 7 * 24 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.IntegrityWarnings) != 1 || !strings.Contains(report.IntegrityWarnings[0], "9,999") || !strings.Contains(report.IntegrityWarnings[0], taskwire.QueueMaintenance) {
		t.Fatalf("当前 archived 水位裁剪提示异常: %+v", report.IntegrityWarnings)
	}
}

// TestBuildWarnsWhenStateScanIsTruncated 验证高吞吐窗口受控截断时明确标记统计不完整。
func TestBuildWarnsWhenStateScanIsTruncated(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	items := make([]types.TaskItem, 0, reportGlobalScanLimit)
	for index := 0; index < reportGlobalScanLimit; index++ {
		items = append(items, reportTestItem(fmt.Sprintf("success-%05d", index), "demo:periodic", asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "demo-periodic",
		}, 10, ""))
	}
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): items,
		},
		totals:    map[string]int64{asynq.TaskStateCompleted.String(): reportGlobalScanLimit + 1},
		retention: 48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.IntegrityWarnings) != 1 || !strings.Contains(report.IntegrityWarnings[0], "10000") || !strings.Contains(report.IntegrityWarnings[0], "completed") {
		t.Fatalf("扫描截断完整性提示异常: %+v", report.IntegrityWarnings)
	}
}

// TestBuildWarnsWhenStateByteBudgetIsReached 验证任务详情超过字节预算时显式输出不完整提示。
func TestBuildWarnsWhenStateByteBudgetIsReached(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): {
				reportTestItem("byte-limited", "demo:periodic", asynq.TaskStateCompleted.String(), end.Add(-time.Hour).Format(time.RFC3339), map[string]string{
					taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
				}, 1, ""),
			},
		},
		retention:   48 * time.Hour,
		byteLimited: true,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.TotalTaskExecutions != 0 || len(report.IntegrityWarnings) != 1 || !strings.Contains(report.IntegrityWarnings[0], "64 MiB") || !strings.Contains(report.IntegrityWarnings[0], "completed") {
		t.Fatalf("字节预算提示异常: total=%d warnings=%+v", report.TotalTaskExecutions, report.IntegrityWarnings)
	}
}

// TestBuildWarnsWhenQueueLimitIsReached 验证可见队列超限时不会把有限结果标成完整统计。
func TestBuildWarnsWhenQueueLimitIsReached(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	manager := &reportFakeManager{
		queues:       []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
		retention:    48 * time.Hour,
		queueLimited: true,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.IntegrityWarnings) != 1 || !strings.Contains(report.IntegrityWarnings[0], "32") {
		t.Fatalf("队列读取上限提示异常: %+v", report.IntegrityWarnings)
	}
}

// TestBuildUsesOneGlobalScanBudget 验证日报跨队列和状态共用严格扫描预算。
func TestBuildUsesOneGlobalScanBudget(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	tasks := make([]types.TaskItem, 0, reportGlobalScanLimit)
	for index := 0; index < reportGlobalScanLimit; index++ {
		tasks = append(tasks, reportTestItem(fmt.Sprintf("global-%05d", index), "demo:periodic", asynq.TaskStateCompleted.String(), end.Add(-time.Minute).Format(time.RFC3339), map[string]string{
			taskwire.HeaderTaskSource:   taskwire.WorkflowSourcePeriodic,
			taskwire.HeaderPeriodicName: "global-budget",
		}, 1, ""))
	}
	manager := &reportFakeManager{
		queues: []types.TaskQueueItem{{Name: taskwire.QueueCritical}, {Name: "aaa-auxiliary"}, {Name: taskwire.QueueMaintenance}},
		tasks: map[string][]types.TaskItem{
			asynq.TaskStateCompleted.String(): tasks,
		},
		totals:    map[string]int64{asynq.TaskStateCompleted.String(): reportGlobalScanLimit + 1},
		retention: 48 * time.Hour,
	}
	report, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.TotalTaskExecutions != reportGlobalScanLimit {
		t.Fatalf("全局扫描任务数=%d, want %d", report.TotalTaskExecutions, reportGlobalScanLimit)
	}
	for _, req := range manager.requests {
		if req.State == asynq.TaskStateCompleted.String() && req.Queue != taskwire.QueueCritical {
			t.Fatalf("核心队列未优先消费全局预算: %+v", req)
		}
	}
	if len(report.IntegrityWarnings) != 1 || !strings.Contains(report.IntegrityWarnings[0], "10000") || !strings.Contains(report.IntegrityWarnings[0], taskwire.QueueMaintenance+"/completed") {
		t.Fatalf("全局扫描预算提示异常: %+v", report.IntegrityWarnings)
	}
}

// TestBuildRejectsStateScanMutation 验证跨页期间总数变化或任务重复时不会静默发送缺数日报。
func TestBuildRejectsStateScanMutation(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	items := make([]types.TaskItem, 0, reportPageSize)
	for index := 0; index < reportPageSize; index++ {
		items = append(items, reportTestItem(fmt.Sprintf("stable-%03d", index), "demo:periodic", asynq.TaskStateCompleted.String(), end.Add(-time.Minute).Format(time.RFC3339), map[string]string{
			taskwire.HeaderTaskSource: taskwire.WorkflowSourcePeriodic,
		}, 1, ""))
	}
	tests := []struct {
		name string                                                               // 测试场景
		hook func(*types.ListTaskItemsReq, int, int) (*types.TaskListResp, error) // 注入跨页状态变化
		want string                                                               // 期望错误片段
	}{
		{
			name: "total_changed",
			hook: func(req *types.ListTaskItemsReq, offset, _ int) (*types.TaskListResp, error) {
				if req.State != asynq.TaskStateCompleted.String() {
					return &types.TaskListResp{Queue: req.Queue, State: req.State}, nil
				}
				if offset == 0 {
					return &types.TaskListResp{Queue: req.Queue, State: req.State, Total: 101, Tasks: append([]types.TaskItem(nil), items...)}, nil
				}
				return &types.TaskListResp{Queue: req.Queue, State: req.State, Total: 100}, nil
			},
			want: "扫描期间总数变化",
		},
		{
			name: "duplicate_task",
			hook: func(req *types.ListTaskItemsReq, offset, _ int) (*types.TaskListResp, error) {
				if req.State != asynq.TaskStateCompleted.String() {
					return &types.TaskListResp{Queue: req.Queue, State: req.State}, nil
				}
				if offset == 0 {
					return &types.TaskListResp{Queue: req.Queue, State: req.State, Total: 101, Tasks: append([]types.TaskItem(nil), items...)}, nil
				}
				return &types.TaskListResp{Queue: req.Queue, State: req.State, Total: 101, Tasks: []types.TaskItem{items[0]}}, nil
			},
			want: "扫描返回重复任务",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &reportFakeManager{
				queues:    []types.TaskQueueItem{{Name: taskwire.QueueMaintenance}},
				listHook:  tt.hook,
				retention: 48 * time.Hour,
			}
			_, err := NewService(manager).Build(context.Background(), ReportRequest{WindowStart: start, WindowEnd: end, GeneratedAt: end})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Build() error=%v, want contains %q", err, tt.want)
			}
		})
	}
}

// reportTestItem 构造日报聚合测试使用的任务快照。
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

// reportFakeManager 提供日报服务测试所需的队列和任务快照。
type reportFakeManager struct {
	queues            []types.TaskQueueItem                                                // 待返回的队列状态列表
	tasks             map[string][]types.TaskItem                                          // 按任务状态分组的任务列表
	workflows         map[string]*types.TaskWorkflowStatusResp                             // 按实例 ID 分组的工作流状态
	workflowErrors    map[string]error                                                     // 按实例 ID 分组的工作流读取错误
	workflowRequests  [][]string                                                           // 批量工作流状态读取请求
	requests          []types.ListTaskItemsReq                                             // 日报发起的任务列表请求
	totals            map[string]int64                                                     // 按状态覆盖任务总数
	retention         time.Duration                                                        // 已完成任务保留时长
	archivedRetention time.Duration                                                        // 失败归档任务保留时长
	listHook          func(*types.ListTaskItemsReq, int, int) (*types.TaskListResp, error) // 自定义列表响应，用于复现跨页集合变化
	queueLimited      bool                                                                 // 是否模拟日报队列达到读取上限
	byteLimited       bool                                                                 // 是否模拟日报详情达到字节读取上限
}

// ListReportQueues 返回测试预置的有界队列状态副本。
func (m *reportFakeManager) ListReportQueues(_ context.Context, limit int) (*types.TaskQueueListResp, bool, error) {
	queues := append([]types.TaskQueueItem(nil), m.queues...)
	limited := m.queueLimited || len(queues) > limit
	if len(queues) > limit {
		queues = queues[:limit]
	}
	return &types.TaskQueueListResp{Queues: queues}, limited, nil
}

// ListTasks 按状态和分页参数返回测试预置的任务列表。
func (m *reportFakeManager) ListReportTasks(_ context.Context, req *types.ListTaskItemsReq, offset, limit int, byteBudget int64) (*types.TaskListResp, types.TaskReportScanUsage, error) {
	m.requests = append(m.requests, *req)
	if m.listHook != nil {
		resp, err := m.listHook(req, offset, limit)
		result, usage := reportFakeScanResult(resp, offset, byteBudget, m.byteLimited)
		return result, usage, err
	}
	if limit <= 0 {
		limit = reportPageSize
	}
	items := m.tasks[req.State]
	total := int64(len(items))
	if override, ok := m.totals[req.State]; ok {
		total = override
	}
	start := offset
	if start >= len(items) {
		resp := &types.TaskListResp{Queue: req.Queue, State: req.State, PageSize: limit, Total: total, Tasks: []types.TaskItem{}}
		result, usage := reportFakeScanResult(resp, offset, byteBudget, m.byteLimited)
		return result, usage, nil
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	resp := &types.TaskListResp{
		Queue:    req.Queue,
		State:    req.State,
		PageSize: limit,
		Total:    total,
		Tasks:    append([]types.TaskItem(nil), items[start:end]...),
	}
	result, usage := reportFakeScanResult(resp, offset, byteBudget, m.byteLimited)
	return result, usage, nil
}

// reportFakeScanResult 模拟生产入口每次读取一个固定大小的 Asynq 原生详情页。
func reportFakeScanResult(resp *types.TaskListResp, offset int, byteBudget int64, byteLimited bool) (*types.TaskListResp, types.TaskReportScanUsage) {
	if resp == nil || resp.Total <= int64(offset) {
		return resp, types.TaskReportScanUsage{}
	}
	usage := types.TaskReportScanUsage{Tasks: reportPageSize, Bytes: reportPageSize * 256}
	if byteLimited || usage.Bytes > byteBudget {
		usage.Bytes = 0
		usage.ByteLimited = true
		resp.Tasks = []types.TaskItem{}
	}
	return resp, usage
}

// GetWorkflowStatusSummaries 批量返回测试预置的工作流轻量状态或读取错误。
func (m *reportFakeManager) GetWorkflowStatusSummaries(_ context.Context, workflowIDs []string) (map[string]*types.TaskWorkflowStatusResp, error) {
	m.workflowRequests = append(m.workflowRequests, append([]string(nil), workflowIDs...))
	result := make(map[string]*types.TaskWorkflowStatusResp, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		if err := m.workflowErrors[workflowID]; err != nil {
			if stderrors.Is(err, redis.Nil) {
				continue
			}
			return nil, err
		}
		if item := m.workflows[workflowID]; item != nil {
			result[workflowID] = item
		}
	}
	return result, nil
}

// CompletedRetention 返回测试指定的保留时长。
func (m *reportFakeManager) CompletedRetention() time.Duration {
	return m.retention
}

// ArchivedRetention 返回测试指定的失败归档保留时长。
func (m *reportFakeManager) ArchivedRetention() time.Duration {
	return m.archivedRetention
}
