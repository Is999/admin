package loggerx

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"admin/internal/requestctx"

	goerrors "github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
)

// TestInfowCallerUsesBusinessCallSite 验证 loggerx 统一封装不会把 caller 固定打印成 loggerx/logger.go。
func TestInfowCallerUsesBusinessCallSite(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	logInfoCallerProbe(context.Background())
	output := buffer.String()
	if !strings.Contains(output, "loggerx/logger_test.go:") {
		t.Fatalf("caller should point to logger test call site, got %s", output)
	}
	if strings.Contains(output, "loggerx/logger.go:") {
		t.Fatalf("caller should not point to loggerx wrapper, got %s", output)
	}
	entry := decodeLogEntry(t, output)
	assertEntryMissing(t, entry, fieldLogCaller)
}

// logInfoCallerProbe 从独立函数触发日志，模拟业务代码调用 loggerx.Infow 的栈层。
func logInfoCallerProbe(ctx context.Context) {
	Infow(ctx, "loggerx caller probe")
}

// TestInfowSkipUsesOuterCallSite 验证调用方可以按需补充封装层数。
func TestInfowSkipUsesOuterCallSite(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	expected := logInfoCallerSkipOuterProbe(context.Background())
	output := buffer.String()
	if !strings.Contains(output, expected) {
		t.Fatalf("caller should point to outer call site %s, got %s", expected, output)
	}
}

// logInfoCallerSkipOuterProbe 返回期望的外层业务调用点。
func logInfoCallerSkipOuterProbe(ctx context.Context) string {
	_, file, line, _ := runtime.Caller(0)
	expected := shortCaller(file, line+2)
	logInfoCallerSkipInnerProbe(ctx)
	return expected
}

// logInfoCallerSkipInnerProbe 模拟业务侧再包一层 loggerx 的调用。
func logInfoCallerSkipInnerProbe(ctx context.Context) {
	InfowSkip(ctx, 1, "loggerx caller skip probe")
}

// TestGoUtilsLoggerCallerUsesSourceCallSite 验证 go-utils 适配器 caller 落在日志产生处。
func TestGoUtilsLoggerCallerUsesSourceCallSite(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	expected := logGoUtilsInfoCallerProbe()
	output := buffer.String()
	if !strings.Contains(output, expected) {
		t.Fatalf("go-utils caller should point to source call site %s, got %s", expected, output)
	}
	if strings.Contains(output, "loggerx/logger.go:") {
		t.Fatalf("go-utils caller should not point to loggerx wrapper, got %s", output)
	}
}

// logGoUtilsInfoCallerProbe 返回 go-utils 适配器应定位的日志产生点。
func logGoUtilsInfoCallerProbe() string {
	_, file, line, _ := runtime.Caller(0)
	expected := shortCaller(file, line+2)
	newGoUtilsLogger(nil).Info("go-utils caller probe")
	return expected
}

// TestInfowMovesDetailFieldsToContent 验证非公共字段进入 content，不再扩散成日志平台顶层字段。
func TestInfowMovesDetailFieldsToContent(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	ctx := requestctx.WithMeta(context.Background(), &requestctx.Meta{
		TraceID:    "trace-1",
		Route:      "user.detail",
		Method:     "GET",
		Path:       "/api/users/7",
		ClientIP:   "127.0.0.1",
		UserID:     7,
		UserName:   "tester",
		BizMessage: "成功",
	})
	Infow(ctx, "字段收口",
		logx.Field(fieldHTTPStatus, 200),
		logx.Field("detail", "select * from demo"),
		logx.Field("bytes", 128),
	)

	entry := decodeLogEntry(t, buffer.String())
	assertEntryValue(t, entry, fieldTraceID, "trace-1")
	assertEntryValue(t, entry, fieldRoute, "user.detail")
	assertEntryValue(t, entry, fieldHTTPMethod, "GET")
	assertEntryValue(t, entry, fieldIP, "127.0.0.1")
	assertEntryValue(t, entry, fieldHTTPStatus, "200")
	assertEntryValue(t, entry, fieldUserID, "7")
	assertEntryMissing(t, entry, "detail")
	assertEntryMissing(t, entry, "bytes")
	assertEntryMissing(t, entry, fieldPath)
	assertEntryMissing(t, entry, fieldUserName)
	assertEntryMissing(t, entry, fieldBizMessage)
	assertEntryMissing(t, entry, fieldLogCaller)
	content := fmt.Sprint(entry["content"])
	for _, want := range []string{`detail="select * from demo"`, "bytes=128", "path=/api/users/7", "user_name=tester", "biz_message=成功"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content should contain %q, got %s", want, content)
		}
	}
}

