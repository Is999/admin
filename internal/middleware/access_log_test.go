package middleware

import (
	"strings"
	"testing"

	"github.com/Is999/go-utils/errors"

	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/requestctx"
)

// TestShouldIgnoreAccessLog 验证访问日志白名单会跳过健康检查等低价值高频路由。
func TestShouldIgnoreAccessLog(t *testing.T) {

	if !shouldIgnoreAccessLog("/api/health") {
		t.Fatal("expected /api/health to be ignored by access log")
	}
	if !shouldIgnoreAccessLog("/api/live") {
		t.Fatal("expected /api/live to be ignored by access log")
	}
	if !shouldIgnoreAccessLog("/api/ready") {
		t.Fatal("expected /api/ready to be ignored by access log")
	}
	if !shouldIgnoreAccessLog("/api/metrics") {
		t.Fatal("expected /api/metrics to be ignored by access log")
	}
	if shouldIgnoreAccessLog("/api/tasks/queues") {
		t.Fatal("expected business api path not to be ignored by access log")
	}
}

// TestBuildAccessErrorChain 验证访问日志会优先输出完整错误链，而不是只保留最外层摘要。
func TestBuildAccessErrorChain(t *testing.T) {
	err := errors.Wrap(errors.New("pem decode failed"), "初始化RSA签名私钥失败")
	chain := loggerx.ErrorChain(err)
	if !strings.Contains(chain, "初始化RSA签名私钥失败") {
		t.Fatalf("error chain should contain outer message, got %q", chain)
	}
	if !strings.Contains(chain, "pem decode failed") {
		t.Fatalf("error chain should contain inner cause, got %q", chain)
	}
}

// TestAccessLogMessageIncludesRouteInfo 验证访问日志正文带上路径等排障关键信息。
func TestAccessLogMessageIncludesRouteInfo(t *testing.T) {
	msg := accessLogMessage(&requestctx.Meta{
		Method:    "POST",
		Path:      "/api/tasks/workflows",
		Route:     "task.workflow.trigger",
		BizCode:   1,
		LatencyMS: 12,
		UserID:    9,
		ClientIP:  "127.0.0.1",
	}, 200, true)
	for _, want := range []string{
		"method=POST",
		"path=/api/tasks/workflows",
		"route=task.workflow.trigger",
		"http_status=200",
		"biz_code=1",
		"latency_ms=12",
		"user_id=9",
		"ip=127.0.0.1",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("accessLogMessage() = %q, want contains %q", msg, want)
		}
	}
}
