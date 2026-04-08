package loggerx

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"admin_cron/internal/requestctx"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
)

// TestInfowCallerUsesBusinessCallSite 验证 loggerx 统一封装不会把 caller 固定打印成 loggerx/logger.go。
func TestInfowCallerUsesBusinessCallSite(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buffer))
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
}

// logInfoCallerProbe 从独立函数触发日志，模拟业务代码调用 loggerx.Infow 的栈层。
func logInfoCallerProbe(ctx context.Context) {
	Infow(ctx, "loggerx caller probe")
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

func fieldsToMap(fields []logx.LogField) map[string]string {
	items := make(map[string]string, len(fields))
	for _, field := range fields {
		items[field.Key] = fmt.Sprint(field.Value)
	}
	return items
}

func attrsToMap(attrs []attribute.KeyValue) map[string]string {
	items := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		items[string(attr.Key)] = fmt.Sprint(attr.Value.AsInterface())
	}
	return items
}

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
