package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	i18n "admin_cron/common/i18n"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/requestctx"

	"github.com/Is999/go-utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceMiddleware 在 HTTP 请求入口创建或继承链路上下文，并把 trace 信息同步进 request meta。
type TraceMiddleware struct {
	tracer trace.Tracer // HTTP 入口 span 使用的 tracer 实例
}

// NewTraceMiddleware 创建服务端 span 中间件，统一承接请求入口的 trace 初始化。
func NewTraceMiddleware() *TraceMiddleware {
	return &TraceMiddleware{
		tracer: otel.Tracer("admin_cron/http"),
	}
}

// Handle 基于标准 W3C tracecontext 继承链路，并兼容前端 X-Trace-Id。
func (m *TraceMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
		requestctx.SetLocale(ctx, i18n.NormalizeLocale(r.Header.Get("Accept-Language")))
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
		ctx = inheritTraceIDFromHeader(ctx, r)

		ctx, span := m.tracer.Start(ctx, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		sc := span.SpanContext()
		requestctx.SetTrace(ctx, sc.TraceID().String(), sc.SpanID().String())
		span.SetAttributes(loggerx.TraceAttributesFromMeta(requestctx.FromContext(ctx))...)
		ctx = loggerx.BindContext(ctx)

		w.Header().Set(requestctx.HeaderTraceID, sc.TraceID().String())
		w.Header().Set(requestctx.HeaderSpanID, sc.SpanID().String())
		next(w, r.WithContext(ctx))
		syncSpanWithMeta(span, requestctx.FromContext(ctx))
	}
}

// inheritTraceIDFromHeader 在没有标准 traceparent 时，把 X-Trace-Id 转换为远端父级 trace。
// 前端历史上发送 UUID 格式的 X-Trace-Id，这里会去掉横线后映射为 OTel 需要的 32 位十六进制 trace_id。
func inheritTraceIDFromHeader(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	if trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	if strings.TrimSpace(r.Header.Get(requestctx.HeaderTraceParent)) != "" {
		return ctx
	}
	traceID, ok := parseHeaderTraceID(r.Header.Get(requestctx.HeaderTraceID))
	if !ok {
		return ctx
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     newParentSpanID(),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	if !parent.IsValid() {
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, parent)
}

// parseHeaderTraceID 解析 X-Trace-Id，支持 UUID 与 32 位十六进制两种格式。
func parseHeaderTraceID(raw string) (trace.TraceID, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", ""))
	if len(normalized) != 32 {
		return trace.TraceID{}, false
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return trace.TraceID{}, false
	}
	traceID, err := trace.TraceIDFromHex(normalized)
	if err != nil || !traceID.IsValid() {
		return trace.TraceID{}, false
	}
	return traceID, true
}

// newParentSpanID 创建一个临时远端父 span_id，用于把 X-Trace-Id 接入标准 trace 树。
func newParentSpanID() trace.SpanID {
	var spanID trace.SpanID
	if _, err := rand.Read(spanID[:]); err != nil || !spanID.IsValid() {
		return trace.SpanID{1}
	}
	return spanID
}

// syncSpanWithMeta 在请求处理完成后，把路由别名、用户、业务码等补充进 span。
// 这样 trace 不再只看到 HTTP 路径，还能直接对齐到系统内部的业务路由与操作者。
func syncSpanWithMeta(span trace.Span, meta *requestctx.Meta) {
	if span == nil || meta == nil {
		return
	}

	route := strings.TrimSpace(meta.Route)
	if route == "" {
		route = strings.TrimSpace(meta.Path)
	}
	if route != "" && strings.TrimSpace(meta.Method) != "" {
		span.SetName(meta.Method + " " + route)
	}
	span.SetAttributes(loggerx.TraceAttributesFromMeta(meta)...)
}
