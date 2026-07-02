package larkx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

func requirePayloadText(t *testing.T, payload messagePayload) string {
	t.Helper()
	if payload.Content == nil {
		t.Fatalf("payload content is nil: %+v", payload)
	}
	return payload.Content.Text
}

func requireCardText(t *testing.T, payload messagePayload) string {
	t.Helper()
	if payload.Card == nil {
		t.Fatalf("payload card is nil: %+v", payload)
	}
	return cardTextContent(*payload.Card)
}

func cardTextContent(card messageCard) string {
	parts := make([]string, 0, 8)
	if card.Header != nil {
		parts = append(parts, card.Header.Title.Content)
	}
	for _, element := range card.Elements {
		if element.Text != nil {
			parts = append(parts, element.Text.Content)
		}
		for _, field := range element.Fields {
			parts = append(parts, field.Text.Content)
		}
		for _, item := range element.Elements {
			parts = append(parts, item.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// TestSendTaskFailureBuildsProfessionalCard 验证终态失败告警卡片、签名和换行错误摘要符合生产排障要求。
func TestSendTaskFailureBuildsProfessionalCard(t *testing.T) {
	var got messagePayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	notifier, err := New(config.LarkAlertConfig{
		Enabled:        true,
		WebhookURL:     server.URL,
		Secret:         "secret",
		TimeoutSeconds: 1,
		MaxErrorBytes:  80,
	})
	if err != nil {
		t.Fatalf("new notifier failed: %v", err)
	}
	notifier.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	notifier.client = server.Client()

	err = notifier.SendTaskFailure(context.Background(), TaskFailureAlert{
		ServiceName:  "admin",
		Environment:  "pro",
		AppID:        "1",
		TaskID:       "task-1",
		TaskType:     "workflow:trigger",
		TaskName:     "工作流触发:user_tag.delta.refresh",
		TaskQueue:    "maintenance",
		TaskSource:   "periodic",
		WorkflowID:   "wf-1",
		WorkflowName: "user_tag.delta.refresh",
		WorkflowNode: "sync_kafka",
		ShardIndex:   2,
		ShardTotal:   10,
		TraceID:      "trace-1",
		OccurredAt:   time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		Err:          errors.Errorf("下游返回失败\ncode=500"),
	})
	if err != nil {
		t.Fatalf("send task failure failed: %v", err)
	}
	if got.MsgType != "interactive" {
		t.Fatalf("msg_type=%s, want interactive", got.MsgType)
	}
	if got.Timestamp != "1700000000" {
		t.Fatalf("timestamp=%s", got.Timestamp)
	}
	if got.Sign != sign("1700000000", "secret") {
		t.Fatalf("unexpected sign")
	}
	if got.Sign != "fiWS2+gh28DOydAv7hzONH/mDn9+b1Y4Y5ivXWXy8vA=" {
		t.Fatalf("sign=%s", got.Sign)
	}
	if got.Content != nil {
		t.Fatalf("task failure should use card instead of text: %+v", got.Content)
	}
	if got.Card == nil || got.Card.Header == nil || got.Card.Header.Template != "red" {
		t.Fatalf("unexpected task failure card: %+v", got.Card)
	}
	text := requireCardText(t, got)
	for _, want := range []string{
		"P1 后台任务终态失败",
		"**状态**：已归档失败，不会继续自动重试",
		"**服务**\nadmin",
		"**环境 / 站点**\npro / 1",
		"**任务**\n工作流触发:user_tag.delta.refresh",
		"**队列 / 来源**\nmaintenance / periodic",
		"**工作流**\nuser_tag.delta.refresh",
		"**分片**\n2/10",
		"**错误摘要**\n下游返回失败 code=500",
		"**处理建议**\n- 在任务中心按任务ID或工作流ID检索执行日志",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\ncode=500") {
		t.Fatalf("error summary should be normalized: %s", text)
	}
}

// TestSendTaskFailureRejectsLarkBusinessError 验证 HTTP 成功但 Lark 业务失败时会返回错误。
func TestSendTaskFailureRejectsLarkBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":9499,"msg":"sign invalid"}`))
	}))
	defer server.Close()

	notifier, err := New(config.LarkAlertConfig{
		Enabled:    true,
		WebhookURL: server.URL,
	})
	if err != nil {
		t.Fatalf("new notifier failed: %v", err)
	}
	notifier.client = server.Client()

	err = notifier.SendTaskFailure(context.Background(), TaskFailureAlert{Err: errors.New("boom")})
	if err == nil || !strings.Contains(err.Error(), "code=9499") {
		t.Fatalf("want lark business error, got %v", err)
	}
}

