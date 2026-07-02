package builtin

import (
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

// TestCollectorConfigWithAppIDScopesRedisStream 确保 Collector Redis Stream 按 app_id 隔离。
func TestCollectorConfigWithAppIDScopesRedisStream(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-1"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	cfg := CollectorConfigWithAppID(config.Config{
		AppID: "site-1",
		Collector: config.CollectorConfig{
			Redis: config.CollectorRedisConfig{Stream: "collector:events"},
		},
	})
	if got := cfg.Redis.Stream; got != "app:site-1:collector:events" {
		t.Fatalf("期望 Collector Redis Stream 按 app_id 加前缀，实际为 %q", got)
	}

	runtimecfg.Set(config.Config{AppID: "site-2"})
	cfg = CollectorConfigWithAppID(config.Config{
		AppID: "site-2",
		Collector: config.CollectorConfig{
			Redis: config.CollectorRedisConfig{Stream: "app:site-1:collector:events"},
		},
	})
	if got := cfg.Redis.Stream; got != "" {
		t.Fatalf("期望已带其它 app 前缀的 Collector Redis Stream 失败闭合，实际为 %q", got)
	}
}
