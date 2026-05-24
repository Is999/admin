package middleware

import (
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Is999/go-utils/errors"

	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"
)

// TestSkipAccessLogMarksRequestMeta 验证路由 wrapper 会把跳过日志规则写入请求元数据。
func TestSkipAccessLogMarksRequestMeta(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	ctx, meta := requestctx.New(req.Context())
	req = req.WithContext(ctx)
	called := false
	handler := SkipAccessLog(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
	if !meta.SkipAccessLog {
		t.Fatal("expected request meta to skip access log")
	}
	if !shouldSkipAccessLog(meta) {
		t.Fatal("expected access log predicate to use request meta")
	}
	if shouldSkipAccessLog(&requestctx.Meta{}) {
		t.Fatal("expected default request meta to keep access log")
	}
}

// TestBuildAccessErrorChain 验证访问日志会优先输出完整错误链，而不是只保留最外层摘要。
func TestBuildAccessErrorChain(t *testing.T) {
	err := errors.Wrap(stderrors.New("pem decode failed"), "初始化RSA签名私钥失败")
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
