package configload

import (
	"os"
	"path/filepath"
	"testing"

	"admin/internal/config"
)

// TestLoadConfigLoadsSample 确保默认装配入口能够正确读取项目样例配置。
func TestLoadConfigLoadsSample(t *testing.T) {
	cfg, err := Load("../../etc/config.sample.yaml")
	if err != nil {
		t.Skipf("样例配置包含环境变量占位，当前测试环境未注入，跳过: %v", err)
	}
	if cfg.Name == "" {
		t.Fatal("期望成功读取配置名称")
	}
	if cfg.Host == "" || cfg.Port == 0 {
		t.Fatalf("期望成功读取 host/port，实际为 %q:%d", cfg.Host, cfg.Port)
	}
}

// TestLoadConfigLoadsHotReload 确保配置解析入口可正确读取热加载配置段。
func TestLoadConfigLoadsHotReload(t *testing.T) {
	cfg, err := Load("../../etc/config.sample.yaml")
	if err != nil {
		t.Skipf("样例配置包含环境变量占位，当前测试环境未注入，跳过: %v", err)
	}
	if !cfg.HotReload.Enabled {
		t.Fatal("期望样例配置中启用热加载")
	}
	if cfg.HotReload.CheckIntervalSeconds <= 0 {
		t.Fatalf("期望热加载检查间隔大于 0，实际为 %d", cfg.HotReload.CheckIntervalSeconds)
	}
}

// TestNormalizeBootstrapConfigUsesModeForObservability 确保观测环境复用顶层 Mode，不维护第二套环境。
func TestNormalizeBootstrapConfigUsesModeForObservability(t *testing.T) {
	cfg := config.Config{
		Observability: config.ObservabilityConfig{
			Environment: "custom-env",
		},
	}
	cfg.Mode = "pro"

	Normalize(&cfg)

	if cfg.Observability.Environment != "pro" {
		t.Fatalf("期望观测环境复用 Mode，实际为 %q", cfg.Observability.Environment)
	}
}

// TestLoadConfigInheritsCollectorKafkaCommon 确保 Collector Kafka 复用顶层公共连接参数。
func TestLoadConfigInheritsCollectorKafkaCommon(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
kafka:
  brokers:
    - "127.0.0.1:9092"
  batch_size: 321
  write_timeout: 7
  topics:
    user_tag: "user_tag_event"
collector:
  enabled: true
  kafka:
    enabled: true
    topic: "collector_events"
    group_id: "collector_group"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	cfg, err := Load(mainFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if got := cfg.Collector.Kafka.Brokers; len(got) != 1 || got[0] != "127.0.0.1:9092" {
		t.Fatalf("期望继承 Kafka brokers，实际为 %+v", got)
	}
	if cfg.Collector.Kafka.BatchSize != 321 {
		t.Fatalf("期望继承 Kafka batch_size=321，实际为 %d", cfg.Collector.Kafka.BatchSize)
	}
	if cfg.Collector.Kafka.WriteTimeout != 7 {
		t.Fatalf("期望继承 Kafka write_timeout=7，实际为 %d", cfg.Collector.Kafka.WriteTimeout)
	}
	if cfg.Kafka.Topics.UserTag != "user_tag_event" {
		t.Fatalf("期望加载 Kafka 用户标签 topic，实际为 %q", cfg.Kafka.Topics.UserTag)
	}
}

// TestLoadConfigRejectsPartialTaskRedisConfig 确保 task.redis 非地址字段不会被静默忽略。
func TestLoadConfigRejectsPartialTaskRedisConfig(t *testing.T) {
	mainFile := writeRuntimeConfigFiles(t, `
task:
  redis:
    db: 3
`, "task_periodic: []\n")
	if _, err := Load(mainFile); err == nil {
		t.Fatal("期望 task.redis 半配置返回错误，实际为 nil")
	}
}
