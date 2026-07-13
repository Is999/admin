package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AccessLogMiddleware 在请求结束时统一输出访问日志并更新 span 状态。
type AccessLogMiddleware struct{}

// NewAccessLogMiddleware 创建访问日志中间件实例。
func NewAccessLogMiddleware() *AccessLogMiddleware {
	return &AccessLogMiddleware{}
}

// Handle 在请求结束时统一收口访问日志，并把状态、耗时、字节数同步回请求元数据和 span。
func (m *AccessLogMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// statusRecorder 用来捕获实际写出的状态码和响应大小，避免 handler 漏填时日志不完整。
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		begin := time.Now()

		// 使用 defer 做统一收口：正常返回、recover 中间件处理后的 panic 响应，都能落访问日志和 span 状态。
		defer func() {
			ctx := r.Context()
			requestctx.SetLatency(ctx, time.Since(begin))

			meta := requestctx.FromContext(ctx)
			if meta == nil {
				return
			}
			if meta.HTTPStatus == 0 || meta.HTTPStatus == http.StatusOK {
				requestctx.SetResponse(ctx, recorder.status, meta.BizCode, meta.BizMessage, meta.ErrorMessage)
			}
			success := meta.ErrorMessage == "" && recorder.status < http.StatusBadRequest

			// 访问日志统一复用请求元数据字段，保证日志、审计、trace 看到的链路信息一致。
			if !shouldSkipAccessLog(meta) {
				fields := []logx.LogField{
					logx.Field("http_status", recorder.status),
					logx.Field("biz_code", meta.BizCode),
					logx.Field("latency_ms", meta.LatencyMS),
					logx.Field("bytes", recorder.bytes),
					logx.Field("success", success),
				}
				if meta.ErrorMessage != "" {
					fields = append(fields,
						logx.Field("error_message", meta.ErrorMessage),
						logx.Field("error", meta.ErrorMessage),
					)
					if meta.ErrorCause == nil {
						fields = append(fields, logx.Field("error_chain", strings.TrimSpace(meta.ErrorMessage)))
					}
				}
				if meta.ErrorCause != nil {
					fields = append(fields, loggerx.ErrorFields(meta.ErrorCause)...)
				}
				loggerx.Infow(ctx, accessLogMessage(meta, recorder.status, success), fields...)
			}

			if span := trace.SpanFromContext(ctx); span != nil {
				attrs := append(loggerx.TraceAttributesFromMeta(meta),
					attribute.Int64("http.response_content_length", int64(recorder.bytes)),
					attribute.Int64("http.server_duration_ms", meta.LatencyMS),
					attribute.Int64("app.response_bytes", int64(recorder.bytes)),
					attribute.Bool("app.success", success),
				)
				span.SetAttributes(attrs...)
				if meta.ErrorMessage != "" || recorder.status >= http.StatusBadRequest {
					errMsg := meta.ErrorMessage
					if errMsg == "" {
						errMsg = http.StatusText(recorder.status)
					}
					span.SetStatus(otelcodes.Error, errMsg)
					if meta.ErrorCause != nil {
						span.RecordError(meta.ErrorCause)
					} else {
						span.RecordError(errors.New(errMsg))
					}
				} else {
					span.SetStatus(otelcodes.Ok, "ok")
				}
			}
		}()
		next(recorder, r)
	}
}

// accessLogMessage 构造访问日志正文，方便列表只展示 message 时直接看到路径和结果。
func accessLogMessage(meta *requestctx.Meta, httpStatus int, success bool) string {
	parts := []string{"请求 访问日志"}
	if meta == nil {
		return strings.Join(parts, " ")
	}
	parts = appendAccessTextKV(parts, "method", meta.Method)
	parts = appendAccessTextKV(parts, "path", meta.Path)
	parts = appendAccessTextKV(parts, "route", meta.Route)
	if httpStatus > 0 {
		parts = append(parts, fmt.Sprintf("http_status=%d", httpStatus))
	}
	if meta.BizCode > 0 {
		parts = append(parts, fmt.Sprintf("biz_code=%d", meta.BizCode))
	}
	if meta.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("latency_ms=%d", meta.LatencyMS))
	}
	parts = append(parts, fmt.Sprintf("success=%t", success))
	if meta.UserID > 0 {
		parts = append(parts, fmt.Sprintf("user_id=%d", meta.UserID))
	}
	parts = appendAccessTextKV(parts, "ip", meta.ClientIP)
	return strings.Join(parts, " ")
}

// appendAccessTextKV 向访问日志正文追加非空键值片段。
func appendAccessTextKV(parts []string, key string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, fmt.Sprintf("%s=%s", key, value))
}

// SkipAccessLog 标记当前 handler 不输出普通访问日志，适用于健康探针和高频轮询接口。
func SkipAccessLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestctx.SetSkipAccessLog(r.Context(), true)
		next(w, r)
	}
}

// shouldSkipAccessLog 判断当前请求是否按路由注册规则跳过普通访问日志。
func shouldSkipAccessLog(meta *requestctx.Meta) bool {
	return meta != nil && meta.SkipAccessLog
}

// statusRecorder 是对 http.ResponseWriter 的轻量包装，用于采集真实响应状态与字节数。
type statusRecorder struct {
	http.ResponseWriter      // 原始响应写入器
	status              int  // 实际写出的 HTTP 状态码
	bytes               int  // 实际写出的响应字节数
	wroteHeader         bool // 是否已经写出首个响应状态
}

// WriteHeader 记录状态码后再透传到底层响应写入器。
func (w *statusRecorder) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Write 记录响应体大小，并在未显式写状态码时补默认 200。
func (w *statusRecorder) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, errors.Tag(err)
}

// Unwrap 返回底层响应写入器，供 http.ResponseController 按需透传扩展能力。
func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
