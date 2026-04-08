package loggerx

import (
	"admin_cron/internal/config"
	"admin_cron/internal/requestctx"
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	jsoniter "github.com/json-iterator/go"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// loggerxCallerSkip 表示从 loggerx 统一封装函数跳到真实业务调用点需要额外跳过的栈层数。
	loggerxCallerSkip = 1

	// fieldTraceID 表示请求级 Trace ID，来源于请求头或请求上下文生成结果，用于串联 API、任务和下游日志。
	fieldTraceID = "trace_id"
	// fieldSpanID 表示当前 Trace Span ID，来源于链路追踪上下文，用于定位单次调用片段。
	fieldSpanID = "span_id"
	// fieldRoute 表示命中的后端路由模板，来源于路由注册元数据，用于按接口维度聚合日志。
	fieldRoute = "route"
	// fieldHTTPMethod 表示 HTTP 请求方法，来源于请求上下文，用于区分同一路径下的不同动作。
	fieldHTTPMethod = "http_method"
	// fieldPath 表示请求原始路径，来源于 HTTP Request，用于排查未命中路由或网关转发问题。
	fieldPath = "path"
	// fieldLocale 表示请求语言环境，来源于 Accept-Language 解析结果，用于定位多语言响应问题。
	fieldLocale = "locale"
	// fieldIP 表示客户端 IP，来源于请求上下文归一化结果，用于安全审计和访问排障。
	fieldIP = "ip"
	// fieldUserID 表示当前登录用户 ID，来源于认证中间件解析结果，用于按操作者检索日志。
	fieldUserID = "user_id"
	// fieldUserName 表示当前登录用户名，来源于认证中间件解析结果，用于辅助审计展示。
	fieldUserName = "user_name"
	// fieldHTTPStatus 表示 HTTP 响应状态码，来源于访问日志收口阶段，用于区分传输层成功与失败。
	fieldHTTPStatus = "http_status"
	// fieldBizCode 表示统一响应业务码，来源于 handler 响应元数据，用于前后端按错误类型联动排障。
	fieldBizCode = "biz_code"
	// fieldBizMessage 表示统一响应业务消息，来源于 handler 响应元数据，用于展示安全裁剪后的错误提示。
	fieldBizMessage = "biz_message"
	// fieldError 表示顶层错误摘要，来源于顶层 handler、worker 或 scheduler，用于 Kibana 快速检索失败事件。
	fieldError = "error"
	// fieldErrorChain 表示完整错误链路，来源于 errors.Wrapf/errors.Tag 结果，用于保留中间层上下文。
	fieldErrorChain = "error_chain"
	// fieldErrorMsg 表示对外错误消息，来源于请求元数据，用于和接口响应中的 message 对齐。
	fieldErrorMsg = "error_message"
	// fieldTaskID 表示任务实例 ID，来源于任务队列上下文，用于串联单个后台任务的生命周期日志。
	fieldTaskID = "task_id"
	// fieldTaskType 表示任务类型，来源于任务注册信息，用于按执行器维度聚合任务日志。
	fieldTaskType = "task_type"
	// fieldTaskName 表示任务名称，来源于任务定义或工作流节点配置，用于排查具体任务入口。
	fieldTaskName = "task_name"
	// fieldTaskQueue 表示任务队列名称，来源于 Asynq 任务上下文，用于观察队列隔离和积压问题。
	fieldTaskQueue = "task_queue"
	// fieldWorkflowID 表示工作流实例 ID，来源于任务负载或任务上下文，用于串联同一次工作流运行。
	fieldWorkflowID = "workflow_id"
	// fieldWorkflowName 表示工作流名称，来源于任务负载或工作流定义，用于区分不同工作流模板。
	fieldWorkflowName = "workflow_name"
	// fieldWorkflowNode 表示工作流节点名称，来源于任务负载，用于定位 DAG 中的具体执行节点。
	fieldWorkflowNode = "workflow_node"
	// fieldMode 表示运行模式或任务模式，来源于请求/任务上下文，用于区分 full、delta 等执行语义。
	fieldMode = "mode"
	// fieldNode 表示统一节点检索字段，来源于 workflow_node 映射或任务上下文，用于兼容按 node 查询的日志面板。
	fieldNode = "node"
	// fieldShard 表示分片摘要，来源于 shard_index/shard_total 组合，用于人眼快速识别当前分片。
	fieldShard = "shard"
	// fieldShardIndex 表示当前分片下标，来源于任务负载，用于精确检索单个分片日志。
	fieldShardIndex = "shard_index"
	// fieldShardTotal 表示分片总数，来源于任务负载，用于判断分片配置是否完整。
	fieldShardTotal = "shard_total"

	// FieldIntervalSeconds 表示轮询、调度或重试间隔秒数。
	FieldIntervalSeconds = "interval_seconds"
	// FieldWindowStartUnix 表示时间窗口起点 Unix 秒。
	FieldWindowStartUnix = "window_start_unix"
	// FieldWindowEndUnix 表示时间窗口终点排他边界 Unix 秒。
	FieldWindowEndUnix = "window_end_unix"
)

