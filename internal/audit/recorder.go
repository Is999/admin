package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"admin/helper"
	"admin/internal/model"
	"admin/internal/requestctx"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	adminLogEventIDPrefix      = "admin_log:audit:" // 审计日志 Collector 事件 ID 前缀
	adminLogEventIDHashBytes   = 16                 // 事件 ID 哈希字节数，控制总长小于 Collector 64 字符限制
	defaultAuditLogBodyMaxSize = 8192               // 审计详情默认最大字节数
)

// Event 描述一次待写入 admin_log 的审计事件，显式字段会优先覆盖请求元数据中的同名信息。
type Event struct {
	EventID      string               // 事件幂等 ID；为空时由 recorder 生成
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

// AdminLogEnqueuer 约束审计日志写入 Collector 的最小能力。
type AdminLogEnqueuer interface {
	EnqueueAdminLog(context.Context, string, model.AdminLog) error
}

// Recorder 负责把请求上下文中的链路元数据与显式事件字段合并后落到 admin_log。
type Recorder struct {
	mu              sync.RWMutex     // 保护 Collector 写入器切换
	enqueuer        AdminLogEnqueuer // Collector 投递入口，正常链路不直接写 DB
	logBodyMaxBytes int              // 审计日志请求体的最大截断长度，防止超大请求拖垮数据库
}

// NewRecorder 创建审计记录器，同时约束审计详情最大长度，避免大对象或敏感信息失控落库。
func NewRecorder(_ *gorm.DB, logBodyMaxBytes int) *Recorder {
	if logBodyMaxBytes <= 0 {
		logBodyMaxBytes = defaultAuditLogBodyMaxSize
	}
	return &Recorder{
		logBodyMaxBytes: logBodyMaxBytes,
	}
}

// SetEnqueuer 设置审计日志 Collector 投递入口，启动期由 Collector 组件注入。
func (r *Recorder) SetEnqueuer(enqueuer AdminLogEnqueuer) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueuer = enqueuer
}

// currentEnqueuer 返回当前 Collector 投递入口。
func (r *Recorder) currentEnqueuer() AdminLogEnqueuer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enqueuer
}

// Record 是统一审计入口，所有登录、登出、CRUD 操作都应通过这里写入审计表。
func (r *Recorder) Record(ctx context.Context, event Event) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	enqueuer := r.currentEnqueuer()
	if enqueuer == nil {
		return errors.Errorf("审计日志 Collector 未初始化")
	}

	entry := r.buildLogEntry(ctx, event)
	eventID := helper.FirstNonEmptyString(event.EventID, buildAdminLogEventID(entry, entry.CreatedAt))
	return enqueuer.EnqueueAdminLog(ctx, eventID, entry)
}

// buildLogEntry 以显式事件字段优先，请求元数据兜底，确保即使业务层只传最少信息也能串起链路。
func (r *Recorder) buildLogEntry(ctx context.Context, event Event) model.AdminLog {
	meta := requestctx.FromContext(ctx)
	success := event.Success != nil && *event.Success
	if event.Success == nil {
		success = inferSuccess(meta, event)
	}
	now := time.Now()

	entry := model.AdminLog{
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
		CreatedAt:    now,
	}
	return entry
}

// buildAdminLogEventID 基于本次审计快照生成稳定长度的幂等 ID，供开启幂等的 Collector 任务去重。
func buildAdminLogEventID(entry model.AdminLog, at time.Time) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strconv.Itoa(entry.UserID),
		entry.UserName,
		entry.Action,
		entry.Route,
		entry.Method,
		entry.TraceID,
		entry.SpanID,
		entry.IP,
		strconv.FormatInt(entry.LatencyMS, 10),
		at.Format(time.RFC3339Nano),
	}, "|")))
	return adminLogEventIDPrefix + hex.EncodeToString(sum[:adminLogEventIDHashBytes])
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