// TestBindContextKeepsOnlyPublicFields 验证原始 logx context 绑定不会重新扩散非公共字段。
func TestBindContextKeepsOnlyPublicFields(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	ctx := requestctx.WithMeta(context.Background(), &requestctx.Meta{
		TraceID:    "trace-1",
		Route:      "user.detail",
		Method:     "GET",
		Path:       "/api/users/7",
		ClientIP:   "127.0.0.1",
		UserID:     7,
		UserName:   "tester",
		BizMessage: "成功",
		TaskName:   "refresh-cache",
	})
	logx.WithContext(BindContext(ctx)).Info("原始 logx 绑定字段")

	entry := decodeLogEntry(t, buffer.String())
	assertEntryValue(t, entry, fieldTraceID, "trace-1")
	assertEntryValue(t, entry, fieldRoute, "user.detail")
	assertEntryValue(t, entry, fieldHTTPMethod, "GET")
	assertEntryValue(t, entry, fieldIP, "127.0.0.1")
	assertEntryValue(t, entry, fieldUserID, "7")
	assertEntryMissing(t, entry, fieldPath)
	assertEntryMissing(t, entry, fieldUserName)
	assertEntryMissing(t, entry, fieldBizMessage)
	assertEntryMissing(t, entry, fieldTaskName)
}

// TestExplicitFieldsOverrideContextFields 验证调用点字段优先于上下文默认值。
func TestExplicitFieldsOverrideContextFields(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(wrapLogWriter(logx.NewWriter(&buffer)))
	logx.SetLevel(logx.InfoLevel)
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
		logx.SetLevel(logx.InfoLevel)
	})

	ctx := requestctx.WithMeta(context.Background(), &requestctx.Meta{
		TraceID:    "trace-1",
		HTTPStatus: 200,
		BizCode:    1,
	})
	Errorw(ctx, "显式字段覆盖", stderrors.New("invalid sign"),
		logx.Field(fieldHTTPStatus, 401),
		logx.Field(fieldBizCode, 20001),
	)

	entry := decodeLogEntry(t, buffer.String())
	assertEntryValue(t, entry, fieldHTTPStatus, "401")
	assertEntryValue(t, entry, fieldBizCode, "20001")
	assertEntryValue(t, entry, fieldError, "invalid sign")
	assertEntryContains(t, entry, fieldErrorChain, "invalid sign")
	assertEntryMissing(t, entry, fieldLogCaller)
}

// TestDedupeLogCallerFieldsKeepsDifferentCaller 验证错误源和打印点不同的时候保留 log_caller。
func TestDedupeLogCallerFieldsKeepsDifferentCaller(t *testing.T) {
	fields := dedupeLogCallerFields([]logx.LogField{
		logx.Field(fieldCaller, "logic/user.go:10"),
		logx.Field(fieldLogCaller, "middleware/recover.go:20"),
	})

	got := fieldsToMap(fields)
	assertFieldValue(t, got, fieldCaller, "logic/user.go:10")
	assertFieldValue(t, got, fieldLogCaller, "middleware/recover.go:20")
}

