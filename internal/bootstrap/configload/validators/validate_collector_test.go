package validators

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigSkipsDisabledCollector 确保关闭 Collector 时不校验子配置细节。
func TestValidateBootstrapConfigSkipsDisabledCollector(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled: false,
		DefaultTask: config.CollectorTaskConfig{
			BatchSize:             -1,
			BatchWaitMilliseconds: maxCollectorTaskBatchWaitMilliseconds + 1,
		},
		Idempotency: config.CollectorIdempotencyConfig{
			TTLSeconds:           60,
			ProcessingTTLSeconds: 120,
			PipelineBatchSize:    maxCollectorIdempotencyPipelineBatchSize + 1,
		},
		Tasks: map[string]config.CollectorTaskConfig{
			" ": {BatchSize: -1},
		},
	}
	if err := ValidateCollector(cfg); err != nil {
		t.Fatalf("关闭 Collector 时不应校验子配置，实际错误: %v", err)
	}
}

// TestValidateBootstrapConfigRejectsEnabledCollectorWithoutKafkaBrokers 确保启用 Collector 时必须具备 Kafka broker。
func TestValidateBootstrapConfigRejectsEnabledCollectorWithoutKafkaBrokers(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector.Enabled = true
	cfg.Collector.DefaultTask = config.CollectorTaskConfig{Topic: "collector_events", GroupID: "collector_group"}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 collector.kafka.brokers 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsCollectorWithoutTaskTopic 确保启用 Collector 时必须配置任务 Topic。
func TestValidateBootstrapConfigRejectsCollectorWithoutTaskTopic(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled: true,
		Kafka: config.CollectorKafkaConfig{
			Brokers: []string{"127.0.0.1:9092"},
		},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望任务 topic 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsCollectorTaskWithoutGroup 确保任务 Topic 必须配套消费组。
func TestValidateBootstrapConfigRejectsCollectorTaskWithoutGroup(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled: true,
		Kafka: config.CollectorKafkaConfig{
			Brokers: []string{"127.0.0.1:9092"},
		},
		DefaultTask: config.CollectorTaskConfig{Topic: "collector_events"},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 task group_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsCollectorTopicGroupConflict 确保同 Topic 不允许配置多个消费组。
func TestValidateBootstrapConfigRejectsCollectorTopicGroupConflict(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled: true,
		Kafka: config.CollectorKafkaConfig{
			Brokers: []string{"127.0.0.1:9092"},
		},
		DefaultTask: config.CollectorTaskConfig{Topic: "collector_events", GroupID: "collector-a"},
		Tasks: map[string]config.CollectorTaskConfig{
			"user_stat": {Topic: "collector_events", GroupID: "collector-b"},
		},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望同 Topic 多 group_id 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidCollectorIdempotencyTTL 确保 Collector 幂等 TTL 配置自洽。
func TestValidateBootstrapConfigRejectsInvalidCollectorIdempotencyTTL(t *testing.T) {
	cfg := validEnabledCollectorConfig()
	cfg.Collector.Idempotency.TTLSeconds = 60
	cfg.Collector.Idempotency.ProcessingTTLSeconds = 120
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 processing_ttl_seconds 大于 ttl_seconds 返回错误，实际为 nil")
	}

	cfg.Collector.Idempotency.ProcessingTTLSeconds = -1
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 processing_ttl_seconds 为负数返回错误，实际为 nil")
	}

	cfg = validEnabledCollectorConfig()
	cfg.Collector.Idempotency.PipelineBatchSize = -1
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 pipeline_batch_size 为负数返回错误，实际为 nil")
	}

	cfg = validEnabledCollectorConfig()
	cfg.Collector.Idempotency.PipelineBatchSize = maxCollectorIdempotencyPipelineBatchSize + 1
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 pipeline_batch_size 超限返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidCollectorTaskPolicy 确保 Collector 任务聚合策略配置自洽。
func TestValidateBootstrapConfigRejectsInvalidCollectorTaskPolicy(t *testing.T) {
	cfg := validEnabledCollectorConfig()
	cfg.Collector.DefaultTask.BatchSize = -1
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 default_task.batch_size 为负数返回错误，实际为 nil")
	}

	cfg = validEnabledCollectorConfig()
	cfg.Collector.Tasks = map[string]config.CollectorTaskConfig{
		" ": {BatchSize: 200},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 collector.tasks 空 bizType 返回错误，实际为 nil")
	}

	cfg = validEnabledCollectorConfig()
	cfg.Collector.Tasks = map[string]config.CollectorTaskConfig{
		"admin_log": {BatchWaitMilliseconds: maxCollectorTaskBatchWaitMilliseconds + 1},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 task batch_wait_milliseconds 超限返回错误，实际为 nil")
	}
}

// validEnabledCollectorConfig 返回启用且满足基础路由的 Collector 测试配置。
func validEnabledCollectorConfig() config.Config {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled: true,
		Kafka: config.CollectorKafkaConfig{
			Brokers: []string{"127.0.0.1:9092"},
		},
		DefaultTask: config.CollectorTaskConfig{
			Topic:   "collector_events",
			GroupID: "collector_group",
		},
	}
	return cfg
}
