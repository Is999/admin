package validators

import (
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const (
	maxCollectorTaskBatchWaitMilliseconds    = 60000   // Collector 单任务聚合等待上限，避免误配导致 partition 长时间阻塞
	maxCollectorIdempotencyPipelineBatchSize = 1000    // Collector Redis 去重 Pipeline 命令数上限，避免单次 Redis 执行过重
	maxCollectorBatchSize                    = 5000    // Collector 单批载体上限
	maxCollectorTimeoutSeconds               = 3600    // Kafka、Processor 和失败租约最大秒数
	minCollectorLeaseSeconds                 = 3       // 租约最小秒数，为续租和状态 CAS 留出窗口
	maxCollectorRetryTimes                   = 100     // 失败账本最大重试次数
	maxCollectorIdempotencyTTLSeconds        = 2678400 // Redis 幂等终态最长保留 31 天
	maxCollectorProcessingTTLSeconds         = 86400   // Redis processing 租约最长 24 小时
)

// ValidateCollector 校验 Collector Kafka 单通道和失败账本配置是否自洽。
func ValidateCollector(c config.Config) error {
	cfg := c.Collector
	if !cfg.Enabled {
		return nil
	}
	if len(nonEmptyStrings(cfg.Kafka.Brokers)) == 0 {
		return errors.Errorf("collector.enabled=true 时必须配置 collector.kafka.brokers 或顶层 kafka.brokers")
	}
	if err := validateCollectorKafka(cfg.Kafka); err != nil {
		return errors.Tag(err)
	}
	if err := validateCollectorFailureRetry(cfg.FailureRetry); err != nil {
		return errors.Tag(err)
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
	if cfg.Idempotency.TTLSeconds > maxCollectorIdempotencyTTLSeconds {
		return errors.Errorf("collector.idempotency.ttl_seconds 不能大于 %d", maxCollectorIdempotencyTTLSeconds)
	}
	if cfg.Idempotency.ProcessingTTLSeconds < 0 {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 不能小于 0")
	}
	if cfg.Idempotency.ProcessingTTLSeconds > 0 && cfg.Idempotency.ProcessingTTLSeconds < minCollectorLeaseSeconds {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 必须为 0 或不小于 %d", minCollectorLeaseSeconds)
	}
	if cfg.Idempotency.ProcessingTTLSeconds > maxCollectorProcessingTTLSeconds {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 不能大于 %d", maxCollectorProcessingTTLSeconds)
	}
	if cfg.Idempotency.PipelineBatchSize < 0 {
		return errors.Errorf("collector.idempotency.pipeline_batch_size 不能小于 0")
	}
	if cfg.Idempotency.PipelineBatchSize > maxCollectorIdempotencyPipelineBatchSize {
		return errors.Errorf("collector.idempotency.pipeline_batch_size 不能大于 %d", maxCollectorIdempotencyPipelineBatchSize)
	}
	ttlSeconds := cfg.Idempotency.TTLSeconds
	if ttlSeconds == 0 {
		ttlSeconds = config.DefaultCollectorIdempotencyTTLSeconds
	}
	processingTTLSeconds := cfg.Idempotency.ProcessingTTLSeconds
	if processingTTLSeconds == 0 {
		processingTTLSeconds = config.DefaultCollectorIdempotencyProcessingTTLSeconds
	}
	if processingTTLSeconds > ttlSeconds {
		return errors.Errorf("collector.idempotency.processing_ttl_seconds 不能大于 collector.idempotency.ttl_seconds")
	}
	return nil
}

// validateCollectorKafka 校验 Kafka 批量与超时参数硬上限。
func validateCollectorKafka(cfg config.CollectorKafkaConfig) error {
	if cfg.WriteBatchSize < 0 || cfg.WriteBatchSize > maxCollectorBatchSize {
		return errors.Errorf("collector.kafka.write_batch_size 必须在 0-%d 之间", maxCollectorBatchSize)
	}
	if cfg.WriteBatchWaitMilliseconds < 0 || cfg.WriteBatchWaitMilliseconds > 5000 {
		return errors.Errorf("collector.kafka.write_batch_wait_milliseconds 必须在 0-5000 之间")
	}
	if cfg.WriteTimeout < 0 || cfg.WriteTimeout > maxCollectorTimeoutSeconds {
		return errors.Errorf("collector.kafka.write_timeout 必须在 0-%d 之间", maxCollectorTimeoutSeconds)
	}
	if cfg.ReadTimeout < 0 || cfg.ReadTimeout > maxCollectorTimeoutSeconds {
		return errors.Errorf("collector.kafka.read_timeout 必须在 0-%d 之间", maxCollectorTimeoutSeconds)
	}
	return nil
}

// validateCollectorFailureRetry 校验失败账本领取规模、轮询、租约和重试上限。
func validateCollectorFailureRetry(cfg config.CollectorFailureRetryConfig) error {
	if cfg.RunnerBatchSize < 0 || cfg.RunnerBatchSize > maxCollectorBatchSize {
		return errors.Errorf("collector.failure_retry.runner_batch_size 必须在 0-%d 之间", maxCollectorBatchSize)
	}
	if cfg.RunnerIntervalSeconds < 0 || cfg.RunnerIntervalSeconds > maxCollectorTimeoutSeconds {
		return errors.Errorf("collector.failure_retry.runner_interval_seconds 必须在 0-%d 之间", maxCollectorTimeoutSeconds)
	}
	if cfg.RunningLeaseSeconds < 0 || cfg.RunningLeaseSeconds > maxCollectorTimeoutSeconds {
		return errors.Errorf("collector.failure_retry.running_lease_seconds 必须在 0-%d 之间", maxCollectorTimeoutSeconds)
	}
	if cfg.RunningLeaseSeconds > 0 && cfg.RunningLeaseSeconds < minCollectorLeaseSeconds {
		return errors.Errorf("collector.failure_retry.running_lease_seconds 必须为 0 或不小于 %d", minCollectorLeaseSeconds)
	}
	if cfg.MaxRetryTimes < 0 || cfg.MaxRetryTimes > maxCollectorRetryTimes {
		return errors.Errorf("collector.failure_retry.max_retry_times 必须在 0-%d 之间", maxCollectorRetryTimes)
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
	if task.BatchSize < 0 || task.BatchSize > maxCollectorBatchSize {
		return errors.Errorf("%s.batch_size 必须在 0-%d 之间", path, maxCollectorBatchSize)
	}
	if task.BatchWaitMilliseconds < 0 {
		return errors.Errorf("%s.batch_wait_milliseconds 不能小于 0", path)
	}
	if task.BatchWaitMilliseconds > maxCollectorTaskBatchWaitMilliseconds {
		return errors.Errorf("%s.batch_wait_milliseconds 不能大于 %d", path, maxCollectorTaskBatchWaitMilliseconds)
	}
	if task.ProcessorTimeoutSeconds < 0 || task.ProcessorTimeoutSeconds > maxCollectorTimeoutSeconds {
		return errors.Errorf("%s.processor_timeout_seconds 必须在 0-%d 之间", path, maxCollectorTimeoutSeconds)
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
