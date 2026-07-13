package loggerx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
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
	// fieldLatencyMS 表示请求、任务或下游调用耗时毫秒数。
	fieldLatencyMS = "latency_ms"
	// fieldSuccess 表示请求或任务是否成功。
	fieldSuccess = "success"

	// FieldIntervalSeconds 表示轮询、调度或重试间隔秒数。
	FieldIntervalSeconds = "interval_seconds"
	// FieldWindowStartUnix 表示时间窗口起点 Unix 秒。
	FieldWindowStartUnix = "window_start_unix"
	// FieldWindowEndUnix 表示时间窗口终点排他边界 Unix 秒。
	FieldWindowEndUnix = "window_end_unix"
)

// publicLogFieldNames 仅保留跨请求、任务和错误排障共用的顶层检索字段。
// 路径、SQL、payload 和统计量等详情写入 content，避免日志平台字段膨胀。
var publicLogFieldNames = map[string]struct{}{
	fieldTraceID:     {},
	fieldSpanID:      {},
	fieldRoute:       {},
	fieldHTTPMethod:  {},
	fieldIP:          {},
	fieldUserID:      {},
	fieldHTTPStatus:  {},
	fieldBizCode:     {},
	fieldError:       {},
	fieldErrorChain:  {},
	fieldErrorCaller: {},
	fieldCaller:      {},
	fieldLogCaller:   {},
	fieldTaskID:      {},
	fieldWorkflowID:  {},
	fieldMode:        {},
	fieldNode:        {},
	fieldShard:       {},
	fieldShardIndex:  {},
	fieldShardTotal:  {},
	fieldLatencyMS:   {},
	fieldSuccess:     {},
}

// Errorw 统一输出带错误链路的错误日志，并自动绑定上下文中的 trace/task/workflow 字段。
func Errorw(ctx context.Context, msg string, err error, fields ...logx.LogField) {
	errorw(ctx, 0, msg, err, fields...)
}

// errorw 写入错误日志，并按调用方传入的额外 skip 修正 caller。
func errorw(ctx context.Context, skip int, msg string, err error, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorFields(err)...)
	fields = appendErrorCallerFields(fields, err, callerLocation(runtimeCallerSkip(skip)))
	msg, fields = splitLogFields(msg, appendContextFields(ctx, fields))
	LoggerWithCallerSkip(normalizeCallerSkip(skip)).Errorw(msg, fields...)
}

// ErrorTextw 统一输出只有错误文本的错误日志，并自动补齐 error 与 error_chain 字段。
func ErrorTextw(ctx context.Context, msg string, errorText string, fields ...logx.LogField) {
	errorTextw(ctx, 0, msg, errorText, fields...)
}

// errorTextw 写入文本错误日志，适用于尚未构造成 error 的失败原因。
func errorTextw(ctx context.Context, skip int, msg string, errorText string, fields ...logx.LogField) {
	fields = appendLogFields(fields, ErrorTextFields(errorText)...)
	fields = appendCallerField(fields, callerLocation(runtimeCallerSkip(skip)))
	msg, fields = splitLogFields(msg, appendContextFields(ctx, fields))
	LoggerWithCallerSkip(normalizeCallerSkip(skip)).Errorw(msg, fields...)
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
	msg, fields = splitLogFields(msg, appendContextFields(ctx, fields))
	LoggerWithCallerSkip(normalizeCallerSkip(skip)).Infow(msg, fields...)
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
	msg, fields = splitLogFields(msg, appendContextFields(ctx, fields))
	LoggerWithCallerSkip(normalizeCallerSkip(skip)).Debugw(msg, fields...)
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
	msg, fields = splitLogFields(msg, appendContextFields(ctx, fields))
	LoggerWithCallerSkip(normalizeCallerSkip(skip)).Sloww(msg, fields...)
}

// appendContextFields 先补上下文字段，再追加调用点字段，保证调用点的事件结果优先。
func appendContextFields(ctx context.Context, fields []logx.LogField) []logx.LogField {
	return appendLogFields(FieldsFromContext(ctx), fields...)
}

// splitLogFields 将非公共字段折叠进 content，减少日志平台动态字段数量。
func splitLogFields(msg string, fields []logx.LogField) (string, []logx.LogField) {
	if len(fields) == 0 {
		return msg, nil
	}
	publicFields := make([]logx.LogField, 0, len(fields))
	details := make([]string, 0, len(fields))
	for _, field := range fields {
		field.Key = strings.TrimSpace(field.Key)
		if field.Key == "" {
			field.Key = "field"
		}
		if isPublicLogField(field.Key) {
			publicFields = append(publicFields, field)
			continue
		}
		details = append(details, formatLogDetail(field))
	}
	if len(details) == 0 {
		return msg, publicFields
	}
	msg = strings.TrimSpace(msg)
	detailText := strings.Join(details, " ")
	if msg == "" {
		return detailText, publicFields
	}
	return msg + " | " + detailText, publicFields
}

// publicLogFields 过滤出允许写入顶层索引的公共字段，供直接绑定 logx context 的场景复用。
func publicLogFields(fields []logx.LogField) []logx.LogField {
	if len(fields) == 0 {
		return nil
	}
	publicFields := make([]logx.LogField, 0, len(fields))
	for _, field := range fields {
		field.Key = strings.TrimSpace(field.Key)
		if field.Key == "" || !isPublicLogField(field.Key) {
			continue
		}
		publicFields = append(publicFields, field)
	}
	return publicFields
}

// isPublicLogField 判断字段是否属于公共检索字段。
func isPublicLogField(key string) bool {
	_, ok := publicLogFieldNames[key]
	return ok
}

// formatLogDetail 把单个非公共字段格式化成稳定的 key=value 文本。
func formatLogDetail(field logx.LogField) string {
	return field.Key + "=" + formatLogValue(field.Value)
}

// formatLogValue 将字段值转换成单行文本，复杂值优先使用 JSON 保真。
func formatLogValue(value any) string {
	switch val := value.(type) {
	case nil:
		return "null"
	case string:
		return formatLogString(val)
	case error:
		return formatLogString(val.Error())
	case fmt.Stringer:
		return formatLogString(val.String())
	default:
		raw, err := json.Marshal(val)
		if err == nil {
			return string(raw)
		}
		return formatLogString(fmt.Sprint(val))
	}
}

// formatLogString 将字符串压成单行，必要时加 JSON 引号避免空格和等号歧义。
func formatLogString(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value))
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " =\"'{}[],:|") {
		raw, err := json.Marshal(value)
		if err == nil {
			return string(raw)
		}
	}
	return value
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
