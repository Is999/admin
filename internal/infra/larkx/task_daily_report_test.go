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
		RetentionWarning: "completed_retention_seconds 不大于统计窗口",
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
		"**队列积压**\npending 1 / active 2 / retry 0 / archived 1",
		"**总览**",
		"- 周期触发：4 次，成功 3，失败 1",
		"- 工作流：4 个，成功 3，失败 1",
		"**重点关注**",
		"- 失败：任务 1，工作流 1",
		"**失败明细**",
		"错误 db timeout query killed",
		"**周期任务 Top**",
		"user-report-recalc-today｜执行 4 / 成功 3 / 失败 1 / 触发 1",
		"**工作流 Top**",
		"user_report.recalc_today｜实例 1 / 成功 0 / 失败 1 / 运行 0",
		"**慢任务 Top**",
		"**处理建议**",
		"- 数据完整性：completed_retention_seconds 不大于统计窗口",
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
