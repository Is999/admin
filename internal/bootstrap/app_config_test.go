package bootstrap

import (
	"context"
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// enabledTaskPeriodicConfig 构造测试中显式启用的周期任务配置。
func enabledTaskPeriodicConfig(item config.TaskPeriodicConfig) config.TaskPeriodicConfig {
	enabled := true
	item.Enabled = &enabled
	return item
}

// TestAppUpdateConfigPropagatesSnapshot 确保应用配置更新会同步刷新 ServiceContext 与 TaskManager 快照。
func TestAppUpdateConfigPropagatesSnapshot(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		JwtSecret: "old-secret",
		Task:      config.TaskQueueConfig{DefaultQueue: "old-queue"},
	}, svc.Dependencies{})

	app := &App{ServiceContext: svcCtx}
	app.UpdateConfig(config.Config{
		JwtSecret: "new-secret",
		Task: config.TaskQueueConfig{
			Enabled:      true,
			DefaultQueue: "new-queue",
			Periodic: []config.TaskPeriodicConfig{
				enabledTaskPeriodicConfig(config.TaskPeriodicConfig{Name: "demo"}),
			},
		},
		Workflows: config.WorkflowsConfig{UserTag: config.UserTagConfig{Enabled: true}},
		HotReload: config.HotReloadConfig{Enabled: true},
		Kafka:     config.KafkaConfig{Enabled: true},
	})

	if got := app.CurrentConfig().JwtSecret; got != "new-secret" {
		t.Fatalf("期望应用配置中的 JwtSecret 已更新，实际为 %q", got)
	}
	if got := svcCtx.CurrentConfig().JwtSecret; got != "new-secret" {
		t.Fatalf("期望 ServiceContext 中的 JwtSecret 已更新，实际为 %q", got)
	}
	if got := svcCtx.CurrentHotReloadStatus().ConfigSummary; got == "" {
		t.Fatal("期望热加载配置摘要已更新")
	}
}

// TestAppUpdateConfigPropagatesTaskManagerSnapshot 验证任务系统快照会随配置热更新。
func TestAppUpdateConfigPropagatesTaskManagerSnapshot(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "old-app"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "old-app",
		DefaultQueue: "old-queue",
	}, client)
	if manager == nil {
		t.Fatal("期望成功创建任务管理器")
	}
	t.Cleanup(func() {
		_ = manager.Stop(context.Background())
	})

	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "old-app",
		Task:  config.TaskQueueConfig{Enabled: true, DefaultQueue: "old-queue"},
	}, svc.Dependencies{Rds: client})
	app := &App{
		ServiceContext: svcCtx,
		TaskManager:    manager,
	}

	runtimecfg.Set(config.Config{AppID: "site-1"})
	app.UpdateConfig(config.Config{
		AppID: "site-1",
		Task: config.TaskQueueConfig{
			Enabled:      true,
			DefaultQueue: "maintenance",
			Queues: map[string]int{
				"default": 1,
			},
		},
	})

	if got := manager.CurrentConfig().DefaultQueue; got != "maintenance" {
		t.Fatalf("期望任务系统默认队列已刷新，实际为 %q", got)
	}
	if got := manager.CurrentConfig().AppID; got != "site-1" {
		t.Fatalf("期望任务系统继承顶层 app_id，实际为 %q", got)
	}
}
