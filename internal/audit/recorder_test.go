package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"admin_cron/internal/model"
	"admin_cron/internal/requestctx"
)

// TestSerializeMasksSensitiveFields 验证审计序列化时会自动脱敏密码、token、MFA 秘钥等敏感字段。
func TestSerializeMasksSensitiveFields(t *testing.T) {
	payload := Serialize(map[string]any{
		"password": "secret",
		"token":    "jwt",
		"profile": map[string]any{
			"mfa_secure_key": "abc",
			"nickname":       "tester",
		},
	}, 1024)

	if strings.Contains(payload, "secret") || strings.Contains(payload, "jwt") || strings.Contains(payload, "abc") {
		t.Fatalf("expected sensitive fields to be masked, got %s", payload)
	}
	if !strings.Contains(payload, "***") {
		t.Fatalf("expected masked placeholder in payload: %s", payload)
	}
}

// TestBuildLogEntryUsesRequestMeta 验证审计落库前会自动合并请求链路元数据，并继承 trace、用户和响应结果。
func TestBuildLogEntryUsesRequestMeta(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetTrace(ctx, "trace-1", "span-1")
	requestctx.SetRoute(ctx, "admin.log.query")
	requestctx.SetUser(ctx, 99, "tester", "127.0.0.1")
	requestctx.SetResponse(ctx, 200, 1001, "参数错误", "参数错误")
	requestctx.SetLatency(ctx, 123456*time.Millisecond)

	recorder := NewRecorder(nil, 1024)
	entry := recorder.buildLogEntry(ctx, Event{
		Action:   model.ActionAdminLogQuery,
		Method:   "QueryAdminLogHandler",
		Describe: "查询管理员操作日志",
		Data: map[string]any{
			"password": "secret",
			"page":     1,
		},
	})

	if entry.TraceID != "trace-1" || entry.SpanID != "span-1" {
		t.Fatalf("unexpected trace info: %+v", entry)
	}
	if entry.UserID != 99 || entry.UserName != "tester" {
		t.Fatalf("unexpected user info: %+v", entry)
	}
	if entry.Route != "admin.log.query" || entry.HTTPStatus != 200 || entry.BizCode != 1001 {
		t.Fatalf("unexpected route/response info: %+v", entry)
	}
	if entry.LatencyMS != 123456 {
		t.Fatalf("expected latency ms 123456, got %d", entry.LatencyMS)
	}
	if entry.Success {
		t.Fatalf("expected success=false when error message exists")
	}
	if strings.Contains(entry.Data, "secret") {
		t.Fatalf("expected sanitized data, got %s", entry.Data)
	}
}
