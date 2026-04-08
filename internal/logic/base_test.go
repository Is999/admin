package logic

import (
	"context"
	"testing"

	"admin_cron/internal/requestctx"
	"admin_cron/internal/svc"
)

// TestNewBaseLogicWithContextUsesRequestMeta 验证 BaseLogic 会复用请求元数据，而不是重新丢失用户和 trace 信息。
func TestNewBaseLogicWithContextUsesRequestMeta(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetUser(ctx, 7, "tester", "127.0.0.1")
	requestctx.SetAccessToken(ctx, "token-1")
	requestctx.SetTrace(ctx, "trace-1", "span-1")

	logic := NewBaseLogicWithContext(ctx, &svc.ServiceContext{})

	admin := logic.GetCtxAdmin()
	if admin.ID != 7 || admin.Name != "tester" || admin.IP != "127.0.0.1" {
		t.Fatalf("unexpected ctx admin: %+v", admin)
	}
	if logic.AccessToken() != "token-1" {
		t.Fatalf("expected access token from request meta")
	}
	if meta := logic.Meta(); meta == nil || meta.TraceID != "trace-1" || meta.SpanID != "span-1" {
		t.Fatalf("unexpected request meta: %+v", meta)
	}
}
