package components

import (
	"context"
	"reflect"
	"testing"
)

// TestRegistryRegistersInOrder 确保启动组件注册器按声明顺序执行。
func TestRegistryRegistersInOrder(t *testing.T) {
	called := make([]string, 0, 2)
	registry := NewRegistry(
		NewFunc("first", func(_ context.Context, _ *State) error {
			called = append(called, "first")
			return nil
		}),
		NewFunc("second", func(_ context.Context, _ *State) error {
			called = append(called, "second")
			return nil
		}),
	)
	if err := registry.Register(context.Background(), &State{}); err != nil {
		t.Fatalf("注册组件失败: %v", err)
	}
	if !reflect.DeepEqual(called, []string{"first", "second"}) {
		t.Fatalf("期望组件按顺序注册，实际为 %+v", called)
	}
}

// TestRegistryRejectsDuplicateName 确保同一注册器内组件名称唯一。
func TestRegistryRejectsDuplicateName(t *testing.T) {
	registry := NewRegistry(
		NewFunc("duplicate", nil),
		NewFunc("duplicate", nil),
	)
	if err := registry.Register(context.Background(), &State{}); err == nil {
		t.Fatal("期望重复组件名称返回错误")
	}
}