// Setup 初始化 go-zero 日志，并在文件输出模式下额外镜像到 stdout 方便容器采集。
func Setup(c config.Config) {
	logx.MustSetup(c.Log)
	if strings.EqualFold(c.Log.Mode, "file") {
		logx.AddWriter(logx.NewWriter(os.Stdout))
	}
	// 显式初始化 go-utils 错误链路追踪配置，保证日志、JSON 与错误行为在启动阶段统一收口。
	errors.SetStackDepth(32)
	errors.SetTraceEnabled(true)
	utils.Configure(
		utils.WithJSON(jsoniter.Marshal, jsoniter.Unmarshal),
		utils.WithLogger(newGoUtilsLogger(nil)),
	)
}

// goUtilsLogger 把 github.com/Is999/go-utils 的结构化日志接口适配到 go-zero logx。
type goUtilsLogger struct {
	fields []any // fields 保存 With 传入的 slog 风格键值对
}

// newGoUtilsLogger 创建 go-utils 日志适配器。
func newGoUtilsLogger(fields []any) *goUtilsLogger {
	copied := make([]any, 0, len(fields))
	copied = append(copied, fields...)
	return &goUtilsLogger{fields: copied}
}

// Debug 输出调试日志。
func (l *goUtilsLogger) Debug(msg string, args ...any) {
	WithCallerSkip(3).Debugw(msg, l.logFields(args...)...)
}

// Info 输出信息日志。
func (l *goUtilsLogger) Info(msg string, args ...any) {
	WithCallerSkip(3).Infow(msg, l.logFields(args...)...)
}

// Warn 输出警告日志。
func (l *goUtilsLogger) Warn(msg string, args ...any) {
	WithCallerSkip(3).Sloww(msg, l.logFields(args...)...)
}

// Error 输出错误日志。
func (l *goUtilsLogger) Error(msg string, args ...any) {
	WithCallerSkip(3).Errorw(msg, l.logFields(args...)...)
}

// With 创建携带固定字段的新日志对象。
func (l *goUtilsLogger) With(args ...any) utils.Logger {
	fields := make([]any, 0, len(l.fields)+len(args))
	fields = append(fields, l.fields...)
	fields = append(fields, args...)
	return newGoUtilsLogger(fields)
}

// Enabled 返回日志级别是否启用；go-zero 当前没有暴露统一级别判断，这里交给 logx 自身过滤。
func (l *goUtilsLogger) Enabled(_ context.Context, _ utils.LogLevel) bool {
	return true
}

// logFields 把 slog 风格键值参数转换成 logx 字段。
func (l *goUtilsLogger) logFields(args ...any) []logx.LogField {
	merged := make([]any, 0, len(l.fields)+len(args))
	merged = append(merged, l.fields...)
	merged = append(merged, args...)
	fields := make([]logx.LogField, 0, (len(merged)+1)/2)
	for i := 0; i < len(merged); i += 2 {
		key, ok := merged[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			key = "field"
		}
		var value any = ""
		if i+1 < len(merged) {
			value = merged[i+1]
		}
		fields = append(fields, logx.Field(key, value))
	}
	return fields
}

// FieldsFromContext 从请求上下文提取统一日志字段，避免业务层重复拼 trace_id、user_id 等公共属性。
func FieldsFromContext(ctx context.Context) []logx.LogField {
	return FieldsFromMeta(requestctx.FromContext(ctx))
}

