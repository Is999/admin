package components

import (
	"context"
	"testing"
)

// TestStateAddLifecycleHooks 确保组件生命周期钩子可以安全注册且名称唯一。
func TestStateAddLifecycleHooks(t *testing.T) {
	state := &State{}
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

// TestStateAddLifecycleHooksDoesNotPartiallyWrite 确保注册失败不会留下半个生命周期钩子。
func TestStateAddLifecycleHooksDoesNotPartiallyWrite(t *testing.T) {
	state := &State{
		stopHooks: []LifecycleHook{{Name: "demo"}},
	}
	err := state.AddLifecycleHooks("demo", func(context.Context) error { return nil }, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("期望重复停止钩子名称返回错误")
	}
	if len(state.startHooks) != 0 {
		t.Fatalf("注册失败不应写入启动钩子，实际数量为 %d", len(state.startHooks))
	}
}

// TestSnapshotCopiesLifecycleHooks 确保装配态切片在进入运行态前会被复制，避免后续误改泄漏。
func TestSnapshotCopiesLifecycleHooks(t *testing.T) {
	state := &State{
		startHooks: []LifecycleHook{{Name: "start-a"}, {Name: "start-b"}},
		stopHooks:  []LifecycleHook{{Name: "stop-a"}},
	}
	snapshot := Snapshot(state)

	state.startHooks[0].Name = "changed"
	state.startHooks = append(state.startHooks, LifecycleHook{Name: "start-c"})
	state.stopHooks[0].Name = "changed-stop"

	if got := len(snapshot.StartHooks); got != 2 {
		t.Fatalf("期望运行态启动钩子快照长度为 2，实际为 %d", got)
	}
	if snapshot.StartHooks[0].Name != "start-a" {
		t.Fatalf("期望运行态启动钩子保留原值，实际为 %q", snapshot.StartHooks[0].Name)
	}
	if snapshot.StopHooks[0].Name != "stop-a" {
		t.Fatalf("期望运行态停止钩子保留原值，实际为 %q", snapshot.StopHooks[0].Name)
	}
}
