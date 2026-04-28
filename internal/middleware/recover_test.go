package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	codes "admin/common/codes"
	"admin/helper"
	"admin/internal/config"
	otelsetup "admin/internal/infra/tracing"
	"admin/internal/requestctx"
)

// TestRecoverMiddlewareReturnsJSON 验证真实全局链路顺序下，panic 会被内层 recover 兜住，并返回统一 JSON 响应与 trace_id。
func TestRecoverMiddlewareReturnsJSON(t *testing.T) {
	shutdown, err := otelsetup.Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
	})
	if err != nil {
		t.Fatalf("setup tracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	// 中间件顺序保持和 RegisterHandlers 一致：outer recover -> trace -> access log -> inner recover。
	handler := NewRecoverMiddleware().Handle(
		NewTraceMiddleware().Handle(
			NewAccessLogMiddleware().Handle(
				NewRecoverMiddleware().Handle(func(w http.ResponseWriter, r *http.Request) {
					panic("boom")
				}),
			),
		),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	handler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected http 500, got %d", rr.Code)
	}
	if rr.Header().Get(requestctx.HeaderTraceID) == "" {
		t.Fatalf("expected trace header on panic response")
	}

	var resp helper.ResponseJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status {
		t.Fatalf("expected failed response, got success")
	}
	if resp.Code != codes.InternalError {
		t.Fatalf("expected biz code %d, got %d", codes.InternalError, resp.Code)
	}
	if resp.TraceID == "" {
		t.Fatalf("expected trace id in response body")
	}
}