// TestCallerDedupWriterRemovesSameLogCaller 验证 Setup 安装的 writer 包装器会去掉重复 log_caller。
func TestCallerDedupWriterRemovesSameLogCaller(t *testing.T) {
	var buffer bytes.Buffer
	writer := wrapLogWriter(logx.NewWriter(&buffer))
	writer.Info("writer caller 去重", logx.Field(fieldCaller, "logic/user.go:10"), logx.Field(fieldLogCaller, "logic/user.go:10"))

	entry := decodeLogEntry(t, buffer.String())
	assertEntryValue(t, entry, fieldCaller, "logic/user.go:10")
	assertEntryMissing(t, entry, fieldLogCaller)
}

// TestErrorFieldsIncludeTraceAndCaller 验证错误字段包含可检索的链路文本和错误源位置。
func TestErrorFieldsIncludeTraceAndCaller(t *testing.T) {
	err := tracedErrorProbe()
	fields := fieldsToMap(ErrorFields(err))

	assertFieldValue(t, fields, fieldError, "boom")
	if !json.Valid([]byte(fields[fieldErrorChain])) {
		t.Fatalf("error_chain should be valid JSON, got %s", fields[fieldErrorChain])
	}
	if !strings.Contains(fields[fieldErrorChain], `"trace"`) {
		t.Fatalf("error_chain should include trace, got %s", fields[fieldErrorChain])
	}
	if !strings.Contains(fields[fieldErrorTrace], "loggerx/logger_test.go:") {
		t.Fatalf("error_trace should include source file, got %s", fields[fieldErrorTrace])
	}
	if !strings.Contains(fields[fieldErrorCaller], "loggerx/logger_test.go:") {
		t.Fatalf("error_caller should include source file, got %s", fields[fieldErrorCaller])
	}
}

// TestErrorCallerFieldsPreferErrorSource 验证错误日志 caller 指向错误源，打印点单独保留为 log_caller。
func TestErrorCallerFieldsPreferErrorSource(t *testing.T) {
	err := tracedErrorProbe()
	fields := fieldsToMap(appendErrorCallerFields(nil, err, "taskqueue/manager.go:2811"))

	if !strings.Contains(fields[fieldCaller], "loggerx/logger_test.go:") {
		t.Fatalf("caller should prefer error source, got %s", fields[fieldCaller])
	}
	assertFieldValue(t, fields, fieldLogCaller, "taskqueue/manager.go:2811")
}

// tracedErrorProbe 构造带 go-utils trace 的测试错误。
func tracedErrorProbe() error {
	return goerrors.Tag(stderrors.New("boom"))
}

