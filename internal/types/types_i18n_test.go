//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"context"
	"strings"
	"testing"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/requestctx"
)

// TestBizResultResolveMessage 验证对应场景。
func TestBizResultResolveMessage(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleENUS)

	resp := &BizResult{Code: codes.DeleteSuccess}
	if got := resp.ResolveMessage(ctx); got != "Deleted successfully" {
		t.Fatalf("按业务码解析文案不符合预期: %q", got)
	}

	resp = (&BizResult{Code: codes.Fail}).SetI18nMessage(i18n.MsgKeyTokenExpired)
	if got := resp.ResolveMessage(ctx); got != "Login expired, please login again" {
		t.Fatalf("按国际化键解析文案不符合预期: %q", got)
	}

	resp = &BizResult{Code: codes.Fail, MessageKey: i18n.MsgKeySuccess}
	if got := resp.ResolveMessage(ctx); got != "Success" {
		t.Fatalf("按消息键解析文案不符合预期: %q", got)
	}
}

// TestParamErrorResult 验证对应场景。
func TestParamErrorResult(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleENUS)

	resp := ParamErrorResult(errors.New("field userId is required"))
	if resp.Code != codes.ParamError {
		t.Fatalf("ParamErrorResult 返回码不符合预期: %d", resp.Code)
	}
	if resp.Error != Nil {
		t.Fatalf("ParamErrorResult 错误字段不符合预期: %v", resp.Error)
	}
	if got := resp.ResolveMessage(ctx); got != "Invalid parameter format: field userId is required" {
		t.Fatalf("ParamErrorResult 解析文案不符合预期: %q", got)
	}
}

// TestPageReqValidateCapsPageSize 验证通用分页会限制单页数量，避免异常请求放大数据库压力。
func TestPageReqValidateCapsPageSize(t *testing.T) {
	getReq := GetPageReq{Page: -1, PageSize: 1000}
	if err := getReq.Validate(); err != nil {
		t.Fatalf("GetPageReq Validate 失败: %v", err)
	}
	if getReq.Page != defaultPageNumber || getReq.PageSize != maxPageSize {
		t.Fatalf("GetPageReq 归一化结果不符合预期: %+v", getReq)
	}

	postReq := PostPageReq{Page: 0, PageSize: 0}
	if err := postReq.Validate(); err != nil {
		t.Fatalf("PostPageReq Validate 失败: %v", err)
	}
	if postReq.Page != defaultPageNumber || postReq.PageSize != defaultPageSize {
		t.Fatalf("PostPageReq 归一化结果不符合预期: %+v", postReq)
	}
}

// TestBizResultState 验证对应场景。
func TestBizResultState(t *testing.T) {
	success := NewBizResult(codes.UpdateSuccess)
	if !success.IsSuccess() || success.IsFailure() {
		t.Fatalf("期望 update success 被视为成功状态")
	}

	failure := NewBizResult(codes.Unauthorized)
	if failure.IsSuccess() || !failure.IsFailure() {
		t.Fatalf("期望 unauthorized 被视为失败状态")
	}

	failure = NewBizResult(codes.Success).WithError(errors.New("boom"))
	if failure.IsSuccess() || !failure.IsFailure() {
		t.Fatalf("期望成功码但带错误时被视为失败状态")
	}
}

// TestServerErrorWrapsContext 验证非模板消息会把额外上下文写入内部错误链，但不影响对外文案。
func TestServerErrorWrapsContext(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleZHCN)

	resp := ServerError(i18n.MsgKeyQueryFail, errors.New("boom"), "TaskLogic.ListQueues 查询任务队列失败").ToBizResult()
	if got := resp.ResolveMessage(ctx); got != "查询失败" {
		t.Fatalf("ServerError 对外文案不符合预期: %q", got)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "TaskLogic.ListQueues 查询任务队列失败") {
		t.Fatalf("ServerError 未把上下文写入错误链: %v", resp.Error)
	}
}

// TestNotFoundKeepsTemplateArgs 验证带模板占位符的消息参数仍保留给对外文案，不会被误吞掉。
func TestNotFoundKeepsTemplateArgs(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleZHCN)

	resp := NotFound(i18n.MsgKeyCacheKeyNotFound, errors.New("redis: nil"), "demo:key").ToBizResult()
	if got := resp.ResolveMessage(ctx); got != "缓存Key不存在: demo:key" {
		t.Fatalf("NotFound 模板参数文案不符合预期: %q", got)
	}
	if resp.Error == nil || resp.Error.Error() != "redis: nil" {
		t.Fatalf("NotFound 不应误改模板参数型错误链: %v", resp.Error)
	}
}
