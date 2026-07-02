package hotreload

import (
	"context"
	"testing"
	"time"

	"admin/internal/config"
)

// TestStateSetConfigFileTrimsPath 确保绑定配置文件路径时会统一裁剪空白。
func TestStateSetConfigFileTrimsPath(t *testing.T) {
	var state State
	if got := state.SetConfigFile("  /tmp/admin.yaml  "); got != "/tmp/admin.yaml" {
		t.Fatalf("期望返回裁剪后的配置路径，实际为 %q", got)
	}
	if got := state.ConfigFile(); got != "/tmp/admin.yaml" {
		t.Fatalf("期望状态保存裁剪后的配置路径，实际为 %q", got)
	}
}

// TestStateStartStopWatcher 确保 watcher 生命周期状态只允许单实例运行。
func TestStateStartStopWatcher(t *testing.T) {
	var state State
	started := make(chan struct{})
	stopped := make(chan struct{})
	if ok := state.StartWatcher(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}); !ok {
		t.Fatal("期望首次启动 watcher 成功")
	}
	<-started
	if !state.WatcherRunning() {
		t.Fatal("期望 watcher 标记为运行中")
	}
	if ok := state.StartWatcher(func(context.Context) {}); ok {
		t.Fatal("期望重复启动 watcher 被拒绝")
	}
	state.StopWatcher()
	<-stopped
	if state.WatcherRunning() {
		t.Fatal("期望停止后 watcher 标记为未运行")
	}
}

// TestStateStartWatcherClearsAfterRunReturns 确保 watcher 自然退出后可再次启动。
func TestStateStartWatcherClearsAfterRunReturns(t *testing.T) {
	var state State
	done := make(chan struct{})
	if ok := state.StartWatcher(func(context.Context) {
		close(done)
	}); !ok {
		t.Fatal("期望首次启动 watcher 成功")
	}
	<-done
	waitForWatcherState(t, func() bool {
		return !state.WatcherRunning()
	})

	started := make(chan struct{})
	stopped := make(chan struct{})
	if ok := state.StartWatcher(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}); !ok {
		t.Fatal("期望自然退出后可以重新启动 watcher")
	}
	<-started
	state.StopWatcher()
	<-stopped
}

// TestStateSuppressFailureWindow 确保重复失败日志在限频窗口内会被抑制。
func TestStateSuppressFailureWindow(t *testing.T) {
	var state State
	now := time.Unix(100, 0)
	if state.SuppressFailure("boom", now, 30*time.Second) {
		t.Fatal("期望首次失败不被抑制")
	}
	if !state.SuppressFailure("boom", now.Add(time.Second), 30*time.Second) {
		t.Fatal("期望窗口内重复失败被抑制")
	}
	if state.SuppressFailure("boom", now.Add(time.Minute), 30*time.Second) {
		t.Fatal("期望窗口外重复失败重新记录")
	}
}

// TestSummaryAndNormalizeDefaults 确保热加载状态摘要和默认归一化稳定。
func TestSummaryAndNormalizeDefaults(t *testing.T) {
	cfg := config.Config{
		Task: config.TaskQueueConfig{Enabled: true, Periodic: []config.TaskPeriodicConfig{
			{Name: "daily"},
		}},
		HotReload: config.HotReloadConfig{Enabled: true},
		Kafka:     config.KafkaConfig{Enabled: true},
	}
	cfg.Mode = " api "
	summary := Summary(cfg)
	if summary != "mode=api user_route=0 task=true periodic=1 hot_reload=true kafka=true" {
		t.Fatalf("热加载摘要不符合预期: %s", summary)
	}
	if got := CheckInterval(0); got != 5*time.Second {
		t.Fatalf("期望默认轮询间隔 5s，实际为 %s", got)
	}
	if got := Source(" "); got != "unknown" {
		t.Fatalf("期望空来源归一化为 unknown，实际为 %q", got)
	}
	if got := FailureCategory(" "); got != "unknown" {
		t.Fatalf("期望空失败分类归一化为 unknown，实际为 %q", got)
	}
}

// waitForWatcherState 等待异步 watcher 状态进入预期。
func waitForWatcherState(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if ok() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("等待 watcher 状态变化超时")
		case <-ticker.C:
		}
	}
}