// TestFieldsFromMetaUsesUnifiedDictionary 验证关键业务日志字段全部从统一字典产出，避免 access/task/audit 各自散落命名。
func TestFieldsFromMetaUsesUnifiedDictionary(t *testing.T) {
	meta := &requestctx.Meta{
		TraceID:      "trace-1",
		SpanID:       "span-1",
		Route:        "task.workflow.trigger",
		Method:       "POST",
		Path:         "/api/tasks/workflows",
		Locale:       "zh-CN",
		ClientIP:     "127.0.0.1",
		UserID:       7,
		UserName:     "tester",
		HTTPStatus:   200,
		BizCode:      1,
		BizMessage:   "成功",
		ErrorMessage: "内部错误摘要",
		TaskID:       "task-1",
		TaskType:     "workflow:trigger",
		TaskName:     "用户标签工作流",
		TaskQueue:    "default",
		WorkflowID:   "wf-1",
		WorkflowName: "user_tag",
		WorkflowNode: "calc_profile",
		Mode:         "delta",
		ShardIndex:   1,
		ShardTotal:   4,
	}

	fields := fieldsToMap(FieldsFromMeta(meta))
	assertFieldValue(t, fields, fieldTraceID, "trace-1")
	assertFieldValue(t, fields, fieldSpanID, "span-1")
	assertFieldValue(t, fields, fieldRoute, "task.workflow.trigger")
	assertFieldValue(t, fields, fieldHTTPMethod, "POST")
	assertFieldValue(t, fields, fieldPath, "/api/tasks/workflows")
	assertFieldValue(t, fields, fieldLocale, "zh-CN")
	assertFieldValue(t, fields, fieldIP, "127.0.0.1")
	assertFieldValue(t, fields, fieldUserID, "7")
	assertFieldValue(t, fields, fieldUserName, "tester")
	assertFieldValue(t, fields, fieldHTTPStatus, "200")
	assertFieldValue(t, fields, fieldBizCode, "1")
	assertFieldValue(t, fields, fieldBizMessage, "成功")
	assertFieldValue(t, fields, fieldErrorMsg, "内部错误摘要")
	assertFieldValue(t, fields, fieldTaskID, "task-1")
	assertFieldValue(t, fields, fieldTaskType, "workflow:trigger")
	assertFieldValue(t, fields, fieldTaskName, "用户标签工作流")
	assertFieldValue(t, fields, fieldTaskQueue, "default")
	assertFieldValue(t, fields, fieldWorkflowID, "wf-1")
	assertFieldValue(t, fields, fieldWorkflowName, "user_tag")
	assertFieldValue(t, fields, fieldWorkflowNode, "calc_profile")
	assertFieldValue(t, fields, fieldNode, "calc_profile")
	assertFieldValue(t, fields, fieldMode, "delta")
	assertFieldValue(t, fields, fieldShard, "1/4")
	assertFieldValue(t, fields, fieldShardIndex, "1")
	assertFieldValue(t, fields, fieldShardTotal, "4")
}

// TestTraceAttributesFromMetaUsesUnifiedChainFields 验证 trace attribute 与日志字段语义一致。
func TestTraceAttributesFromMetaUsesUnifiedChainFields(t *testing.T) {
	meta := &requestctx.Meta{
		TraceID:      "trace-1",
		SpanID:       "span-1",
		Route:        "task.workflow.trigger",
		Method:       "POST",
		Path:         "/api/tasks/workflows",
		Locale:       "zh-CN",
		ClientIP:     "127.0.0.1",
		UserID:       7,
		UserName:     "tester",
		HTTPStatus:   200,
		BizCode:      1,
		BizMessage:   "成功",
		ErrorMessage: "内部错误摘要",
		LatencyMS:    35,
		TaskID:       "task-1",
		TaskType:     "workflow:trigger",
		TaskName:     "用户标签工作流",
		TaskQueue:    "default",
		WorkflowID:   "wf-1",
		WorkflowName: "user_tag",
		WorkflowNode: "calc_profile",
		Mode:         "delta",
		ShardIndex:   1,
		ShardTotal:   4,
	}

	attrs := attrsToMap(TraceAttributesFromMeta(meta))
	assertFieldValue(t, attrs, "app."+fieldTraceID, "trace-1")
	assertFieldValue(t, attrs, "app."+fieldSpanID, "span-1")
	assertFieldValue(t, attrs, "http.route", "task.workflow.trigger")
	assertFieldValue(t, attrs, "app."+fieldRoute, "task.workflow.trigger")
	assertFieldValue(t, attrs, "http.method", "POST")
	assertFieldValue(t, attrs, "app."+fieldHTTPMethod, "POST")
	assertFieldValue(t, attrs, "url.path", "/api/tasks/workflows")
	assertFieldValue(t, attrs, "app."+fieldPath, "/api/tasks/workflows")
	assertFieldValue(t, attrs, "app."+fieldLocale, "zh-CN")
	assertFieldValue(t, attrs, "client.address", "127.0.0.1")
	assertFieldValue(t, attrs, "app.client_ip", "127.0.0.1")
	assertFieldValue(t, attrs, "enduser.id", "7")
	assertFieldValue(t, attrs, "app."+fieldUserID, "7")
	assertFieldValue(t, attrs, "enduser.name", "tester")
	assertFieldValue(t, attrs, "app."+fieldUserName, "tester")
	assertFieldValue(t, attrs, "http.status_code", "200")
	assertFieldValue(t, attrs, "app."+fieldHTTPStatus, "200")
	assertFieldValue(t, attrs, "app."+fieldBizCode, "1")
	assertFieldValue(t, attrs, "app."+fieldBizMessage, "成功")
	assertFieldValue(t, attrs, "app."+fieldErrorMsg, "内部错误摘要")
	assertFieldValue(t, attrs, "app.latency_ms", "35")
	assertFieldValue(t, attrs, "app."+fieldTaskID, "task-1")
	assertFieldValue(t, attrs, "app."+fieldTaskType, "workflow:trigger")
	assertFieldValue(t, attrs, "app."+fieldTaskName, "用户标签工作流")
	assertFieldValue(t, attrs, "app."+fieldTaskQueue, "default")
	assertFieldValue(t, attrs, "app."+fieldWorkflowID, "wf-1")
	assertFieldValue(t, attrs, "app."+fieldWorkflowName, "user_tag")
	assertFieldValue(t, attrs, "app."+fieldWorkflowNode, "calc_profile")
	assertFieldValue(t, attrs, "app."+fieldNode, "calc_profile")
	assertFieldValue(t, attrs, "app."+fieldMode, "delta")
	assertFieldValue(t, attrs, "app."+fieldShard, "1/4")
	assertFieldValue(t, attrs, "app."+fieldShardIndex, "1")
	assertFieldValue(t, attrs, "app."+fieldShardTotal, "4")
}

