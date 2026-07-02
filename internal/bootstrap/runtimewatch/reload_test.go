package runtimewatch

import (
	"context"
	"testing"
	"time"

	"admin/internal/config"
)

// TestStartSkipsFileSource 验证文件来源配置不会启动 DB active release watcher。
func TestStartSkipsFileSource(t *testing.T) {
	var state State
	started := Start(WatchScope{
		State: &state,
		CurrentConfig: func() config.Config {
			return config.Config{}
		},
	})
	if started {
		t.Fatal("期望 file 来源配置不启动 runtime watcher")
	}
}

// TestPollDelayUsesMinIntervalAndJitter 验证轮询间隔使用 runtime_config 下限并追加有限抖动。
func TestPollDelayUsesMinIntervalAndJitter(t *testing.T) {
	cfg := config.Config{}
	cfg.RuntimeConfig.Source = "database"
	cfg.RuntimeConfig.PollIntervalSeconds = 1

	delay := PollDelay(cfg)
	if delay < 5*time.Second {
		t.Fatalf("PollDelay() = %s, want >= 5s", delay)
	}
	if delay >= 5500*time.Millisecond {
		t.Fatalf("PollDelay() = %s, want < 5.5s", delay)
	}
}

// TestApplyActiveSnapshotSkipsFileSource 验证文件来源热加载不会访问 DB active release。
func TestApplyActiveSnapshotSkipsFileSource(t *testing.T) {
	before := config.Config{}
	after := config.Config{}
	if err := ApplyActiveSnapshot(context.Background(), nil, before, &after); err != nil {
		t.Fatalf("ApplyActiveSnapshot() error = %v", err)
	}
}
