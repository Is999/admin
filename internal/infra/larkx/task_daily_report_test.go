package larkx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
)

// TestSendTaskDailyReportBuildsReadableCard 验证任务运行日报使用卡片并包含关键汇总分区。
func TestSendTaskDailyReportBuildsReadableCard(t *testing.T) {
	var got messagePayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	notifier, err := New(config.LarkAlertConfig{
		Enabled:       true,
		WebhookURL:    server.URL,
		MaxErrorBytes: 120,
		AtAll:         true,
	})
	if err != nil {
		t.Fatalf("new notifier failed: %v", err)
	}
	notifier.client = server.Client()
	notifier.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }

	err = notifier.SendTaskDailyReport(context.Background(), TaskDailyReport{
		ReportID:              "report-1",
		ServiceName:           "admin",
		Environment:           "prod",
		AppID:                 "203",
		WindowStart:           time.Date(2026, 6, 29, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		WindowEnd:             time.Date(2026, 6, 30, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		GeneratedAt:           time.Date(2026, 6, 30, 10, 0, 5, 0, time.FixedZone("CST", 8*3600)),
		TotalTaskExecutions:   12,
		SuccessTaskExecutions: 11,
		FailedTaskExecutions:  1,
		PeriodicTriggerTotal:  4,
		PeriodicTriggerOK:     3,
		PeriodicTriggerFailed: 1,
		NodeTaskTotal:         8,
		WorkflowTotal:         4,
		WorkflowSuccess:       3,
		WorkflowFailed:        1,
		TraceTotalCount:       10,
		TraceErrorCount:       7,
		AverageDurationMS:     1200,
		MaxDurationMS:         3500,
		Queues: []TaskDailyReportQueue{{
			Name:           "maintenance",
			TaskExecutions: 12,
			Success:        11,
			Failed:         1,
			Triggers:       4,
			NodeTasks:      8,
			Pending:        1,
			Active:         2,
			Scheduled:      3,
			Retry:          0,
			Archived:       1,
		}},
		PeriodicTasks: []TaskDailyReportItem{{
			Name:           "user-report-recalc-today",
			Related:        "user_report.recalc_today",
			Queue:          "maintenance",
			TaskExecutions: 4,
			Triggers:       1,
			NodeTasks:      3,
			Success:        3,
			Failed:         1,
			AverageMS:      1200,
			MaxMS:          3500,
			LastAt:         "2026-06-30T09:58:00+08:00",
		}},
		Workflows: []TaskDailyReportItem{{
			Name:           "user_report.recalc_today",
			Related:        "user-report-recalc-today",
			Queue:          "maintenance",
			TaskExecutions: 1,
			NodeTasks:      3,
			Success:        0,
			Failed:         1,
		}},
		TimeBuckets: []TaskDailyReportTimeBucket{{
			StartAt:           "2026-06-30T09:00:00+08:00",
			EndAt:             "2026-06-30T10:00:00+08:00",
			TaskExecutions:    4,
			Success:           3,
			Failed:            1,
			Triggers:          1,
			NodeTasks:         3,
			TraceTotalCount:   10,
			TraceReadCount:    2,
			TraceWriteCount:   3,
			TraceDeleteCount:  1,
			TraceErrorCount:   7,
			AverageDurationMS: 1200,
			MaxDurationMS:     3500,
		}},
		FailureTasks: []TaskDailyReportTask{{
			ID:           "task-1",
			Name:         "user_report.recalc_today/recalc_today",
			State:        "archived",
			Queue:        "maintenance",
			PeriodicName: "user-report-recalc-today",
			WorkflowID:   "wf-1",
			WorkflowName: "user_report.recalc_today",
			WorkflowNode: "recalc_today",
			FinishedAt:   "2026-06-30T09:58:00+08:00",
			DurationMS:   3500,
			Error:        "db timeout\nquery killed",
		}},
		SlowTasks: []TaskDailyReportTask{{
			Name:         "user_report.recalc_today/recalc_today",
			State:        "archived",
			Queue:        "maintenance",
			WorkflowName: "user_report.recalc_today",
			DurationMS:   3500,
		}},
		TraceErrorTasks: []TaskDailyReportTask{{
			Name:         "user_tag.delta.refresh/sync_kafka",
			State:        "completed",
			Queue:        "maintenance",
			WorkflowName: "user_tag.delta.refresh",
			WorkflowNode: "sync_kafka",
			FinishedAt:   "2026-06-30T09:57:00+08:00",
			TraceErrors:  7,
			TraceDetails: []string{"kafka_outbox 7"},
		}},
		IntegrityWarnings: []string{"窗口最早数据已达到终态任务保留期"},
	})
	if err != nil {
		t.Fatalf("send task daily report failed: %v", err)
	}
	if got.MsgType != "interactive" {
		t.Fatalf("msg_type=%s, want interactive", got.MsgType)
	}
	if got.Content != nil {
		t.Fatalf("daily report should use card instead of text: %+v", got.Content)
	}
	if got.Card == nil || got.Card.Header == nil {
		t.Fatalf("missing card/header: %+v", got)
	}
	if !got.Card.Config.WideScreenMode {
		t.Fatalf("daily report card should use wide screen mode")
	}
	if got.Card.Header.Template != "red" {
		t.Fatalf("header template=%s, want red", got.Card.Header.Template)
	}
	text := requireCardText(t, got)
	for _, want := range []string{
		"P3 任务运行日报 | 存在失败",
		"**状态**：存在终态失败",
		"**窗口**：2026-06-29 10:00 ~ 2026-06-30 10:00",
		"**日报 ID**\nreport-1",
		"**队列积压**\npending 1 / active 2 / scheduled 3 / retry 0 / archived 1",
		"**总览**",
		"- 周期触发：4 次，成功 3，失败 1",
		"- 涉及工作流：4 个，成功 3，失败 1",
		"- 处理量：总 10，读 0，写 0，删 0，错 7",
		"**重点关注**",
		"- 失败：任务 1，工作流 1",
		"- 处理异常：隔离错误 7",
		"**高峰时段**",
		"任务高峰：2026-06-30 09:00~10:00｜执行 4 / 触发 1 / 节点 3 / 失败 1",
		"处理量高峰：2026-06-30 09:00~10:00｜总 10 / 读 2 / 写 3 / 删 1 / 错 7",
		"错误高峰：2026-06-30 09:00~10:00｜错 7 / 执行 4",
		"最慢时段：2026-06-30 09:00~10:00｜均 1.2s / 最长 3.5s",
		"**失败明细**",
		"错误 db timeout query killed",
		"**周期任务 Top**",
		"user-report-recalc-today｜执行 4 / 成功 3 / 失败 1 / 触发 1",
		"**工作流 Top**",
		"user_report.recalc_today｜实例 1 / 成功 0 / 失败 1 / 运行 0",
		"**慢任务 Top**",
		"**处理异常 Top**",
		"user_tag.delta.refresh/sync_kafka；错 7；时间 2026-06-30 09:57",
		"明细 kafka_outbox 7",
		"**处理建议**",
		"- 数据完整性：窗口最早数据已达到终态任务保留期",
		"处理量存在隔离错误",
		`<at id=all></at>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q:\n%s", want, text)
		}
	}
}

// TestTaskDailyReportAtAllsWorkflowFailure 验证工作流失败也会触发日报 @all。
func TestTaskDailyReportAtAllsWorkflowFailure(t *testing.T) {
	notifier := &Notifier{
		atAll:        true,
		maxErrorByte: defaultMaxErrorBytes,
		now:          func() time.Time { return time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC) },
	}
	card := notifier.formatTaskDailyReportCard(TaskDailyReport{
		ServiceName:          "admin",
		Environment:          "prod",
		WindowStart:          time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
		WindowEnd:            time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		WorkflowTotal:        1,
		WorkflowFailed:       1,
		FailureTasks:         nil,
		FailedTaskExecutions: 0,
	})
	text := cardTextContent(card)
	if !strings.Contains(text, `<at id=all></at>`) {
		t.Fatalf("expected workflow failure to at all:\n%s", text)
	}
}

// TestTaskDailyReportTraceErrorsNeedAttention 验证处理量隔离错误会进入需关注卡片。
func TestTaskDailyReportTraceErrorsNeedAttention(t *testing.T) {
	notifier := &Notifier{
		atAll:        true,
		maxErrorByte: defaultMaxErrorBytes,
		now:          func() time.Time { return time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC) },
	}
	card := notifier.formatTaskDailyReportCard(TaskDailyReport{
		ServiceName:          "admin",
		Environment:          "prod",
		WindowStart:          time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC),
		WindowEnd:            time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		TraceTotalCount:      20,
		TraceErrorCount:      2,
		WorkflowTotal:        1,
		WorkflowSuccess:      1,
		PeriodicTriggerTotal: 1,
		PeriodicTriggerOK:    1,
		TraceErrorTasks: []TaskDailyReportTask{{
			Name:         "user_tag.runtime.cleanup/runtime_cleanup",
			WorkflowName: "user_tag.runtime.cleanup",
			WorkflowNode: "runtime_cleanup",
			FinishedAt:   "2026-06-30T09:50:00Z",
			TraceErrors:  2,
		}},
	})
	if card.Header == nil || card.Header.Template != "orange" || card.Header.Title.Content != "P3 任务运行日报 | 需关注" {
		t.Fatalf("trace error card header unexpected: %+v", card.Header)
	}
	text := cardTextContent(card)
	if strings.Contains(text, `<at id=all></at>`) {
		t.Fatalf("trace error should not at all without terminal failure:\n%s", text)
	}
	for _, want := range []string{
		"**状态**：存在处理量隔离错误",
		"**处理异常 Top**",
		"user_tag.runtime.cleanup/runtime_cleanup；错 2；时间 2026-06-30 09:50",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q:\n%s", want, text)
		}
	}
}

// TestTaskDailyReportIntegrityWarningAvoidsFalseHealthyAdvice 验证数据不完整时不会宣称窗口内确定无失败。
func TestTaskDailyReportIntegrityWarningAvoidsFalseHealthyAdvice(t *testing.T) {
	notifier := &Notifier{maxErrorByte: defaultMaxErrorBytes, now: time.Now}
	warning := "archived 任务达到实际裁剪阈值，失败统计可能不完整"
	card := notifier.formatTaskDailyReportCard(TaskDailyReport{
		ServiceName:       "admin",
		IntegrityWarnings: []string{warning},
	})
	if card.Header == nil || card.Header.Template != "orange" || card.Header.Title.Content != "P3 任务运行日报 | 需关注" {
		t.Fatalf("完整性风险卡片等级异常: %+v", card.Header)
	}
	text := cardTextContent(card)
	if !strings.Contains(text, "**状态**：统计完整性存在风险，当前可见数据不能证明窗口运行正常") {
		t.Fatalf("完整性风险状态摘要缺失:\n%s", text)
	}
	if !strings.Contains(text, "当前可见数据未发现终态失败，但存在数据完整性风险") {
		t.Fatalf("完整性风险处理建议缺失:\n%s", text)
	}
	if strings.Contains(text, "当前窗口无终态失败；继续观察") {
		t.Fatalf("完整性风险下不应输出确定无失败结论:\n%s", text)
	}
	if count := strings.Count(text, warning); count != 2 {
		t.Fatalf("完整性提示应分别在重点关注和处理建议出现一次，实际=%d:\n%s", count, text)
	}
}

// TestTaskDailyReportUnknownWorkflowDoesNotClaimHealthy 验证未知工作流状态不会输出正常结论。
func TestTaskDailyReportUnknownWorkflowDoesNotClaimHealthy(t *testing.T) {
	notifier := &Notifier{maxErrorByte: defaultMaxErrorBytes, now: time.Now}
	card := notifier.formatTaskDailyReportCard(TaskDailyReport{ServiceName: "admin", WorkflowTotal: 1, WorkflowUnknown: 1})
	text := cardTextContent(card)
	if !strings.Contains(text, "**状态**：存在无法确认状态的工作流，当前不能判定窗口运行正常") {
		t.Fatalf("未知工作流状态摘要缺失:\n%s", text)
	}
	if strings.Contains(text, "**状态**：周期任务与工作流运行正常") {
		t.Fatalf("未知工作流不应输出正常结论:\n%s", text)
	}
}
