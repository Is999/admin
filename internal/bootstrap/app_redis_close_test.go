package bootstrap

import (
	"context"
	"strings"
	"testing"

	"admin_cron/internal/config"
	"admin_cron/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

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
