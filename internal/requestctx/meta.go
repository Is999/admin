package requestctx

import (
	"context"
	"strings"
	"time"
)

const (
	HeaderTraceID     = "X-Trace-Id"  // HeaderTraceID 是前后端约定的请求追踪头，参与签名与防重放。
	HeaderTimestamp   = "X-Timestamp" // HeaderTimestamp 是秒级请求时间戳，参与签名并校验时间窗口。
	HeaderSpanID      = "X-Span-Id"   // HeaderSpanID 是服务端当前处理片段标识，便于一次请求内定位服务节点。
	HeaderTraceParent = "traceparent" // HeaderTraceParent 是 W3C Trace Context 标准头，用于跨服务链路继承。
)

// metaKey 是 request meta 在 context 中的私有 key，避免与其他中间件的 key 冲突。
type metaKey struct{}

// Meta 保存一次请求在应用内传播的链路与审计元数据。
type Meta struct {
	StartedAt     time.Time // 请求元数据创建时间，用于审计日志在 access log defer 前计算当前真实耗时
	TraceID       string    // 链路追踪 ID（trace_id），用于串联整条请求链路
	SpanID        string    // 当前服务内处理片段 ID（span_id），用于定位本服务节点
	Route         string    // 统一路由别名（如 user_tag.workflow.trigger），用于权限/审计/日志对齐
	Method        string    // HTTP 方法（GET/POST/PUT/DELETE 等）
	Path          string    // HTTP 请求路径（URL Path）
	ClientIP      string    // 客户端 IP（优先真实来源 IP）
	Locale        string    // 请求语言（如 zh-CN/en-US），用于响应多语言翻译
	UserID        int       // 当前操作人 ID（登录后写入）
	UserName      string    // 当前操作人名称（登录后写入）
	AccessToken   string    // 当前请求携带的访问令牌（仅在本次请求上下文中传播）
	HTTPStatus    int       // HTTP 状态码（统一响应出口回填）
	BizCode       int       // 业务状态码（common/codes 中定义）
	BizMessage    string    // 业务响应文案（已按 locale 解析后的最终文案）
	LatencyMS     int64     // 请求总耗时（毫秒）
	ErrorMessage  string    // 错误信息（失败时写入，供日志与审计检索）
	ErrorCause    error     // 原始错误对象（失败时写入，供 trace 和内部日志复用）
	SkipAccessLog bool      // 是否跳过普通访问日志，供健康探针和高频轮询接口降噪
	TaskID        string    // 当前异步任务 ID（Asynq task id）
	TaskType      string    // 当前异步任务类型（如 workflow:trigger）
	TaskName      string    // 当前异步任务名称/脚本名称，优先用于日志和管理后台识别
	TaskQueue     string    // 当前异步任务所属队列
	WorkflowID    string    // 当前工作流实例 ID，用于串联多个 DAG 节点
	WorkflowName  string    // 当前工作流名称（如 cache.refresh）
	WorkflowNode  string    // 当前工作流节点名称（如 refresh.batch）
	Mode          string    // 当前业务运行模式（如 full/delta/targeted），用于任务链路检索
	ShardIndex    int       // 当前工作流分片索引，从 0 开始
	ShardTotal    int       // 当前工作流总分片数
}

// New 为请求创建统一元数据容器，后续中间件、logic、审计都围绕这一份状态读写。
func New(ctx context.Context) (context.Context, *Meta) {
	if meta := FromContext(ctx); meta != nil {
		if meta.StartedAt.IsZero() {
			meta.StartedAt = time.Now()
		}
		return ctx, meta
	}

	meta := &Meta{
		HTTPStatus: 200,
		StartedAt:  time.Now(),
	}
	return context.WithValue(ctx, metaKey{}, meta), meta
}

// WithMeta 主要用于测试或需要替换整份请求元数据的场景。
func WithMeta(ctx context.Context, meta *Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, meta)
}

// FromContext 读取当前请求元数据，不鼓励在业务层继续散落使用自定义 context key。
func FromContext(ctx context.Context) *Meta {
	if ctx == nil {
		return nil
	}
	if meta, ok := ctx.Value(metaKey{}).(*Meta); ok {
		return meta
	}
	return nil
}

// SetTrace 记录当前请求最终采用的 trace/span，用于日志、审计和响应头回传。
func SetTrace(ctx context.Context, traceID, spanID string) {
	if meta := FromContext(ctx); meta != nil {
		meta.TraceID = strings.TrimSpace(traceID)
		meta.SpanID = strings.TrimSpace(spanID)
	}
}

// SetRoute 写入统一路由别名，避免 access log、审计日志、权限判断各自维护一套路由名称。
func SetRoute(ctx context.Context, route string) {
	if meta := FromContext(ctx); meta != nil && route != "" {
		meta.Route = route
	}
}

// SetSkipAccessLog 设置当前请求是否跳过普通访问日志。
func SetSkipAccessLog(ctx context.Context, skip bool) {
	if meta := FromContext(ctx); meta != nil {
		meta.SkipAccessLog = skip
	}
}

