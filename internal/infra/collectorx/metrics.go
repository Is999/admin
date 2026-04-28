package collectorx

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	collectorMetricsOnce        sync.Once
	collectorMetricBizTypeGuard = struct {
		mu      sync.RWMutex        // 保护 biz_type label 集合，避免高基数维度无限增长
		allowed map[string]struct{} // 已明确放行的业务类型标签
	}{
		allowed: make(map[string]struct{}),
	}

	collectorOutboxPersistEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "outbox_persist_events_total",
			Help:      "Collector 写入 outbox 的事件累计数量。",
		},
		[]string{"transport"},
	)
	collectorOutboxPersistDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "outbox_persist_duration_seconds",
			Help:      "Collector 批量写入 outbox 的耗时分布。",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
		},
		[]string{"transport"},
	)
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
	collectorProcessorEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "admin",
			Subsystem: "collector",
			Name:      "processor_events_total",
			Help:      "Collector 进入 Processor 处理后的事件累计数量。",
		},
		[]string{"biz_type", "result"},
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
			collectorOutboxPersistEventsTotal,
			collectorOutboxPersistDuration,
			collectorProcessorBatchSize,
			collectorProcessorBatchDuration,
			collectorProcessorEventsTotal,
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

// recordOutboxPersistBatch 记录一次批量写 outbox 的事件数量和耗时。
func recordOutboxPersistBatch(transport string, count int, duration time.Duration) {
	if count <= 0 {
		return
	}
	ensureMetricsRegistered()
	label := normalizeMetricLabel(transport, collectorTransportUnknown)
	collectorOutboxPersistEventsTotal.WithLabelValues(label).Add(float64(count))
	collectorOutboxPersistDuration.WithLabelValues(label).Observe(duration.Seconds())
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
