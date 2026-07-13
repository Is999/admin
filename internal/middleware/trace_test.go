package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"admin/internal/config"
	otelsetup "admin/internal/infra/tracing"
	"admin/internal/requestctx"
)

// TestTraceMiddlewareGeneratesTraceID 验证请求未携带链路信息时，会自动生成 trace/span 并回写响应头。
func TestTraceMiddlewareGeneratesTraceID(t *testing.T) {
	shutdown, err := otelsetup.Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
	})
	if err != nil {
		t.Fatalf("setup tracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	var meta *requestctx.Meta
	handler := NewTraceMiddleware(nil).Handle(func(w http.ResponseWriter, r *http.Request) {
		meta = requestctx.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	handler(rr, req)

	if rr.Header().Get(requestctx.HeaderTraceID) == "" {
		t.Fatalf("expected %s header", requestctx.HeaderTraceID)
	}
	if rr.Header().Get(requestctx.HeaderSpanID) == "" {
		t.Fatalf("expected %s header", requestctx.HeaderSpanID)
	}
	if meta == nil {
		t.Fatalf("expected request meta")
	}
	if meta.TraceID == "" || meta.SpanID == "" {
		t.Fatalf("expected trace/span id in request meta, got %+v", meta)
	}
	if got := rr.Header().Get(requestctx.HeaderTraceID); got != meta.TraceID {
		t.Fatalf("expected response trace id %q to equal meta trace id %q", got, meta.TraceID)
	}
	if got := rr.Header().Get(requestctx.HeaderSpanID); got != meta.SpanID {
		t.Fatalf("expected response span id %q to equal meta span id %q", got, meta.SpanID)
	}
}

// TestTraceMiddlewareUsesTraceparent 验证优先继承标准 traceparent，而不是重新生成新的 trace_id。
func TestTraceMiddlewareUsesTraceparent(t *testing.T) {
	shutdown, err := otelsetup.Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
	})
	if err != nil {
		t.Fatalf("setup tracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	handler := NewTraceMiddleware(nil).Handle(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	handler(rr, req)

	if got := rr.Header().Get(requestctx.HeaderTraceID); got != traceID {
		t.Fatalf("expected inherited trace id %q, got %q", traceID, got)
	}
}

// TestTraceMiddlewareUsesXTraceID 验证前端 UUID 格式 X-Trace-Id 会被转换成标准 trace_id。
func TestTraceMiddlewareUsesXTraceID(t *testing.T) {
	shutdown, err := otelsetup.Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
	})
	if err != nil {
		t.Fatalf("setup tracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	const headerTraceID = "4bf92f35-77b3-4da6-a3ce-929d0e0e4736"
	const normalizedTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	handler := NewTraceMiddleware(nil).Handle(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(requestctx.HeaderTraceID, headerTraceID)
	handler(rr, req)

	if got := rr.Header().Get(requestctx.HeaderTraceID); got != normalizedTraceID {
		t.Fatalf("expected inherited x-trace-id %q, got %q", normalizedTraceID, got)
	}
}
