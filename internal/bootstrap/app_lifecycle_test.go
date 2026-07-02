package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"admin/internal/bootstrap/components"
)

// TestAppLifecycleHooksOrder 确保启动钩子按注册顺序执行，停止钩子按逆序执行。
func TestAppLifecycleHooksOrder(t *testing.T) {
	called := make([]string, 0, 4)
	app := &App{
		startHooks: []components.LifecycleHook{
			{Name: "first", Run: func(context.Context) error {
				called = append(called, "start:first")
				return nil
			}},
			{Name: "second", Run: func(context.Context) error {
				called = append(called, "start:second")
				return nil
			}},
		},
		stopHooks: []components.LifecycleHook{
			{Name: "first", Run: func(context.Context) error {
				called = append(called, "stop:first")
				return nil
			}},
			{Name: "second", Run: func(context.Context) error {
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
		startHooks: []components.LifecycleHook{
			{Name: "first", Run: func(context.Context) error {
				called = append(called, "start:first")
				return nil
			}},
			{Name: "second", Run: func(context.Context) error {
				called = append(called, "start:second")
				return assertLifecycleError{}
			}},
		},
		stopHooks: []components.LifecycleHook{
			{Name: "first", Run: func(context.Context) error {
				called = append(called, "stop:first")
				return nil
			}},
			{Name: "second", Run: func(context.Context) error {
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
