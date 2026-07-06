package collectorx

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Collector 指标变量集中定义 Kafka 投递消费、失败账本、Processor 批量处理和结果统计。
var (
	collectorMetricsOnce sync.Once // 保证 Collector 指标只注册一次
	// collectorMetricBizTypeGuard 保护 Collector biz_type 指标标签白名单。
	collectorMetricBizTypeGuard = struct {
		mu      sync.RWMutex        // 保护 biz_type label 集合，避免高基数维度无限增长
		allowed map[string]struct{} // 已明确放行的业务类型标签
	}{
		allowed: make(map[string]struct{}),
	}

	// collectorKafkaPublishEventsTotal 统计 Collector Kafka 投递事件数量。
	collectorKafkaPublishEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "kafka_publish_events_total",
			Help:      "Collector Kafka 投递事件累计数量。",
		},
		[]string{"result"},
	)
	// collectorKafkaConsumeEventsTotal 统计 Collector Kafka 消费事件数量。
	collectorKafkaConsumeEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "kafka_consume_events_total",
			Help:      "Collector Kafka 消费事件累计数量。",
		},
		[]string{"result"},
	)
	// collectorFailurePersistEventsTotal 统计 Collector 写入失败账本的事件数量。
	collectorFailurePersistEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "failure_persist_events_total",
			Help:      "Collector 写入失败账本的事件累计数量。",
		},
		[]string{"state"},
	)
	// collectorFailurePersistDuration 统计 Collector 写入失败账本的耗时分布。
	collectorFailurePersistDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "failure_persist_duration_seconds",
			Help:      "Collector 批量写入失败账本的耗时分布。",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
		},
		[]string{"state"},
	)
	// collectorFailureDeadEventsTotal 统计 Collector 进入死信的失败事件数量。
	collectorFailureDeadEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "failure_dead_events_total",
			Help:      "Collector 进入死信的失败事件累计数量。",
		},
		[]string{"biz_type"},
	)
	// collectorProcessorBatchSize 统计 Collector Processor 单批事件数量。
	collectorProcessorBatchSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "processor_batch_size",
			Help:      "Collector 单次 Processor 批量处理的事件数量分布。",
			Buckets:   []float64{1, 5, 10, 20, 50, 100, 200, 500, 1000},
		},
		[]string{"biz_type"},
	)
	// collectorProcessorBatchDuration 统计 Collector Processor 单批处理耗时。
	collectorProcessorBatchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "processor_batch_duration_seconds",
			Help:      "Collector 单次 Processor 批量处理耗时分布。",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"biz_type", "result"},
	)
	// collectorProcessorEventsTotal 统计进入 Collector Processor 后的事件结果数量。
	collectorProcessorEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "processor_events_total",
			Help:      "Collector 进入 Processor 处理后的事件累计数量。",
		},
		[]string{"biz_type", "result"},
	)
	// authSecurityEventsTotal 统计前台认证风控事件数量。
	authSecurityEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "auth_security",
			Name:      "events_total",
			Help:      "前台认证风控事件累计数量。",
		},
		[]string{"app_id", "action", "reason", "category"},
	)
)

const (
	maxCollectorMetricBizTypeLabels = 64      // 限制动态 biz_type label 数量，避免 Prometheus 指标维度失控
	collectorMetricBizTypeOther     = "other" // 超出上限或未知 bizType 统一归并到 other
)

// ensureMetricsRegistered 保证 Prometheus 指标只注册一次，避免测试和多实例初始化时重复注册 panic。
func ensureMetricsRegistered() {
	collectorMetricsOnce.Do(func() {
		prometheus.MustRegister(
			collectorKafkaPublishEventsTotal,
			collectorKafkaConsumeEventsTotal,
			collectorFailurePersistEventsTotal,
			collectorFailurePersistDuration,
			collectorFailureDeadEventsTotal,
			collectorProcessorBatchSize,
			collectorProcessorBatchDuration,
			collectorProcessorEventsTotal,
			authSecurityEventsTotal,
		)
	})
}

// normalizeMetricLabel 统一规整指标 label，避免空值和无序输入污染指标维度。
func normalizeMetricLabel(text string, fallback string) string {
	value := strings.TrimSpace(text)
	if value == "" {
		return fallback
	}
	return value
}

