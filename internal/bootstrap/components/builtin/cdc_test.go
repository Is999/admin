package builtin

import (
	"context"
	"testing"

	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/runmode"
	"admin/internal/config"
	"admin/internal/svc"
)

// TestCDCConsumerExposesStatus 确保 CDC 组件挂到 ServiceContext 供后续管理入口读取。
func TestCDCConsumerExposesStatus(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	state := &core.State{
		Config: config.Config{
			CDC: config.CDCConfig{
				Enabled: false,
				Topics: []config.CDCTopicConfig{{
					Enabled: true,
					Topic:   "dnmp-admin.admin.admin_log",
					Table:   "admin.admin_log",
				}},
			},
		},
		ServiceContext: svcCtx,
		Mode:           runmode.Worker,
	}
	if err := newCDCConsumer().Register(context.Background(), state); err != nil {
		t.Fatalf("注册 CDC 组件失败: %v", err)
	}
	if svcCtx.CDC == nil {
		t.Fatal("期望 ServiceContext.CDC 已挂载")
	}
	tables := svcCtx.CDC.RegisteredTables()
	if len(tables) != 1 || tables[0] != "admin.admin_log" {
		t.Fatalf("CDC 注册表不符合预期: %v", tables)
	}
	if scoped := svcCtx.ScopedWithContext(context.Background()); scoped == nil || scoped.CDC == nil {
		t.Fatal("期望请求作用域 ServiceContext 保留 CDC 状态接口")
	}
}

// TestCDCConsumerRejectsMissingProcessor 确保启用 CDC 时未知表处理器会在启动期失败。
func TestCDCConsumerRejectsMissingProcessor(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	state := &core.State{
		Config: config.Config{
			CDC: config.CDCConfig{
				Enabled: true,
				Brokers: []string{"127.0.0.1:9092"},
				GroupID: "admin-cdc-test",
				Topics: []config.CDCTopicConfig{{
					Enabled: true,
					Topic:   "dnmp-admin.unknown.table",
					Table:   "admin.unknown_table",
				}},
			},
		},
		ServiceContext: svcCtx,
		Mode:           runmode.Worker,
	}
	if err := newCDCConsumer().Register(context.Background(), state); err == nil {
		t.Fatal("期望未知 CDC 表处理器启动校验失败")
	}
}
