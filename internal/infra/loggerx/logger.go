package loggerx

import (
	"admin/internal/config"
	"admin/internal/requestctx"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	jsoniter "github.com/json-iterator/go"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// loggerxCallerSkip 表示从 loggerx 内部写入函数跳到真实业务调用点需要额外跳过的栈层数。
	loggerxCallerSkip = 2
	// loggerxRuntimeCallerSkip 表示 runtime.Caller 从内部写入函数跳到业务调用点的默认栈层数。
	loggerxRuntimeCallerSkip = 3
	// goUtilsCallerSkip 表示 go-utils 日志适配器自身增加的一层封装。
	goUtilsCallerSkip = 1

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
	// fieldErrorTrace 表示便于人读的错误追踪文本，来源于 errors.TraceString。
	fieldErrorTrace = "error_trace"
	// fieldErrorCaller 表示错误链中最早的业务栈帧，用于定位 error 产生位置。
	fieldErrorCaller = "error_caller"
	// fieldCaller 表示本条日志的业务定位点；错误日志优先使用 error_caller，普通日志使用调用点。
	fieldCaller = "caller"
	// fieldLogCaller 表示统一日志封装实际打印位置，避免覆盖 caller 的业务定位语义。
	fieldLogCaller = "log_caller"
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
	if shouldMoveBuiltinCaller(c.Log.FieldKeys.CallerKey) {
		c.Log.FieldKeys.CallerKey = fieldLogCaller
	}
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
	DebugwSkip(context.Background(), goUtilsCallerSkip, msg, l.logFields(args...)...)
}

// Info 输出信息日志。
func (l *goUtilsLogger) Info(msg string, args ...any) {
	InfowSkip(context.Background(), goUtilsCallerSkip, msg, l.logFields(args...)...)
}

// Warn 输出警告日志。
func (l *goUtilsLogger) Warn(msg string, args ...any) {
	SlowwSkip(context.Background(), goUtilsCallerSkip, msg, l.logFields(args...)...)
}

// Error 输出错误日志。
func (l *goUtilsLogger) Error(msg string, args ...any) {
	ErrorTextwSkip(context.Background(), goUtilsCallerSkip, msg, msg, l.logFields(args...)...)
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
	summary := strings.TrimSpace(err.Error())
	return summary
}

// ErrorTrace 把错误链渲染为便于人眼检索的单行文本。
func ErrorTrace(err error) string {
	if err == nil {
		return ""
	}
	trace := strings.TrimSpace(errors.TraceString(err))
	if trace != "" {
		return trace
	}
	return ErrorChain(err)
}

// ErrorCaller 返回错误链中最早的业务栈帧。
func ErrorCaller(err error) string {
	if err == nil {
		return ""
	}
	if caller := firstTraceCallerFromJSON(ErrorChain(err)); caller != "" {
		return caller
	}
	return firstTraceCallerFromText(ErrorTrace(err))
}

// ErrorFields 返回统一错误日志字段，确保所有错误日志同时输出错误信息和错误追踪链路。
func ErrorFields(err error) []logx.LogField {
	if err == nil {
		return nil
	}
	summary := strings.TrimSpace(err.Error())
	chain := ErrorChain(err)
	fields := []logx.LogField{
		logx.Field(fieldError, summary),
		logx.Field(fieldErrorChain, chain),
	}
	if trace := ErrorTrace(err); trace != "" && trace != summary {
		fields = append(fields, logx.Field(fieldErrorTrace, trace))
	}
	if caller := ErrorCaller(err); caller != "" {
		fields = append(fields, logx.Field(fieldErrorCaller, caller))
	}
	return fields
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
	errorw(ctx, 0, msg, err, fields...)
}

// errorw 写入错误日志，并按调用方传入的额外 skip 修正 caller。
func errorw(ctx context.Context, skip int, msg string, err error, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorFields(err)...)
	fields = appendErrorCallerFields(fields, err, callerLocation(runtimeCallerSkip(skip)))
	loggerFor(ctx, normalizeCallerSkip(skip)).Errorw(msg, fields...)
}

// ErrorTextw 统一输出只有错误文本的错误日志，并自动补齐 error 与 error_chain 字段。
func ErrorTextw(ctx context.Context, msg string, errorText string, fields ...logx.LogField) {
	errorTextw(ctx, 0, msg, errorText, fields...)
}

