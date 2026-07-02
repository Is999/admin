package validators

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigRejectsInvalidCollectorTransport 确保 Collector 载体只能使用受支持枚举。
func TestValidateBootstrapConfigRejectsInvalidCollectorTransport(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector.Transport = "amqp"
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望非法 collector.transport 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsForeignCollectorStream 确保 Collector 不会误用其它站点 Redis Stream。
func TestValidateBootstrapConfigRejectsForeignCollectorStream(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = "site-2"
	cfg.Collector.Redis.Stream = "app:site-1:collector:events"
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望其它 app_id 的 collector.redis.stream 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsForcedKafkaWithoutTopic 确保强制 Kafka 载体时必须具备可投递配置。
func TestValidateBootstrapConfigRejectsForcedKafkaWithoutTopic(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled:   true,
		Transport: "kafka",
		Kafka: config.CollectorKafkaConfig{
			Enabled: true,
			Brokers: []string{"127.0.0.1:9092"},
		},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 collector.kafka.topic 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsCollectorKafkaWithoutGroup 确保启用 Collector 后 Kafka 消费组不可缺失。
func TestValidateBootstrapConfigRejectsCollectorKafkaWithoutGroup(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled:   true,
		Transport: "kafka",
		Kafka: config.CollectorKafkaConfig{
			Enabled: true,
			Brokers: []string{"127.0.0.1:9092"},
			Topic:   "collector_events",
		},
	}
	if err := ValidateCollector(cfg); err == nil {
		t.Fatal("期望 collector.kafka.group_id 缺失返回错误，实际为 nil")
	}
}
