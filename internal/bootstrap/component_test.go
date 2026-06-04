package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"
)

// TestComponentRegistryRegistersInOrder 确保启动组件注册中心按声明顺序执行组件。
func TestComponentRegistryRegistersInOrder(t *testing.T) {
	called := make([]string, 0, 2)
	registry := NewComponentRegistry(
		NewComponentFunc("first", func(ctx context.Context, state *ComponentState) error {
			_ = ctx
			_ = state
			called = append(called, "first")
			return nil
		}),
		NewComponentFunc("second", func(ctx context.Context, state *ComponentState) error {
			_ = ctx
			_ = state
			called = append(called, "second")
			return nil
		}),
	)

	if err := registry.Register(context.Background(), &ComponentState{}); err != nil {
		t.Fatalf("注册启动组件失败: %v", err)
	}
	if !reflect.DeepEqual(called, []string{"first", "second"}) {
		t.Fatalf("期望组件按顺序注册，实际为 %+v", called)
	}
}

// TestComponentRegistryRejectsDuplicateName 确保启动组件名称保持唯一。
func TestComponentRegistryRejectsDuplicateName(t *testing.T) {
	registry := NewComponentRegistry(
		NewComponentFunc("duplicate", nil),
		NewComponentFunc("duplicate", nil),
	)

	if err := registry.Register(context.Background(), &ComponentState{}); err == nil {
		t.Fatal("期望重复组件名称返回错误")
	}
}

// TestResolveStartupComponentsUsesOptions 确保内置组件开关与外部组件追加逻辑符合预期。
func TestResolveStartupComponentsUsesOptions(t *testing.T) {
	component := NewComponentFunc("external", nil)
	components := resolveStartupComponents(Options{
		UseDefaultComponents: false,
		Components:           []Component{component},
	})

	if len(components) != 1 || components[0].Name() != "external" {
		t.Fatalf("期望只保留外部组件，实际为 %+v", components)
	}
}

// TestComponentStateAddLifecycleHooks 确保组件生命周期钩子可以安全注册且名称唯一。
func TestComponentStateAddLifecycleHooks(t *testing.T) {
	state := &ComponentState{}
	if err := state.AddLifecycleHooks("demo", func(context.Context) error { return nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("注册生命周期钩子失败: %v", err)
	}
	if len(state.startHooks) != 1 || len(state.stopHooks) != 1 {
		t.Fatalf("期望启动/停止钩子各 1 个，实际 start=%d stop=%d", len(state.startHooks), len(state.stopHooks))
	}
	if err := state.AddLifecycleHooks("demo", func(context.Context) error { return nil }, nil); err == nil {
		t.Fatal("期望重复启动钩子名称返回错误")
	}
}

// TestSnapshotComponentRuntimeCopiesLifecycleHooks 确保装配态切片在进入运行态前会被复制，避免后续误改泄漏。
func TestSnapshotComponentRuntimeCopiesLifecycleHooks(t *testing.T) {
	state := &ComponentState{
		startHooks: []lifecycleHook{{name: "start-a"}, {name: "start-b"}},
		stopHooks:  []lifecycleHook{{name: "stop-a"}},
	}
	snapshot := snapshotComponentRuntime(state)

	state.startHooks[0].name = "changed"
	state.startHooks = append(state.startHooks, lifecycleHook{name: "start-c"})
	state.stopHooks[0].name = "changed-stop"

	if got := len(snapshot.startHooks); got != 2 {
		t.Fatalf("期望运行态启动钩子快照长度为 2，实际为 %d", got)
	}
	if snapshot.startHooks[0].name != "start-a" {
		t.Fatalf("期望运行态启动钩子保留原值，实际为 %q", snapshot.startHooks[0].name)
	}
	if snapshot.stopHooks[0].name != "stop-a" {
		t.Fatalf("期望运行态停止钩子保留原值，实际为 %q", snapshot.stopHooks[0].name)
	}
}

// TestCDCConsumerComponentExposesStatus 确保 CDC 组件挂到 ServiceContext 供后续管理入口读取。
func TestCDCConsumerComponentExposesStatus(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	state := &ComponentState{
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
		Mode:           ModeWorker,
	}
	if err := newCDCConsumerComponent().Register(context.Background(), state); err != nil {
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

// TestCDCConsumerComponentRejectsMissingProcessor 确保启用 CDC 时未知表处理器会在启动期失败。
func TestCDCConsumerComponentRejectsMissingProcessor(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	state := &ComponentState{
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
		Mode:           ModeWorker,
	}
	if err := newCDCConsumerComponent().Register(context.Background(), state); err == nil {
		t.Fatal("期望未知 CDC 表处理器启动校验失败")
	}
}

// TestAppLifecycleHooksOrder 确保启动钩子按注册顺序执行，停止钩子按逆序执行。
func TestAppLifecycleHooksOrder(t *testing.T) {
	called := make([]string, 0, 4)
	app := &App{
		startHooks: []lifecycleHook{
			{name: "first", run: func(context.Context) error {
				called = append(called, "start:first")
				return nil
			}},
			{name: "second", run: func(context.Context) error {
				called = append(called, "start:second")
				return nil
			}},
		},
		stopHooks: []lifecycleHook{
			{name: "first", run: func(context.Context) error {
				called = append(called, "stop:first")
				return nil
			}},
			{name: "second", run: func(context.Context) error {
				called = append(called, "stop:second")
				return nil
			}},
		},
	}

	if err := app.runStartHooks(context.Background()); err != nil {
		t.Fatalf("启动生命周期钩子失败: %v", err)
	}
	if err := app.runStopHooks(context.Background()); err != nil {
		t.Fatalf("停止生命周期钩子失败: %v", err)
	}
	want := []string{"start:first", "start:second", "stop:second", "stop:first"}
	if !reflect.DeepEqual(called, want) {
		t.Fatalf("期望生命周期顺序为 %+v，实际为 %+v", want, called)
	}
}

// TestAppLifecycleHooksRollbackOnStartFailure 确保启动中途失败时会回滚已启动组件，避免后台协程残留。
func TestAppLifecycleHooksRollbackOnStartFailure(t *testing.T) {
	called := make([]string, 0, 3)
	app := &App{
		startHooks: []lifecycleHook{
			{name: "first", run: func(context.Context) error {
				called = append(called, "start:first")
				return nil
			}},
			{name: "second", run: func(context.Context) error {
				called = append(called, "start:second")
				return assertLifecycleError{}
			}},
		},
		stopHooks: []lifecycleHook{
			{name: "first", run: func(context.Context) error {
				called = append(called, "stop:first")
				return nil
			}},
			{name: "second", run: func(context.Context) error {
				called = append(called, "stop:second")
				return nil
			}},
		},
	}

	if err := app.runStartHooks(context.Background()); err == nil {
		t.Fatal("期望启动失败返回错误，实际为 nil")
	}
	want := []string{"start:first", "start:second", "stop:first"}
	if !reflect.DeepEqual(called, want) {
		t.Fatalf("期望启动失败回滚顺序为 %+v，实际为 %+v", want, called)
	}
	if err := app.runStopHooks(context.Background()); err != nil {
		t.Fatalf("期望重复停止保持幂等，实际错误为 %v", err)
	}
}

// assertLifecycleError 是生命周期测试使用的固定错误。
type assertLifecycleError struct{}

// Error 返回测试错误文本。
func (assertLifecycleError) Error() string {
	return "lifecycle failed"
}
