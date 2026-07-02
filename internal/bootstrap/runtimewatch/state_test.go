package runtimewatch

import (
	"context"
	"testing"
	"time"
)

// TestStateStartStop 确保 DB 运行配置 watcher 生命周期只允许单实例运行。
func TestStateStartStop(t *testing.T) {
	var state State
	started := make(chan struct{})
	stopped := make(chan struct{})
	if ok := state.Start(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}); !ok {
		t.Fatal("期望首次启动 watcher 成功")
	}
	<-started
	if ok := state.Start(func(context.Context) {}); ok {
		t.Fatal("期望重复启动 watcher 被拒绝")
	}
	state.Stop()
	<-stopped
}

// TestStateStartClearsAfterRunReturns 确保 watcher 自然退出后可再次启动。
func TestStateStartClearsAfterRunReturns(t *testing.T) {
	var state State
	done := make(chan struct{})
	if ok := state.Start(func(context.Context) {
		close(done)
	}); !ok {
		t.Fatal("期望首次启动 watcher 成功")
	}
	<-done

	started := make(chan struct{})
	stopped := make(chan struct{})
	waitForRuntimeWatcherRestart(t, &state, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	})
	<-started
	state.Stop()
	<-stopped
}

// TestStateAppliedWatermark 确保 active release 水位记录和判断稳定。
func TestStateAppliedWatermark(t *testing.T) {
	var state State
	if state.Applied(3, "sum-a") {
		t.Fatal("期望未记录水位时不命中")
	}
	state.MarkApplied(3, "sum-a")
	if !state.Applied(3, "sum-a") {
		t.Fatal("期望相同版本和 checksum 命中已应用水位")
	}
	if state.Applied(3, "sum-b") {
		t.Fatal("期望 checksum 不同时不命中")
	}
}

// waitForRuntimeWatcherRestart 等待自然退出后的 watcher 状态可重新启动。
func waitForRuntimeWatcherRestart(t *testing.T, state *State, run func(context.Context)) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if state.Start(run) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("等待 runtime watcher 可重启超时")
		case <-ticker.C:
		}
	}
}
