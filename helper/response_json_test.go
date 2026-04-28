package helper

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http/httptest"
	"testing"

	"github.com/Is999/go-utils/errors"

	i18n "admin/common/i18n"
	"admin/internal/requestctx"
)

// TestJsonRespFailSeparatesUserMessageAndInternalError 验证失败响应会隔离前端文案与内部错误链。
func TestJsonRespFailSeparatesUserMessageAndInternalError(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleZHCN)
	requestctx.SetTrace(ctx, "0123456789abcdef0123456789abcdef", "0123456789abcdef")

	recorder := httptest.NewRecorder()
	internalErr := errors.Wrap(stderrors.New("boom"), "TaskLogic.ListQueues 查询任务队列失败")
	NewJsonResp(ctx, recorder).
		SetCode(1005).
		SetError(internalErr).
		Fail(i18n.MsgKeyQueryFail)

	var resp ResponseJSON
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
	if resp.Message != "查询失败" {
		t.Fatalf("前端响应文案不符合预期: %q", resp.Message)
	}
	if resp.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("响应 trace_id 不符合预期: %q", resp.TraceID)
	}
	if resp.SpanID != "0123456789abcdef" {
		t.Fatalf("响应 span_id 不符合预期: %q", resp.SpanID)
	}
	meta := requestctx.FromContext(ctx)
	if meta == nil {
		t.Fatalf("请求元数据不应为空")
	}
	if meta.BizMessage != "查询失败" {
		t.Fatalf("业务文案不符合预期: %q", meta.BizMessage)
	}
	if meta.ErrorMessage != internalErr.Error() {
		t.Fatalf("内部错误链未写入 meta: %q", meta.ErrorMessage)
	}
	if meta.ErrorCause == nil || meta.ErrorCause.Error() != internalErr.Error() {
		t.Fatalf("内部错误对象未保留: %v", meta.ErrorCause)
	}
}
