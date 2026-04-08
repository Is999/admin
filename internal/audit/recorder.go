package audit

import (
	"context"
	"fmt"
	"time"

	"admin_cron/helper"
	"admin_cron/internal/model"
	"admin_cron/internal/requestctx"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// Event 描述一次待写入 admin_log 的审计事件，显式字段会优先覆盖请求元数据中的同名信息。
type Event struct {
	Action       model.AdminLogAction // 审计动作
	Route        string               // 路由别名
	Method       string               // 处理方法标识
	Describe     string               // 业务描述
	Data         any                  // 审计负载
	UserID       int                  // 操作人 ID
	UserName     string               // 操作人名称
	IP           string               // 操作人 IP
	Success      *bool                // 是否成功（nil 表示自动推断）
	HTTPStatus   int                  // HTTP 状态码
	BizCode      int                  // 业务码
	ErrorMessage string               // 错误信息
	TraceID      string               // Trace ID
	SpanID       string               // Span ID
}

// Recorder 负责把请求上下文中的链路元数据与显式事件字段合并后落到 admin_log。
type Recorder struct {
	db              *gorm.DB // 数据库连接实例，用于将审计日志持久化
	logBodyMaxBytes int      // 审计日志请求体的最大截断长度，防止超大请求拖垮数据库
}

// NewRecorder 创建审计记录器，同时约束审计详情最大长度，避免大对象或敏感信息失控落库。
func NewRecorder(db *gorm.DB, logBodyMaxBytes int) *Recorder {
	if logBodyMaxBytes <= 0 {
		logBodyMaxBytes = 8192
	}
	return &Recorder{
		db:              db,
		logBodyMaxBytes: logBodyMaxBytes,
	}
}

// Record 是统一审计入口，所有登录、登出、CRUD 操作都应通过这里写入审计表。
func (r *Recorder) Record(ctx context.Context, event Event) error {
	if r == nil || r.db == nil {
		return nil
	}

	// 审计日志属于强一致写链路，固定写入主库，避免主从延迟影响后台追踪查询。
	return model.CreateAdminLog(r.db.WithContext(ctx).Clauses(dbresolver.Write), new(r.buildLogEntry(ctx, event)))
}

// buildLogEntry 以显式事件字段优先，请求元数据兜底，确保即使业务层只传最少信息也能串起链路。
func (r *Recorder) buildLogEntry(ctx context.Context, event Event) model.AdminLog {
	meta := requestctx.FromContext(ctx)
	success := event.Success != nil && *event.Success
	if event.Success == nil {
		success = inferSuccess(meta, event)
	}

	return model.AdminLog{
		UserID:       firstPositive(event.UserID, userIDFromMeta(meta)),
		UserName:     helper.FirstNonEmptyString(event.UserName, userNameFromMeta(meta)),
		Action:       string(event.Action),
		Route:        helper.FirstNonEmptyString(event.Route, routeFromMeta(meta)),
		Method:       event.Method,
		Describe:     event.Describe,
		Data:         Serialize(event.Data, r.logBodyMaxBytes),
		IP:           helper.FirstNonEmptyString(event.IP, ipFromMeta(meta)),
		Ipaddr:       "",
		TraceID:      helper.FirstNonEmptyString(event.TraceID, traceIDFromMeta(meta)),
		SpanID:       helper.FirstNonEmptyString(event.SpanID, spanIDFromMeta(meta)),
		HTTPStatus:   firstPositive(event.HTTPStatus, httpStatusFromMeta(meta)),
		BizCode:      firstPositive(event.BizCode, bizCodeFromMeta(meta)),
		LatencyMS:    latencyFromMeta(meta),
		Success:      success,
		ErrorMessage: helper.FirstNonEmptyString(event.ErrorMessage, errorMessageFromMeta(meta)),
		CreatedAt:    time.Now(),
	}
}

// inferSuccess 在业务未显式声明成功状态时，根据请求结果自动推断审计成功与否。
func inferSuccess(meta *requestctx.Meta, event Event) bool {
	if event.ErrorMessage != "" {
		return false
	}
	if meta == nil {
		return true
	}
	if meta.ErrorMessage != "" {
		return false
	}
	if meta.HTTPStatus >= 400 {
		return false
	}
	return true
}

// firstPositive 返回第一个大于 0 的整数，用于优先使用显式事件字段。
func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// userIDFromMeta 从请求元数据读取用户 ID。
func userIDFromMeta(meta *requestctx.Meta) int {
	if meta == nil {
		return 0
	}
	return meta.UserID
}

// userNameFromMeta 从请求元数据读取用户名。
func userNameFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.UserName
}

// routeFromMeta 从请求元数据读取路由别名。
func routeFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.Route
}

// ipFromMeta 从请求元数据读取客户端 IP。
func ipFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.ClientIP
}

// traceIDFromMeta 从请求元数据读取 trace_id。
func traceIDFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.TraceID
}

// spanIDFromMeta 从请求元数据读取 span_id。
func spanIDFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.SpanID
}

// httpStatusFromMeta 从请求元数据读取 HTTP 状态码。
func httpStatusFromMeta(meta *requestctx.Meta) int {
	if meta == nil {
		return 0
	}
	return meta.HTTPStatus
}

// bizCodeFromMeta 从请求元数据读取业务码。
func bizCodeFromMeta(meta *requestctx.Meta) int {
	if meta == nil {
		return 0
	}
	return meta.BizCode
}

// latencyFromMeta 从请求元数据读取请求耗时（毫秒）。
func latencyFromMeta(meta *requestctx.Meta) int64 {
	if meta == nil {
		return 0
	}
	return meta.LatencyMS
}

// errorMessageFromMeta 从请求元数据读取错误信息。
func errorMessageFromMeta(meta *requestctx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.ErrorMessage
}

// String 返回便于日志打印的人类可读事件摘要。
func (e Event) String() string {
	return fmt.Sprintf("%s %s", e.Route, e.Method)
}
