package logic

import (
	"context"
	"testing"

	"admin/internal/config"
	"admin/internal/requestctx"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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

// TestBaseLogicRedisHelpersScopeLogicalKeys 验证通用 Redis helper 会自动追加 app_id 前缀。
func TestBaseLogicRedisHelpersScopeLogicalKeys(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	logic := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	if err := logic.RdsSetJSONValue("demo:profile:1", map[string]any{"id": 1}, 60); err != nil {
		t.Fatalf("RdsSetJSONValue() error = %v", err)
	}
	if server.Exists("demo:profile:1") {
		t.Fatal("不应写入未带 app_id 前缀的裸 Redis key")
	}
	if !server.Exists("app:site-a:demo:profile:1") {
		t.Fatal("期望写入 app_id 命名空间下的 Redis key")
	}

	var profile struct {
		ID int `json:"id"` // ID 表示测试记录 ID。
	}
	if err := logic.RdsGetJsonObj("demo:profile:1", &profile); err != nil {
		t.Fatalf("RdsGetJsonObj() error = %v", err)
	}
	if profile.ID != 1 {
		t.Fatalf("RdsGetJsonObj() ID = %d, want 1", profile.ID)
	}
	if err := logic.RdsDelKeys("demo:profile:1"); err != nil {
		t.Fatalf("RdsDelKeys() error = %v", err)
	}
	if server.Exists("app:site-a:demo:profile:1") {
		t.Fatal("期望删除 app_id 命名空间下的 Redis key")
	}
}

// TestBaseLogicAppRedisKeyFailsClosedWithoutAppID 确保运行时缺少 app_id 时不 panic，也不生成裸 Redis key。
func TestBaseLogicAppRedisKeyFailsClosedWithoutAppID(t *testing.T) {
	useRuntimeAppID(t, "")
	if got := (*BaseLogic)(nil).AppID(); got != "" {
		t.Fatalf("nil AppID() = %q, want empty", got)
	}
	if got := (*BaseLogic)(nil).AppRedisKey("demo:key"); got != "" {
		t.Fatalf("nil AppRedisKey() = %q, want empty", got)
	}

	logic := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	if got := logic.AppID(); got != "" {
		t.Fatalf("empty config AppID() = %q, want empty", got)
	}
	if got := logic.AppRedisKey("demo:key"); got != "" {
		t.Fatalf("empty config AppRedisKey() = %q, want empty", got)
	}
}

// TestBaseLogicAppRedisKeyFailsClosedOnAppIDMismatch 确保局部构造的配置不能写入其它站点命名空间。
func TestBaseLogicAppRedisKeyFailsClosedOnAppIDMismatch(t *testing.T) {
	useRuntimeAppID(t, "site-b")
	logic := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	if got := logic.AppRedisKey("demo:key"); got != "" {
		t.Fatalf("mismatched AppRedisKey() = %q, want empty", got)
	}
}