// errorTextw 写入文本错误日志，适用于尚未构造成 error 的失败原因。
func errorTextw(ctx context.Context, skip int, msg string, errorText string, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorTextFields(errorText)...)
	fields = appendCallerField(fields, callerLocation(runtimeCallerSkip(skip)))
	loggerFor(ctx, normalizeCallerSkip(skip)).Errorw(msg, fields...)
}

// ErrorwSkip 统一输出带 caller skip 的错误日志，适用于 GORM、适配器等封装层。
func ErrorwSkip(ctx context.Context, skip int, msg string, err error, fields ...logx.LogField) {
	errorw(ctx, skip, msg, err, fields...)
}

// ErrorTextwSkip 统一输出带 caller skip 的文本错误日志，适用于尚未构造成 error 对象的场景。
func ErrorTextwSkip(ctx context.Context, skip int, msg string, errorText string, fields ...logx.LogField) {
	errorTextw(ctx, skip, msg, errorText, fields...)
}

// Infow 统一输出信息日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Infow(ctx context.Context, msg string, fields ...logx.LogField) {
	infow(ctx, 0, msg, fields...)
}

// InfowSkip 输出带 caller skip 的信息日志，适用于第三方适配器等额外封装层。
func InfowSkip(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	infow(ctx, skip, msg, fields...)
}

// infow 写入信息日志，并统一补充业务 caller 字段。
func infow(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	fields = appendCallerField(fields, callerLocation(runtimeCallerSkip(skip)))
	loggerFor(ctx, normalizeCallerSkip(skip)).Infow(msg, fields...)
}

// Debugw 统一输出调试日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Debugw(ctx context.Context, msg string, fields ...logx.LogField) {
	debugw(ctx, 0, msg, fields...)
}

// DebugwSkip 输出带 caller skip 的调试日志，适用于第三方适配器等额外封装层。
func DebugwSkip(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	debugw(ctx, skip, msg, fields...)
}

// debugw 写入调试日志，并统一补充业务 caller 字段。
func debugw(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	fields = appendCallerField(fields, callerLocation(runtimeCallerSkip(skip)))
	loggerFor(ctx, normalizeCallerSkip(skip)).Debugw(msg, fields...)
}

// Sloww 统一输出慢操作日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Sloww(ctx context.Context, msg string, fields ...logx.LogField) {
	sloww(ctx, 0, msg, fields...)
}

// SlowwSkip 输出带 caller skip 的慢操作日志，适用于第三方适配器等额外封装层。
func SlowwSkip(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	sloww(ctx, skip, msg, fields...)
}

// sloww 写入慢操作日志，并统一补充业务 caller 字段。
func sloww(ctx context.Context, skip int, msg string, fields ...logx.LogField) {
	fields = appendCallerField(fields, callerLocation(runtimeCallerSkip(skip)))
	loggerFor(ctx, normalizeCallerSkip(skip)).Sloww(msg, fields...)
}

// appendLogFields 统一复制并追加日志字段，避免调用方直接复用底层切片导致字段串写。
func appendLogFields(base []logx.LogField, extra ...logx.LogField) []logx.LogField {
	merged := make([]logx.LogField, 0, len(base)+len(extra))
	merged = append(merged, base...)
	merged = append(merged, extra...)
	return merged
}

// runtimeCallerSkip 返回 runtime.Caller 使用的最终 skip。
func runtimeCallerSkip(skip int) int {
	return loggerxRuntimeCallerSkip + positiveSkip(skip)
}

// appendErrorCallerFields 优先使用 error 产生位置作为业务 caller。
func appendErrorCallerFields(fields []logx.LogField, err error, logCaller string) []logx.LogField {
	sourceCaller := ErrorCaller(err)
	if sourceCaller == "" {
		sourceCaller = logCaller
	}
	fields = appendCallerField(fields, sourceCaller)
	if logCaller != "" && logCaller != sourceCaller {
		fields = appendLogFields(fields, logx.Field(fieldLogCaller, logCaller))
	}
	return fields
}

// appendCallerField 追加统一 caller 字段，空值不输出。
func appendCallerField(fields []logx.LogField, caller string) []logx.LogField {
	caller = strings.TrimSpace(caller)
	if caller == "" {
		return fields
	}
	return appendLogFields(fields, logx.Field(fieldCaller, caller))
}

// errorTraceNode 表示 go-utils errors.TraceJSON 中的错误链节点。
type errorTraceNode struct {
	Trace []string          `json:"trace"` // Trace 保存当前错误节点的栈帧文本。
	Err   json.RawMessage   `json:"err"`   // Err 保存单个下级错误节点。
	Errs  []json.RawMessage `json:"errs"`  // Errs 保存多个下级错误节点。
}

