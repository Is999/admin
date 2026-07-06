package configload

import (
	"os"
	"path/filepath"
	"strings"
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

// TestLoadConfigIgnoresRemovedWorkflowRetention 确保历史工作流保留字段不影响新配置加载。
func TestLoadConfigIgnoresRemovedWorkflowRetention(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
task:
  completed_retention_seconds: 90
  workflow_retention_seconds: 120
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}

	cfg, err := Load(mainFile)
	if err != nil {
		t.Fatalf("历史 workflow_retention_seconds 不应导致配置加载失败: %v", err)
	}
	if cfg.Task.CompletedRetentionSeconds != 90 {
		t.Fatalf("期望保留 completed_retention_seconds=90，实际为 %d", cfg.Task.CompletedRetentionSeconds)
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
  default_task:
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
	if cfg.Collector.Kafka.WriteBatchSize != 321 {
		t.Fatalf("期望继承 Kafka write_batch_size=321，实际为 %d", cfg.Collector.Kafka.WriteBatchSize)
	}
	if cfg.Collector.Kafka.WriteTimeout != 7 {
		t.Fatalf("期望继承 Kafka write_timeout=7，实际为 %d", cfg.Collector.Kafka.WriteTimeout)
	}
	if cfg.Kafka.Topics.UserTag != "user_tag_event" {
		t.Fatalf("期望加载 Kafka 用户标签 topic，实际为 %q", cfg.Kafka.Topics.UserTag)
	}
}

// TestLoadConfigSkipsDisabledCollectorDetailValidation 确保关闭 Collector 时不校验其子配置细节。
func TestLoadConfigSkipsDisabledCollectorDetailValidation(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
kafka:
  brokers:
    - "127.0.0.1:9092"
collector:
  enabled: false
  transport: "db"
  consume_mode: "outbox"
  kafka:
    enabled: true
    topic: "collector_events"
    group_id: "collector_group"
  default_task:
    batch_size: -1
  redis:
    enabled: false
  db:
    runner_batch_size: 500
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}

	cfg, err := Load(mainFile)
	if err != nil {
		t.Fatalf("关闭 Collector 时不应校验子配置，实际错误: %v", err)
	}
	if cfg.Collector.Enabled {
		t.Fatal("期望 Collector 保持关闭")
	}
}

// TestLoadConfigValidatesEnabledCollectorConfig 确保开启 Collector 时只按当前配置契约校验。
func TestLoadConfigValidatesEnabledCollectorConfig(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
kafka:
  brokers:
    - "127.0.0.1:9092"
collector:
  enabled: true
  transport: "db"
  kafka:
    enabled: true
    topic: "collector_events"
    group_id: "collector_group"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}

	_, err := Load(mainFile)
	if err == nil {
		t.Fatal("期望开启 Collector 但缺少当前任务配置时返回错误，实际为 nil")
	}
	if !strings.Contains(err.Error(), "collector.default_task.topic") {
		t.Fatalf("期望按当前配置契约返回任务 Topic 错误，实际为: %v", err)
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