// FieldsFromMeta 把请求元数据转换成结构化日志字段。
func FieldsFromMeta(meta *requestctx.Meta) []logx.LogField {
	if meta == nil {
		return nil
	}

	fields := make([]logx.LogField, 0, 16)
	if meta.TraceID != "" {
		fields = append(fields, logx.Field(fieldTraceID, meta.TraceID))
	}
	if meta.SpanID != "" {
		fields = append(fields, logx.Field(fieldSpanID, meta.SpanID))
	}
	if meta.Route != "" {
		fields = append(fields, logx.Field(fieldRoute, meta.Route))
	}
	if meta.Method != "" {
		fields = append(fields, logx.Field(fieldHTTPMethod, meta.Method))
	}
	if meta.Path != "" {
		fields = append(fields, logx.Field(fieldPath, meta.Path))
	}
	if meta.Locale != "" {
		fields = append(fields, logx.Field(fieldLocale, meta.Locale))
	}
	if meta.ClientIP != "" {
		fields = append(fields, logx.Field(fieldIP, meta.ClientIP))
	}
	if meta.UserID > 0 {
		fields = append(fields, logx.Field(fieldUserID, meta.UserID))
	}
	if meta.UserName != "" {
		fields = append(fields, logx.Field(fieldUserName, meta.UserName))
	}
	if meta.HTTPStatus > 0 {
		fields = append(fields, logx.Field(fieldHTTPStatus, meta.HTTPStatus))
	}
	if meta.BizCode > 0 {
		fields = append(fields, logx.Field(fieldBizCode, meta.BizCode))
	}
	if meta.BizMessage != "" {
		fields = append(fields, logx.Field(fieldBizMessage, meta.BizMessage))
	}
	if meta.ErrorMessage != "" {
		fields = append(fields, logx.Field(fieldErrorMsg, meta.ErrorMessage))
	}
	if meta.TaskID != "" {
		fields = append(fields, logx.Field(fieldTaskID, meta.TaskID))
	}
	if meta.TaskType != "" {
		fields = append(fields, logx.Field(fieldTaskType, meta.TaskType))
	}
	if meta.TaskName != "" {
		fields = append(fields, logx.Field(fieldTaskName, meta.TaskName))
	}
	if meta.TaskQueue != "" {
		fields = append(fields, logx.Field(fieldTaskQueue, meta.TaskQueue))
	}
	if meta.WorkflowID != "" {
		fields = append(fields, logx.Field(fieldWorkflowID, meta.WorkflowID))
	}
	if meta.WorkflowName != "" {
		fields = append(fields, logx.Field(fieldWorkflowName, meta.WorkflowName))
	}
	if meta.WorkflowNode != "" {
		fields = append(fields,
			logx.Field(fieldWorkflowNode, meta.WorkflowNode),
			logx.Field(fieldNode, meta.WorkflowNode),
		)
	}
	if meta.Mode != "" {
		fields = append(fields, logx.Field(fieldMode, meta.Mode))
	}
	if meta.ShardTotal > 0 {
		shard := strconv.Itoa(meta.ShardIndex) + "/" + strconv.Itoa(meta.ShardTotal)
		fields = append(fields,
			logx.Field(fieldShard, shard),
			logx.Field(fieldShardIndex, meta.ShardIndex),
			logx.Field(fieldShardTotal, meta.ShardTotal),
		)
	}
	return fields
}

