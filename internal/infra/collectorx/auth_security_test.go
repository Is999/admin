package collectorx

import (
	"context"
	"encoding/json"
	"testing"
)

// TestAuthSecurityProcessorRejectsInvalidPayload 确保异常事件按单条结果失败，不中断批处理。
func TestAuthSecurityProcessorRejectsInvalidPayload(t *testing.T) {
	processor := NewAuthSecurityProcessor()
	results, err := processor.ProcessBatch(context.Background(), []Event{
		{
			EventID: "bad",
			BizType: BizTypeAuthSecurity,
			Payload: json.RawMessage(`{bad`),
		},
		{
			EventID: "ok",
			BizType: BizTypeAuthSecurity,
			Payload: json.RawMessage(`{"action":"auth_failed","reason":"token_invalid","app_id":"site-a"}`),
		},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if results[0].Success || results[0].Error == "" {
		t.Fatalf("first result = %+v, want failed", results[0])
	}
	if !results[1].Success || results[1].Error != "" {
		t.Fatalf("second result = %+v, want success", results[1])
	}
}

// TestNormalizeAuthSecurityLabels 确保认证事件指标标签不会被异常值撑爆。
func TestNormalizeAuthSecurityLabels(t *testing.T) {
	if got := normalizeAuthSecurityAction("login_success"); got != "login_success" {
		t.Fatalf("normalizeAuthSecurityAction() = %q", got)
	}
	if got := normalizeAuthSecurityAction("dynamic_action"); got != authSecurityLabelOther {
		t.Fatalf("normalizeAuthSecurityAction(dynamic) = %q, want other", got)
	}
	if got := normalizeAuthSecurityReason(""); got != authSecurityLabelUnknown {
		t.Fatalf("normalizeAuthSecurityReason(empty) = %q, want unknown", got)
	}
	if got := normalizeAuthSecurityReason("security_payload_too_large"); got != AuthSecurityReasonSecurityPayloadTooLarge {
		t.Fatalf("normalizeAuthSecurityReason(payload limit) = %q, want %q", got, AuthSecurityReasonSecurityPayloadTooLarge)
	}
	if got := normalizeAuthSecurityAppID("site-a"); got != "site-a" {
		t.Fatalf("normalizeAuthSecurityAppID() = %q", got)
	}
	if got := normalizeAuthSecurityAppID("site.api"); got != "site.api" {
		t.Fatalf("normalizeAuthSecurityAppID(dot) = %q", got)
	}
	if got := normalizeAuthSecurityAppID("site/a"); got != authSecurityLabelOther {
		t.Fatalf("normalizeAuthSecurityAppID(invalid) = %q, want other", got)
	}
}

// TestAuthSecurityContracts 确保认证风控动作、原因和分类都从契约派生。
func TestAuthSecurityContracts(t *testing.T) {
	actionSeen := make(map[string]struct{})
	for _, action := range defaultAuthSecurityActions {
		if action == "" {
			t.Fatal("empty auth security action")
		}
		if _, ok := actionSeen[action]; ok {
			t.Fatalf("duplicate auth security action=%s", action)
		}
		actionSeen[action] = struct{}{}
		if got := normalizeAuthSecurityAction(action); got != action {
			t.Fatalf("normalizeAuthSecurityAction(%s)=%s", action, got)
		}
	}

	reasonSeen := make(map[string]struct{})
	for _, contract := range defaultAuthSecurityReasonContracts {
		if contract.Reason == "" || contract.Category == "" {
			t.Fatalf("invalid auth security reason contract=%+v", contract)
		}
		if _, ok := reasonSeen[contract.Reason]; ok {
			t.Fatalf("duplicate auth security reason=%s", contract.Reason)
		}
		reasonSeen[contract.Reason] = struct{}{}
		if got := normalizeAuthSecurityReason(contract.Reason); got != contract.Reason {
			t.Fatalf("normalizeAuthSecurityReason(%s)=%s", contract.Reason, got)
		}
		if got := normalizeAuthSecurityCategory(contract.Reason); got != contract.Category {
			t.Fatalf("normalizeAuthSecurityCategory(%s)=%s, want %s", contract.Reason, got, contract.Category)
		}
	}
}

// TestNormalizeAuthSecurityCategory 确保认证安全指标按低基数分类聚合。
func TestNormalizeAuthSecurityCategory(t *testing.T) {
	tests := []struct {
		name   string // name 表示测试场景名称。
		reason string // reason 表示安全失败原因。
		want   string // want 表示期望结果。
	}{
		{name: "auth", reason: AuthSecurityReasonInvalidPassword, want: authSecurityCategoryAuth},
		{name: "token", reason: AuthSecurityReasonTokenInvalid, want: authSecurityCategoryToken},
		{name: "rate limit", reason: AuthSecurityReasonLoginIPRateLimited, want: authSecurityCategoryRateLimit},
		{name: "identity rate limit", reason: AuthSecurityReasonLoginIdentityRateLimited, want: authSecurityCategoryRateLimit},
		{name: "security client", reason: AuthSecurityReasonSignatureFailed, want: authSecurityCategorySecurityClient},
		{name: "security config", reason: AuthSecurityReasonSecurityKeyUnavailable, want: authSecurityCategorySecurityConfig},
		{name: "payload limit", reason: AuthSecurityReasonSecurityPayloadTooLarge, want: authSecurityCategorySecurityPayloadLimit},
		{name: "security response", reason: AuthSecurityReasonResponseEncryptFailed, want: authSecurityCategorySecurityResponse},
		{name: "session lifecycle", reason: AuthSecurityReasonSessionRotated, want: authSecurityCategorySessionLifecycle},
		{name: "unknown", reason: "", want: authSecurityLabelUnknown},
		{name: "other", reason: "dynamic_reason", want: authSecurityLabelOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAuthSecurityCategory(tt.reason); got != tt.want {
				t.Fatalf("normalizeAuthSecurityCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}
