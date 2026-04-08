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

	"admin_cron/internal/config"

	"github.com/Is999/go-utils/errors"
)

// TestSendTaskFailureBuildsProfessionalText 验证告警文本、签名和换行错误摘要符合生产排障要求。
func TestSendTaskFailureBuildsProfessionalText(t *testing.T) {
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
		ServiceName:  "admin_cron",
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
	if got.MsgType != "text" {
		t.Fatalf("msg_type=%s, want text", got.MsgType)
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
	for _, want := range []string{
		"【P1 后台任务终态失败】",
		"状态：已归档失败，不会继续自动重试",
		"任务\n- 名称：工作流触发:user_tag.delta.refresh",
		"- 来源：periodic",
		"工作流\n- 名称：user_tag.delta.refresh",
		"- 工作流ID：wf-1",
		"- 分片：2/10",
		"错误摘要\n下游返回失败 code=500",
		"处理建议\n请在任务中心按任务ID或工作流ID检索执行日志",
	} {
		if !strings.Contains(got.Content.Text, want) {
			t.Fatalf("payload text missing %q:\n%s", want, got.Content.Text)
		}
	}
	if strings.Contains(got.Content.Text, "\ncode=500") {
		t.Fatalf("error summary should be normalized: %s", got.Content.Text)
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