// allowMetricBizTypeLabel 显式登记允许暴露的 bizType 指标维度，优先用于业务正式注册的 Processor。
func allowMetricBizTypeLabel(bizType string) {
	value := strings.TrimSpace(bizType)
	if value == "" {
		return
	}
	collectorMetricBizTypeGuard.mu.Lock()
	defer collectorMetricBizTypeGuard.mu.Unlock()
	collectorMetricBizTypeGuard.allowed[value] = struct{}{}
}

// normalizeBizTypeMetricLabel 对 biz_type 指标维度做白名单和上限保护，避免高基数冲垮指标系统。
func normalizeBizTypeMetricLabel(bizType string) string {
	value := normalizeMetricLabel(bizType, "unknown")
	if value == "unknown" {
		return value
	}
	collectorMetricBizTypeGuard.mu.RLock()
	_, ok := collectorMetricBizTypeGuard.allowed[value]
	currentSize := len(collectorMetricBizTypeGuard.allowed)
	collectorMetricBizTypeGuard.mu.RUnlock()
	if ok {
		return value
	}
	collectorMetricBizTypeGuard.mu.Lock()
	defer collectorMetricBizTypeGuard.mu.Unlock()
	if _, ok = collectorMetricBizTypeGuard.allowed[value]; ok {
		return value
	}
	if len(collectorMetricBizTypeGuard.allowed) >= maxCollectorMetricBizTypeLabels && currentSize >= maxCollectorMetricBizTypeLabels {
		return collectorMetricBizTypeOther
	}
	collectorMetricBizTypeGuard.allowed[value] = struct{}{}
	return value
}

// recordKafkaPublish 记录 Kafka 投递结果。
func recordKafkaPublish(result string, count int) {
	if count <= 0 {
		return
	}
	ensureMetricsRegistered()
	collectorKafkaPublishEventsTotal.WithLabelValues(normalizeMetricLabel(result, "unknown")).Add(float64(count))
}

// recordKafkaConsume 记录 Kafka 消费处理结果。
func recordKafkaConsume(result string, count int) {
	if count <= 0 {
		return
	}
	ensureMetricsRegistered()
	collectorKafkaConsumeEventsTotal.WithLabelValues(normalizeMetricLabel(result, "unknown")).Add(float64(count))
}

// recordFailurePersistBatch 记录一次批量写失败账本的事件数量和耗时。
func recordFailurePersistBatch(state string, count int, duration time.Duration) {
	if count <= 0 {
		return
	}
	ensureMetricsRegistered()
	stateLabel := normalizeMetricLabel(state, "unknown")
	collectorFailurePersistEventsTotal.WithLabelValues(stateLabel).Add(float64(count))
	collectorFailurePersistDuration.WithLabelValues(stateLabel).Observe(duration.Seconds())
}

// recordFailureDead 记录失败事件进入死信的数量。
func recordFailureDead(bizType string, count int) {
	if count <= 0 {
		return
	}
	ensureMetricsRegistered()
	collectorFailureDeadEventsTotal.WithLabelValues(normalizeBizTypeMetricLabel(bizType)).Add(float64(count))
}

// recordProcessorBatch 记录一次 Processor 批处理的批次规模、耗时和成功/失败事件数。
func recordProcessorBatch(bizType string, batchSize int, successCount int, failCount int, duration time.Duration) {
	if batchSize <= 0 {
		return
	}
	ensureMetricsRegistered()
	label := normalizeBizTypeMetricLabel(bizType)
	collectorProcessorBatchSize.WithLabelValues(label).Observe(float64(batchSize))
	result := "success"
	switch {
	case failCount > 0 && successCount > 0:
		result = "partial"
	case failCount > 0:
		result = "failed"
	}
	collectorProcessorBatchDuration.WithLabelValues(label, result).Observe(duration.Seconds())
	if successCount > 0 {
		collectorProcessorEventsTotal.WithLabelValues(label, "success").Add(float64(successCount))
	}
	if failCount > 0 {
		collectorProcessorEventsTotal.WithLabelValues(label, "failed").Add(float64(failCount))
	}
}

// recordAuthSecurityEvent 记录认证风控事件聚合指标。
func recordAuthSecurityEvent(appID string, action string, reason string) {
	ensureMetricsRegistered()
	normalizedReason := normalizeAuthSecurityReason(reason)
	authSecurityEventsTotal.WithLabelValues(
		normalizeAuthSecurityAppID(appID),
		normalizeAuthSecurityAction(action),
		normalizedReason,
		normalizeAuthSecurityCategory(reason),
	).Inc()
}
