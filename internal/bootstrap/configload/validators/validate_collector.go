package validators

import (
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const maxCollectorTaskBatchWaitMilliseconds = 60000   // Collector 单任务聚合等待上限，避免误配导致 partition 长时间阻塞
const maxCollectorIdempotencyPipelineBatchSize = 1000 // Collector Redis 去重 Pipeline 命令数上限，避免单次 Redis 执行过重

// ValidateCollector 校验 Collector Kafka 单通道和失败账本配置是否自洽。
func ValidateCollector(c config.Config) error {
	cfg := c.Collector
	if !cfg.Enabled {
		return nil
	}
	if len(nonEmptyStrings(cfg.Kafka.Brokers)) == 0 {
		return errors.Errorf("collector.enabled=true 时必须配置 collector.kafka.brokers 或顶层 kafka.brokers")
	}
	if err := validateCollectorTaskConfig("collector.default_task", cfg.DefaultTask); err != nil {
		return errors.Tag(err)
	}
	topicGroups := make(map[string]string)
	if err := validateCollectorTaskRoute("collector.default_task", cfg.DefaultTask.Topic, cfg.DefaultTask.GroupID, topicGroups); err != nil {
		return errors.Tag(err)
	}
	for bizType, task := range cfg.Tasks {
		bizType = strings.TrimSpace(bizType)
		if bizType == "" {
			return errors.Errorf("collector.tasks 的 bizType 不能为空")
		}
		if err := validateCollectorTaskConfig("collector.tasks."+bizType, task); err != nil {
			return errors.Tag(err)
		}
		topic := firstNonEmpty(task.Topic, cfg.DefaultTask.Topic)
		groupID := firstNonEmpty(task.GroupID, cfg.DefaultTask.GroupID)
		if err := validateCollectorTaskRoute("collector.tasks."+bizType, topic, groupID, topicGroups); err != nil {
			return errors.Tag(err)
		}
	}
	if len(topicGroups) == 0 {
		return errors.Errorf("collector.enabled=true 时必须配置 collector.default_task.topic 或 collector.tasks.<bizType>.topic")
	}
	if cfg.Idempotency.TTLSeconds < 0 {
		return errors.Errorf("collector.idempotency.ttl_seconds 不能小于 0")
	}
	if cfg.Idempotency.ProcessingTTLSeconds < 0 {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 不能小于 0")
	}
	if cfg.Idempotency.PipelineBatchSize < 0 {
		return errors.Errorf("collector.idempotency.pipeline_batch_size 不能小于 0")
	}
	if cfg.Idempotency.PipelineBatchSize > maxCollectorIdempotencyPipelineBatchSize {
		return errors.Errorf("collector.idempotency.pipeline_batch_size 不能大于 %d", maxCollectorIdempotencyPipelineBatchSize)
	}
	if cfg.Idempotency.TTLSeconds > 0 &&
		cfg.Idempotency.ProcessingTTLSeconds > cfg.Idempotency.TTLSeconds {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 不能大于 collector.idempotency.ttl_seconds")
	}
	return nil
}

// validateCollectorTaskRoute 校验任务 Kafka Topic 和消费组配置。
func validateCollectorTaskRoute(path string, topic string, groupID string, topicGroups map[string]string) error {
	topic = strings.TrimSpace(topic)
	groupID = strings.TrimSpace(groupID)
	if topic == "" && groupID == "" {
		return nil
	}
	if topic == "" {
		return errors.Errorf("%s.topic 不能为空", path)
	}
	if groupID == "" {
		return errors.Errorf("%s.group_id 不能为空", path)
	}
	if existing, ok := topicGroups[topic]; ok && existing != groupID {
		return errors.Errorf("collector topic=%s 配置了多个 group_id: %s, %s", topic, existing, groupID)
	}
	topicGroups[topic] = groupID
	return nil
}

// validateCollectorTaskConfig 校验单个 Collector 任务聚合策略。
func validateCollectorTaskConfig(path string, task config.CollectorTaskConfig) error {
	if task.BatchSize < 0 {
		return errors.Errorf("%s.batch_size 不能小于 0", path)
	}
	if task.BatchWaitMilliseconds < 0 {
		return errors.Errorf("%s.batch_wait_milliseconds 不能小于 0", path)
	}
	if task.BatchWaitMilliseconds > maxCollectorTaskBatchWaitMilliseconds {
		return errors.Errorf("%s.batch_wait_milliseconds 不能大于 %d", path, maxCollectorTaskBatchWaitMilliseconds)
	}
	return nil
}

// firstNonEmpty 返回第一个非空白字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// nonEmptyStrings 过滤空白字符串，避免配置数组里的空项参与校验。
func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