// TraceAttributesFromMeta 把请求元数据映射成统一的 trace attributes，保证日志字段与 span 检索维度一致。
func TraceAttributesFromMeta(meta *requestctx.Meta) []attribute.KeyValue {
	if meta == nil {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, 24)
	if meta.TraceID != "" {
		attrs = append(attrs, attribute.String("app."+fieldTraceID, meta.TraceID))
	}
	if meta.SpanID != "" {
		attrs = append(attrs, attribute.String("app."+fieldSpanID, meta.SpanID))
	}
	route := strings.TrimSpace(meta.Route)
	if route == "" {
		route = strings.TrimSpace(meta.Path)
	}
	if route != "" {
		attrs = append(attrs,
			attribute.String("http.route", route),
			attribute.String("app."+fieldRoute, route),
		)
	}
	if meta.Method != "" {
		attrs = append(attrs,
			attribute.String("http.method", meta.Method),
			attribute.String("app."+fieldHTTPMethod, meta.Method),
		)
	}
	if meta.Path != "" {
		attrs = append(attrs,
			attribute.String("url.path", meta.Path),
			attribute.String("app."+fieldPath, meta.Path),
		)
	}
	if meta.Locale != "" {
		attrs = append(attrs, attribute.String("app."+fieldLocale, meta.Locale))
	}
	if meta.ClientIP != "" {
		attrs = append(attrs,
			attribute.String("client.address", meta.ClientIP),
			attribute.String("app.client_ip", meta.ClientIP),
		)
	}
	if meta.UserID > 0 {
		attrs = append(attrs,
			attribute.String("enduser.id", strconv.Itoa(meta.UserID)),
			attribute.Int("app."+fieldUserID, meta.UserID),
		)
	}
	if meta.UserName != "" {
		attrs = append(attrs,
			attribute.String("enduser.name", meta.UserName),
			attribute.String("app."+fieldUserName, meta.UserName),
		)
	}
	if meta.HTTPStatus > 0 {
		attrs = append(attrs,
			attribute.Int("http.status_code", meta.HTTPStatus),
			attribute.Int("app."+fieldHTTPStatus, meta.HTTPStatus),
		)
	}
	if meta.BizCode > 0 {
		attrs = append(attrs, attribute.Int("app."+fieldBizCode, meta.BizCode))
	}
	if meta.BizMessage != "" {
		attrs = append(attrs, attribute.String("app."+fieldBizMessage, meta.BizMessage))
	}
	if meta.ErrorMessage != "" {
		attrs = append(attrs, attribute.String("app."+fieldErrorMsg, meta.ErrorMessage))
	}
	if meta.LatencyMS > 0 {
		attrs = append(attrs, attribute.Int64("app.latency_ms", meta.LatencyMS))
	}
	if meta.TaskID != "" {
		attrs = append(attrs, attribute.String("app."+fieldTaskID, meta.TaskID))
	}
	if meta.TaskType != "" {
		attrs = append(attrs, attribute.String("app."+fieldTaskType, meta.TaskType))
	}
	if meta.TaskName != "" {
		attrs = append(attrs, attribute.String("app."+fieldTaskName, meta.TaskName))
	}
	if meta.TaskQueue != "" {
		attrs = append(attrs, attribute.String("app."+fieldTaskQueue, meta.TaskQueue))
	}
	if meta.WorkflowID != "" {
		attrs = append(attrs, attribute.String("app."+fieldWorkflowID, meta.WorkflowID))
	}
	if meta.WorkflowName != "" {
		attrs = append(attrs, attribute.String("app."+fieldWorkflowName, meta.WorkflowName))
	}
	if meta.WorkflowNode != "" {
		attrs = append(attrs,
			attribute.String("app."+fieldWorkflowNode, meta.WorkflowNode),
			attribute.String("app."+fieldNode, meta.WorkflowNode),
		)
	}
	if meta.Mode != "" {
		attrs = append(attrs, attribute.String("app."+fieldMode, meta.Mode))
	}
	if meta.ShardTotal > 0 {
		shard := strconv.Itoa(meta.ShardIndex) + "/" + strconv.Itoa(meta.ShardTotal)
		attrs = append(attrs,
			attribute.String("app."+fieldShard, shard),
			attribute.Int("app."+fieldShardIndex, meta.ShardIndex),
			attribute.Int("app."+fieldShardTotal, meta.ShardTotal),
		)
	}
	return attrs
}

// ErrorChain 把错误链渲染为单行 JSON 字符串，便于日志完整保留 wrap 链与 trace 信息。
func ErrorChain(err error) string {
	if err == nil {
		return ""
	}
	traceJSON := strings.TrimSpace(errors.TraceJSON(err))
	if traceJSON != "" {
		return traceJSON
	}
	return strings.TrimSpace(err.Error())
}

// ErrorFields 返回统一错误日志字段，确保所有错误日志同时输出错误信息和错误追踪链路。
func ErrorFields(err error) []logx.LogField {
	if err == nil {
		return nil
	}
	return []logx.LogField{
		logx.Field(fieldError, strings.TrimSpace(err.Error())),
		logx.Field(fieldErrorChain, ErrorChain(err)),
	}
}

