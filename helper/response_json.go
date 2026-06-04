package helper

import (
	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/requestctx"
	"context"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/rest/httpx"
)

const (
	RespCodeUndefined = 0 // 未定义状态
	RespCodeSuccess   = 1 // 成功
	RespCodeFail      = 2 // 失败
)

// ResponseJSON 标准响应JSON结构
type ResponseJSON struct {
	Status  bool   `json:"status"`            // 状态: true 成功, false 失败
	Code    int    `json:"code"`              // 业务状态码，按 common/codes 分段定义
	Message string `json:"message,omitempty"` // 响应消息，按请求语言解析后的最终文案
	Data    any    `json:"data,omitempty"`    // 响应数据，成功或失败详情由业务方按场景传入
	TraceID string `json:"traceId,omitempty"` // 链路追踪 ID，便于前端错误提示与日志检索对齐
	SpanID  string `json:"spanId,omitempty"`  // 当前服务 span ID，便于定位服务内处理片段
}

// JSONResp 用链式写法封装统一响应，并在写出响应前把 HTTP/Biz 结果同步回请求元数据。
type JSONResp struct {
	ctx        context.Context     // 请求上下文，用于回填响应元数据
	write      http.ResponseWriter // HTTP 响应写入器
	httpStatus *int                // 显式 HTTP 状态码
	code       *int                // 显式业务码
	message    *string             // 显式响应文案
	err        error               // 内部错误对象，仅用于日志、审计和 trace
}

// NewJSONResp 创建响应写入器，ctx 用于回填 request meta 和复用 go-zero 的上下文日志能力。
func NewJSONResp(ctx context.Context, w http.ResponseWriter) *JSONResp {
	return &JSONResp{
		ctx:   ctx,
		write: w,
	}
}

// SetHTTPStatus 设置本次响应的 HTTP 状态码。
func (r *JSONResp) SetHTTPStatus(status int) *JSONResp {
	r.httpStatus = &status
	return r
}

// SetCode 设置业务状态码，默认会被写入标准响应体中的 code 字段。
func (r *JSONResp) SetCode(code int) *JSONResp {
	r.code = &code
	return r
}

// SetMessage 设置响应消息，可传完整多语言 key；如果不是已登记 key，则视为已解析的最终展示文案。
func (r *JSONResp) SetMessage(message string) *JSONResp {
	r.message = &message
	return r
}

// SetError 设置仅供内部日志、审计和 trace 使用的错误对象，不会直接返回给前端。
func (r *JSONResp) SetError(err error) *JSONResp {
	r.err = err
	return r
}

// Success 构造成功响应，并把请求结果同步写入 request meta 供 access log 和审计复用。
func (r *JSONResp) Success(data any) {
	locale := responseLocale(r.ctx)
	code := RespCodeSuccess
	if r.code != nil {
		code = *r.code
	}
	message := i18n.MessageByCode(code, locale)
	if r.message != nil {
		message = i18n.MessageByKey(*r.message, locale)
	}
	requestctx.SetErrorResponse(r.ctx, http.StatusOK, code, message, nil, "")

	response := ResponseJSON{
		Status:  true,
		Code:    code,
		Message: message,
		Data:    data,
	}
	attachTraceToResponse(r.ctx, &response)
	httpx.WriteJsonCtx(r.ctx, r.write, http.StatusOK, response)
}

// Fail 构造失败响应，并同步 request meta 供日志与 trace 使用。
func (r *JSONResp) Fail(message string, data ...any) {
	locale := responseLocale(r.ctx)
	code := RespCodeFail
	if r.code != nil {
		code = *r.code
	}
	if message == "" {
		message = i18n.MessageByCode(code, locale)
	} else {
		message = i18n.MessageByKey(message, locale)
	}
	response := ResponseJSON{
		Status:  false,
		Code:    code,
		Message: message,
		Data: func() any {
			if len(data) > 0 {
				return data[0]
			}
			return nil
		}(),
	}
	attachTraceToResponse(r.ctx, &response)
	httpStatus := codes.HTTPStatus(code)
	if r.httpStatus != nil {
		httpStatus = *r.httpStatus
	}
	requestctx.SetErrorResponse(r.ctx, httpStatus, code, message, r.err, internalErrorSummary(r.err))
	httpx.WriteJsonCtx(r.ctx, r.write, httpStatus, response)
}

// Write 允许业务显式指定成功/失败标志，用于少量非标准分支复用统一响应格式。
func (r *JSONResp) Write(success bool, data ...any) {
	locale := responseLocale(r.ctx)
	code := RespCodeUndefined
	if r.code != nil {
		code = *r.code
	}
	message := i18n.MessageByCode(code, locale)
	if r.message != nil {
		message = i18n.MessageByKey(*r.message, locale)
	}
	response := ResponseJSON{
		Status:  success,
		Code:    code,
		Message: message,
		Data: func() any {
			if len(data) > 0 {
				return data[0]
			}
			return nil
		}(),
	}
	attachTraceToResponse(r.ctx, &response)
	httpStatus := codes.HTTPStatus(code)
	if r.httpStatus != nil {
		httpStatus = *r.httpStatus
	}
	if success {
		requestctx.SetErrorResponse(r.ctx, httpStatus, code, message, nil, "")
	} else {
		requestctx.SetErrorResponse(r.ctx, httpStatus, code, message, r.err, internalErrorSummary(r.err))
	}
	httpx.WriteJsonCtx(r.ctx, r.write, httpStatus, response)
}

// attachTraceToResponse 把 request meta 中的链路字段回填到响应体，便于前端错误提示直接附带 trace_id。
func attachTraceToResponse(ctx context.Context, response *ResponseJSON) {
	if response == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	if meta == nil {
		return
	}
	response.TraceID = strings.TrimSpace(meta.TraceID)
	response.SpanID = strings.TrimSpace(meta.SpanID)
}

// responseLocale 统一读取响应语言，缺省时回退中文，避免响应出口散落默认值判断。
func responseLocale(ctx context.Context) string {
	if meta := requestctx.FromContext(ctx); meta != nil && meta.Locale != "" {
		return meta.Locale
	}
	return i18n.LocaleZHCN
}

// internalErrorSummary 生成仅供内部日志、审计和 trace 使用的错误摘要。
func internalErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