// TestSendPeriodicConfigInvalidBuildsProfessionalCard 验证周期任务配置异常告警卡片适合生产排障。
func TestSendPeriodicConfigInvalidBuildsProfessionalCard(t *testing.T) {
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

	err = notifier.SendPeriodicConfigInvalid(context.Background(), PeriodicConfigAlert{
		ServiceName:  "admin",
		Environment:  "prod",
		AppID:        "site-1",
		TaskIndex:    14,
		TaskName:     "task-report-daily-summary",
		WorkflowName: "task_report.daily_summary",
		Cron:         "0 10 * * *",
		TaskQueue:    "maintenance",
		UniqueKey:    "periodic:task_report.daily_summary",
		OccurredAt:   time.Date(2026, 6, 29, 15, 33, 40, 0, time.FixedZone("CST", 8*3600)),
		Reason:       "工作流定义不存在",
		TriggerCount: 3,
	})
	if err != nil {
		t.Fatalf("send periodic config invalid failed: %v", err)
	}
	if got.MsgType != "interactive" {
		t.Fatalf("msg_type=%s, want interactive", got.MsgType)
	}
	if got.Card == nil || got.Card.Header == nil || got.Card.Header.Template != "red" {
		t.Fatalf("unexpected periodic config card: %+v", got.Card)
	}
	text := requireCardText(t, got)
	for _, want := range []string{
		"P1 周期任务配置异常",
		"**状态**：已跳过该周期任务，调度器继续运行",
		"**序号**\n14",
		"**名称**\ntask-report-daily-summary",
		"**工作流**\ntask_report.daily_summary",
		"**Cron / Every**\ncron 0 10 * * *",
		"**队列**\nmaintenance",
		"**去重键**\nperiodic:task_report.daily_summary",
		"**窗口触发次数**\n3",
		"**错误摘要**\n工作流定义不存在",
		"**影响与建议**\n- 该周期任务不会注册到调度器",
		`<at id=all></at>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q:\n%s", want, text)
		}
	}
}

// TestSendTaskRuntimeAlertBuildsProfessionalCard 验证任务系统运行异常告警卡片适合生产排障。
func TestSendTaskRuntimeAlertBuildsProfessionalCard(t *testing.T) {
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

	err = notifier.SendTaskRuntimeAlert(context.Background(), TaskRuntimeAlert{
		ServiceName:  "admin",
		Environment:  "prod",
		AppID:        "site-1",
		Kind:         "periodic_enqueue_failed",
		Title:        "【P1 周期任务入队失败】",
		Status:       "本轮周期任务未成功投递到队列，调度器继续运行",
		Component:    "scheduler",
		Operation:    "enqueue_periodic_task",
		TaskName:     "周期任务触发:task-report-daily-summary",
		TaskType:     "workflow:trigger",
		WorkflowName: "task_report.daily_summary",
		Cron:         "0 10 * * *",
		TaskQueue:    "maintenance",
		UniqueKey:    "periodic:task_report.daily_summary",
		OccurredAt:   time.Date(2026, 6, 29, 15, 40, 0, 0, time.FixedZone("CST", 8*3600)),
		Reason:       "redis: connection refused",
		Advice:       "请检查 maintenance 队列和 Redis 连接池。",
		TriggerCount: 4,
	})
	if err != nil {
		t.Fatalf("send task runtime alert failed: %v", err)
	}
	if got.MsgType != "interactive" {
		t.Fatalf("msg_type=%s, want interactive", got.MsgType)
	}
	if got.Card == nil || got.Card.Header == nil || got.Card.Header.Template != "red" {
		t.Fatalf("unexpected runtime alert card: %+v", got.Card)
	}
	text := requireCardText(t, got)
	for _, want := range []string{
		"P1 周期任务入队失败",
		"**状态**：本轮周期任务未成功投递到队列，调度器继续运行",
		"**类型**\nperiodic_enqueue_failed",
		"**组件 / 动作**\nscheduler / enqueue_periodic_task",
		"**任务**\n周期任务触发:task-report-daily-summary",
		"**任务类型**\nworkflow:trigger",
		"**工作流**\ntask_report.daily_summary",
		"**Cron**\n0 10 * * *",
		"**队列**\nmaintenance",
		"**去重键**\nperiodic:task_report.daily_summary",
		"**窗口触发次数**\n4",
		"**错误摘要**\nredis: connection refused",
		"**处理建议**\n- 请检查 maintenance 队列和 Redis 连接池。",
		`<at id=all></at>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q:\n%s", want, text)
		}
	}
}

// TestSendTextSendsPlainText 验证通用文本消息发送链路。
func TestSendTextSendsPlainText(t *testing.T) {
	payloadCh := make(chan messagePayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload messagePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("解析 Lark 请求失败: %v", err)
		}
		payloadCh <- payload
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()

	notifier, err := New(config.LarkAlertConfig{
		Enabled:    true,
		WebhookURL: server.URL,
		Secret:     "secret",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	notifier.now = func() time.Time {
		return time.Unix(1700000000, 0)
	}

	if err = notifier.SendText(context.Background(), "CDC 审核日志"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	payload := <-payloadCh
	if payload.MsgType != "text" || requirePayloadText(t, payload) != "CDC 审核日志" {
		t.Fatalf("Lark payload 不符合预期: %+v", payload)
	}
	if payload.Timestamp == "" || payload.Sign == "" {
		t.Fatalf("期望签名字段不为空: %+v", payload)
	}
}

// TestNewRejectsInvalidWebhookURL 验证启用告警时会拒绝非法 webhook URL。
func TestNewRejectsInvalidWebhookURL(t *testing.T) {
	_, err := New(config.LarkAlertConfig{
		Enabled:    true,
		WebhookURL: "open-apis/bot/v2/hook/token",
	})
	if err == nil || !strings.Contains(err.Error(), "http/https") {
		t.Fatalf("want invalid webhook url error, got %v", err)
	}
}

// TestTruncateErrorKeepsUTF8 验证错误摘要按字节截断时不破坏 UTF-8 字符。
func TestTruncateErrorKeepsUTF8(t *testing.T) {
	notifier := &Notifier{maxErrorByte: 10}
	got := notifier.truncateError(errors.New("错误摘要内容很长"))
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("want ellipsis, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text should be valid utf8: %q", got)
	}
	if len(got) > 10 {
		t.Fatalf("truncated text length=%d, want <=10", len(got))
	}
}

// TestNewDisabledReturnsNil 验证未启用 Lark 告警时不会创建通知器。
func TestNewDisabledReturnsNil(t *testing.T) {
	notifier, err := New(config.LarkAlertConfig{})
	if err != nil {
		t.Fatalf("new disabled notifier failed: %v", err)
	}
	if notifier != nil {
		t.Fatal("disabled notifier should be nil")
	}
}