// SetRequest 在请求入口补齐基础 HTTP 信息，便于后续链路打点统一取值。
func SetRequest(ctx context.Context, method, path, clientIP string) {
	if meta := FromContext(ctx); meta != nil {
		if method != "" {
			meta.Method = method
		}
		if path != "" {
			meta.Path = path
		}
		if clientIP != "" {
			meta.ClientIP = clientIP
		}
	}
}

// SetLocale 设置请求语言，供响应消息做多语言翻译。
func SetLocale(ctx context.Context, locale string) {
	if meta := FromContext(ctx); meta != nil && locale != "" {
		meta.Locale = locale
	}
}

// SetUser 由鉴权中间件或登录流程补充操作者信息，供业务日志和审计复用。
func SetUser(ctx context.Context, userID int, userName, clientIP string) {
	if meta := FromContext(ctx); meta != nil {
		if userID > 0 {
			meta.UserID = userID
		}
		if userName != "" {
			meta.UserName = userName
		}
		if clientIP != "" {
			meta.ClientIP = clientIP
		}
	}
}

// SetAccessToken 仅在当前请求链路内保存 token，避免业务层再从 header 反复解析。
func SetAccessToken(ctx context.Context, token string) {
	if meta := FromContext(ctx); meta != nil && token != "" {
		meta.AccessToken = token
	}
}

// SetResponse 在统一响应出口回填 HTTP/Biz 结果，保证 access log 与审计看到的是同一份结果。
func SetResponse(ctx context.Context, httpStatus, bizCode int, bizMessage, errorMessage string) {
	if meta := FromContext(ctx); meta != nil {
		if httpStatus > 0 {
			meta.HTTPStatus = httpStatus
		}
		meta.BizCode = bizCode
		meta.BizMessage = bizMessage
		meta.ErrorMessage = errorMessage
	}
}

// SetErrorResponse 在同一入口同时写入响应结果和内部错误对象，避免调用方把两者拆开后出现链路不一致。
func SetErrorResponse(ctx context.Context, httpStatus, bizCode int, bizMessage string, err error, fallback string) {
	SetResponse(ctx, httpStatus, bizCode, bizMessage, strings.TrimSpace(fallback))
	SetError(ctx, err, fallback)
}

// SetError 在请求上下文中写入内部错误对象及其内部摘要。
// fallback 只允许传入“内部错误摘要”，禁止把前端业务提示文案直接复用到这里。
func SetError(ctx context.Context, err error, fallback string) {
	if meta := FromContext(ctx); meta != nil {
		meta.ErrorCause = err
		if err != nil {
			meta.ErrorMessage = strings.TrimSpace(fallback)
			return
		}
		meta.ErrorMessage = strings.TrimSpace(fallback)
	}
}

// SetLatency 在请求结束时记录总耗时，供 access log、审计日志和 span 状态复用。
func SetLatency(ctx context.Context, d time.Duration) {
	if meta := FromContext(ctx); meta != nil {
		meta.LatencyMS = durationMilliseconds(d)
	}
}

// RefreshLatency 按请求创建时间刷新当前耗时，供 access log defer 执行前的审计落库使用。
func RefreshLatency(ctx context.Context) {
	if meta := FromContext(ctx); meta != nil && !meta.StartedAt.IsZero() {
		meta.LatencyMS = durationMilliseconds(time.Since(meta.StartedAt))
	}
}

// durationMilliseconds 把耗时转换成毫秒；非零亚毫秒请求按 1ms 记录，避免后台展示已执行请求耗时为 0。
func durationMilliseconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	milliseconds := d.Milliseconds()
	if milliseconds <= 0 {
		return 1
	}
	return milliseconds
}

// SetTask 写入异步任务基础信息，供 worker 日志、trace 和审计统一取值。
func SetTask(ctx context.Context, taskID, taskType, queue string) {
	if meta := FromContext(ctx); meta != nil {
		if taskID != "" {
			meta.TaskID = taskID
		}
		if taskType != "" {
			meta.TaskType = taskType
		}
		if queue != "" {
			meta.TaskQueue = queue
		}
	}
}

// SetTaskName 写入异步任务名称，供 worker 日志和任务管理页识别“当前跑的是哪个脚本”。
func SetTaskName(ctx context.Context, taskName string) {
	if meta := FromContext(ctx); meta != nil && strings.TrimSpace(taskName) != "" {
		meta.TaskName = strings.TrimSpace(taskName)
	}
}

// SetWorkflow 写入当前 DAG 工作流上下文，方便按 workflow_id / node / shard 检索日志。
func SetWorkflow(ctx context.Context, workflowID, workflowName, workflowNode string, shardIndex, shardTotal int) {
	if meta := FromContext(ctx); meta != nil {
		if workflowID != "" {
			meta.WorkflowID = workflowID
		}
		if workflowName != "" {
			meta.WorkflowName = workflowName
		}
		if workflowNode != "" {
			meta.WorkflowNode = workflowNode
		}
		if shardIndex >= 0 {
			meta.ShardIndex = shardIndex
		}
		if shardTotal > 0 {
			meta.ShardTotal = shardTotal
		}
	}
}

// SetMode 写入当前业务运行模式，便于日志按 mode / node / shard 快速检索。
func SetMode(ctx context.Context, mode string) {
	if meta := FromContext(ctx); meta != nil && strings.TrimSpace(mode) != "" {
		meta.Mode = strings.TrimSpace(mode)
	}
}