// ErrorTextFields 返回纯文本错误对应的统一日志字段，适用于尚未封装成 error 对象的失败原因。
func ErrorTextFields(message string) []logx.LogField {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return []logx.LogField{
		logx.Field(fieldError, message),
		logx.Field(fieldErrorChain, message),
	}
}

// Errorw 统一输出带错误链路的错误日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Errorw(ctx context.Context, msg string, err error, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorFields(err)...)
	if ctx == nil {
		WithCallerSkip(loggerxCallerSkip).Errorw(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), loggerxCallerSkip).Errorw(msg, fields...)
}

// ErrorTextw 统一输出只有错误文本的错误日志，并自动补齐 error 与 error_chain 字段。
func ErrorTextw(ctx context.Context, msg string, errorText string, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorTextFields(errorText)...)
	if ctx == nil {
		WithCallerSkip(loggerxCallerSkip).Errorw(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), loggerxCallerSkip).Errorw(msg, fields...)
}

// ErrorwSkip 统一输出带 caller skip 的错误日志，适用于 GORM、适配器等封装层。
func ErrorwSkip(ctx context.Context, skip int, msg string, err error, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorFields(err)...)
	if ctx == nil {
		WithCallerSkip(normalizeCallerSkip(skip)).Errorw(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), normalizeCallerSkip(skip)).Errorw(msg, fields...)
}

// ErrorTextwSkip 统一输出带 caller skip 的文本错误日志，适用于尚未构造成 error 对象的场景。
func ErrorTextwSkip(ctx context.Context, skip int, msg string, errorText string, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorTextFields(errorText)...)
	if ctx == nil {
		WithCallerSkip(normalizeCallerSkip(skip)).Errorw(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), normalizeCallerSkip(skip)).Errorw(msg, fields...)
}

// Infow 统一输出信息日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Infow(ctx context.Context, msg string, fields ...logx.LogField) {
	if ctx == nil {
		WithCallerSkip(loggerxCallerSkip).Infow(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), loggerxCallerSkip).Infow(msg, fields...)
}

// Debugw 统一输出调试日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Debugw(ctx context.Context, msg string, fields ...logx.LogField) {
	if ctx == nil {
		WithCallerSkip(loggerxCallerSkip).Debugw(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), loggerxCallerSkip).Debugw(msg, fields...)
}

// Sloww 统一输出慢操作日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Sloww(ctx context.Context, msg string, fields ...logx.LogField) {
	if ctx == nil {
		WithCallerSkip(loggerxCallerSkip).Sloww(msg, fields...)
		return
	}
	WithContextCallerSkip(BindContext(ctx), loggerxCallerSkip).Sloww(msg, fields...)
}

// appendLogFields 统一复制并追加日志字段，避免调用方直接复用底层切片导致字段串写。
func appendLogFields(base []logx.LogField, extra ...logx.LogField) []logx.LogField {
	merged := make([]logx.LogField, 0, len(base)+len(extra))
	merged = append(merged, base...)
	merged = append(merged, extra...)
	return merged
}

// normalizeCallerSkip 归一化调用方传入的额外 skip，并补上 loggerx 自身封装层。
func normalizeCallerSkip(skip int) int {
	if skip < 0 {
		skip = 0
	}
	return loggerxCallerSkip + skip
}

// BindContext 将当前请求字段绑定进 logx context，后续 logx.WithContext(ctx) 会自动带上这些字段。
func BindContext(ctx context.Context) context.Context {
	fields := FieldsFromContext(ctx)
	if len(fields) == 0 {
		return ctx
	}
	return logx.ContextWithFields(ctx, fields...)
}

// WithCallerSkip 返回带 caller skip 的 logger，用于日志适配层把 caller 还原到真实触发位置。
// skip 表示在当前封装链路上需要额外跳过的层数；不传上下文时仅修正 caller，不附带请求字段。
func WithCallerSkip(skip int) logx.Logger {
	return logx.WithCallerSkip(skip)
}

// WithContextCallerSkip 返回同时带上下文和 caller skip 的 logger。
// 适用于 GORM、Asynq 等二次封装场景，避免 caller 固定落在适配层文件。
func WithContextCallerSkip(ctx context.Context, skip int) logx.Logger {
	return logx.WithContext(ctx).WithCallerSkip(skip)
}
