package bootstrap

import (
	"context"
	"strings"
	"testing"

	"admin/common/runtimecfg"
	bootstrapresources "admin/internal/bootstrap/resources"
	"admin/internal/config"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestBuildServiceContextDoesNotPublishRuntimeConfigOnFailure 确保启动失败不会污染进程级运行配置。
func TestBuildServiceContextDoesNotPublishRuntimeConfigOnFailure(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "stable-app"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	svcCtx, shutdown, err := BuildServiceContext(context.Background(), config.Config{AppID: "failed-app"})
	if err == nil {
		if svcCtx != nil {
			_ = bootstrapresources.CloseServiceContextResources(context.Background(), svcCtx, nil, false)
		}
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		t.Fatal("期望缺少 MySQL 配置时启动失败")
	}
	if got := runtimecfg.AppID(); got != "stable-app" {
		t.Fatalf("启动失败后 runtimecfg.AppID() = %q, want stable-app", got)
	}
}

// TestAppStopClosesServiceRedis 验证应用停止时会关闭业务主 Redis 连接，避免长生命周期进程退出后残留连接池。
func TestAppStopClosesServiceRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(server.Close)

	app := &App{
		ServiceContext: svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client}),
		taskRedis:      client,
		taskRedisOwned: false,
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("停止应用失败: %v", err)
	}
	if err := client.Ping(context.Background()).Err(); err == nil || !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("期望业务 Redis 已关闭，实际错误: %v", err)
	}
}