// fieldsToMap 把日志字段转成便于断言的字符串 map。
func fieldsToMap(fields []logx.LogField) map[string]string {
	items := make(map[string]string, len(fields))
	for _, field := range fields {
		items[field.Key] = fmt.Sprint(field.Value)
	}
	return items
}

// attrsToMap 把 trace 属性转成便于断言的字符串 map。
func attrsToMap(attrs []attribute.KeyValue) map[string]string {
	items := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		items[string(attr.Key)] = fmt.Sprint(attr.Value.AsInterface())
	}
	return items
}

// assertFieldValue 断言指定字段存在且值匹配。
func assertFieldValue(t *testing.T, fields map[string]string, key, want string) {
	t.Helper()
	got, ok := fields[key]
	if !ok {
		t.Fatalf("expected key %q to exist", key)
	}
	if got != want {
		t.Fatalf("expected key %q to be %q, got %q", key, want, got)
	}
}

// decodeLogEntry 解析 go-zero JSON 日志首行。
func decodeLogEntry(t *testing.T, output string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(output)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	if line == "" {
		t.Fatal("expected log output")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("decode log entry failed: %v, output=%s", err, output)
	}
	return entry
}

// assertEntryValue 断言 JSON 日志字段存在且值匹配。
func assertEntryValue(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	got, ok := entry[key]
	if !ok {
		t.Fatalf("expected entry key %q to exist", key)
	}
	if fmt.Sprint(got) != want {
		t.Fatalf("expected entry key %q to be %q, got %q", key, want, fmt.Sprint(got))
	}
}

// assertEntryContains 断言 JSON 日志字段存在且包含指定文本。
func assertEntryContains(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	got, ok := entry[key]
	if !ok {
		t.Fatalf("expected entry key %q to exist", key)
	}
	if !strings.Contains(fmt.Sprint(got), want) {
		t.Fatalf("expected entry key %q to contain %q, got %q", key, want, fmt.Sprint(got))
	}
}

// assertEntryMissing 断言 JSON 日志没有输出指定顶层字段。
func assertEntryMissing(t *testing.T, entry map[string]any, key string) {
	t.Helper()
	if _, ok := entry[key]; ok {
		t.Fatalf("expected entry key %q to be folded into content, entry=%v", key, entry)
	}
}