// firstTraceCallerFromJSON 从错误链 JSON 中提取最早的业务栈帧。
func firstTraceCallerFromJSON(traceJSON string) string {
	traceJSON = strings.TrimSpace(traceJSON)
	if traceJSON == "" || traceJSON == "null" {
		return ""
	}
	var node errorTraceNode
	if err := json.Unmarshal([]byte(traceJSON), &node); err != nil {
		return ""
	}
	return firstTraceCallerFromNode(node)
}

// firstTraceCallerFromNode 按当前节点、单错误、多错误顺序查找业务栈帧。
func firstTraceCallerFromNode(node errorTraceNode) string {
	for _, frame := range node.Trace {
		if caller := traceFrameLocation(frame); caller != "" {
			return caller
		}
	}
	if caller := firstTraceCallerFromRaw(node.Err); caller != "" {
		return caller
	}
	for _, raw := range node.Errs {
		if caller := firstTraceCallerFromRaw(raw); caller != "" {
			return caller
		}
	}
	return ""
}

// firstTraceCallerFromRaw 解析原始错误节点并提取业务栈帧。
func firstTraceCallerFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var node errorTraceNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	return firstTraceCallerFromNode(node)
}

// firstTraceCallerFromText 从文本错误链中提取业务栈帧。
func firstTraceCallerFromText(traceText string) string {
	traceText = strings.TrimSpace(traceText)
	if traceText == "" {
		return ""
	}
	return traceFrameLocation(traceText)
}

// traceFrameLocation 从单个栈帧文本中截取短文件名和行号。
func traceFrameLocation(frame string) string {
	frame = strings.TrimSpace(frame)
	if frame == "" {
		return ""
	}
	if start := strings.LastIndex(frame, " ("); start >= 0 && strings.HasSuffix(frame, ")") {
		frame = strings.TrimSuffix(frame[start+2:], ")")
	}
	idx := strings.LastIndex(frame, ".go:")
	if idx < 0 {
		return ""
	}
	end := idx + len(".go:")
	for end < len(frame) && frame[end] >= '0' && frame[end] <= '9' {
		end++
	}
	start := strings.LastIndexAny(frame[:idx], " \t(")
	if start >= 0 {
		return frame[start+1 : end]
	}
	return frame[:end]
}

// callerLocation 返回 runtime.Caller 对应的短文件名和行号。
func callerLocation(skip int) string {
	if skip < 0 {
		skip = 0
	}
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	return shortCaller(file, line)
}

// shortCaller 保留最后两段路径，避免日志 caller 过长。
func shortCaller(file string, line int) string {
	file = filepath.ToSlash(file)
	idx := strings.LastIndexByte(file, '/')
	if idx >= 0 {
		if prev := strings.LastIndexByte(file[:idx], '/'); prev >= 0 {
			file = file[prev+1:]
		}
	}
	return file + ":" + strconv.Itoa(line)
}

// shouldMoveBuiltinCaller 判断是否需要把 go-zero 内置 caller 改名为 log_caller。
func shouldMoveBuiltinCaller(callerKey string) bool {
	callerKey = strings.TrimSpace(callerKey)
	return callerKey == "" || callerKey == fieldCaller
}

// positiveSkip 把负数 skip 归零，避免调用点向错误方向偏移。
func positiveSkip(skip int) int {
	if skip < 0 {
		return 0
	}
	return skip
}

// normalizeCallerSkip 归一化调用方传入的额外 skip，并补上 loggerx 自身封装层。
func normalizeCallerSkip(skip int) int {
	return loggerxCallerSkip + positiveSkip(skip)
}

// loggerFor 返回带上下文字段和 caller skip 的 logx logger。
func loggerFor(ctx context.Context, skip int) logx.Logger {
	if ctx == nil {
		return LoggerWithCallerSkip(skip)
	}
	return logx.WithContext(BindContext(ctx)).WithCallerSkip(positiveSkip(skip))
}

// BindContext 将当前请求字段绑定进 logx context，后续 logx.WithContext(ctx) 会自动带上这些字段。
func BindContext(ctx context.Context) context.Context {
	fields := FieldsFromContext(ctx)
	if len(fields) == 0 {
		return ctx
	}
	return logx.ContextWithFields(ctx, fields...)
}

// LoggerWithCallerSkip 返回带 caller skip 的底层 logger，供第三方 logger 适配器使用。
func LoggerWithCallerSkip(skip int) logx.Logger {
	return logx.WithCallerSkip(positiveSkip(skip))
}
