package collectorx

import (
	"sort"
	"sync"
	"time"
)

const (
	collectorRuntimeWindowSeconds     = 15 * 60           // 运行态指标保留最近 15 分钟滑动窗口
	collectorRuntimeTopLimit          = 10                // 管理端展示最近窗口内最活跃的任务数量
	collectorRuntimeMaxBizTypesBucket = 128               // 单秒桶内最多保留的业务类型数量，防止异常高基数撑爆内存
	collectorRuntimeScopeCurrent      = "current_process" // 运行态指标为当前进程内存快照
)

// RuntimeMetricTotals 表示 Collector 运行期累计计数。
type RuntimeMetricTotals struct {
	Published     int64 // Kafka 投递成功事件数
	PublishFailed int64 // Kafka 投递失败事件数
	Consumed      int64 // Kafka 已拉取事件数
	Invalid       int64 // Kafka 坏消息事件数
	Duplicate     int64 // Collector 幂等去重跳过事件数
	Processed     int64 // 进入 Processor 的事件数
	Succeeded     int64 // Processor 成功事件数
	Failed        int64 // Processor 失败事件数
	Batches       int64 // Processor 批次数
	FailedBatches int64 // 存在失败事件的 Processor 批次数
	Dead          int64 // 进入死信的事件数
}

// RuntimeMetricWindow 表示 Collector 运行态滑动窗口统计。
type RuntimeMetricWindow struct {
	WindowMinutes       int       // 统计窗口分钟数
	RuntimeMetricTotals           // 窗口内累计计数
	AvgBatchSize        float64   // 平均批次大小
	AvgCostMs           float64   // 平均 Processor 批处理耗时毫秒
	MaxCostMs           float64   // 最大 Processor 批处理耗时毫秒
	LastEventAt         time.Time // 窗口内最后一次指标更新时间
}

// RuntimeBizTypeMetric 表示单个 bizType 的运行态热点统计。
type RuntimeBizTypeMetric struct {
	BizType             string    // 业务类型
	RuntimeMetricTotals           // 当前业务类型累计计数
	AvgBatchSize        float64   // 平均批次大小
	AvgCostMs           float64   // 平均 Processor 批处理耗时毫秒
	MaxCostMs           float64   // 最大 Processor 批处理耗时毫秒
	LastEventAt         time.Time // 最近一次指标更新时间
}

// RuntimeMetricsSnapshot 表示 Collector 当前进程的运行态指标快照。
type RuntimeMetricsSnapshot struct {
	Enabled       bool                   // Collector 是否启用
	Scope         string                 // 指标作用域，当前为 current_process
	StartedAt     time.Time              // 运行态统计器创建时间
	GeneratedAt   time.Time              // 快照生成时间
	Totals        RuntimeMetricTotals    // 当前进程累计计数
	Recent1m      RuntimeMetricWindow    // 最近 1 分钟窗口
	Recent5m      RuntimeMetricWindow    // 最近 5 分钟窗口
	Recent15m     RuntimeMetricWindow    // 最近 15 分钟窗口
	BizTypeTop15m []RuntimeBizTypeMetric // 最近 15 分钟任务热点排行
}

// collectorRuntimeStats 维护 Collector 当前进程的轻量运行态指标。
type collectorRuntimeStats struct {
	mu        sync.RWMutex             // 保护滑动窗口和累计计数
	startedAt time.Time                // 统计器创建时间
	buckets   []collectorRuntimeBucket // 按秒滚动的固定窗口桶
	totals    collectorRuntimeCounters // 当前进程累计计数
}

// collectorRuntimeBucket 表示运行态指标中的单秒聚合桶。
type collectorRuntimeBucket struct {
	unix  int64                               // 桶对应的 Unix 秒
	total collectorRuntimeCounters            // 当前秒总计数
	biz   map[string]collectorRuntimeCounters // 当前秒按 bizType 聚合的计数
}

// collectorRuntimeCounters 表示运行态指标的内部计数。
type collectorRuntimeCounters struct {
	published     int64   // Kafka 投递成功事件数
	publishFailed int64   // Kafka 投递失败事件数
	consumed      int64   // Kafka 已拉取事件数
	invalid       int64   // Kafka 坏消息事件数
	duplicate     int64   // Collector 幂等去重跳过事件数
	processed     int64   // 进入 Processor 的事件数
	succeeded     int64   // Processor 成功事件数
	failed        int64   // Processor 失败事件数
	batches       int64   // Processor 批次数
	failedBatches int64   // 存在失败事件的 Processor 批次数
	dead          int64   // 进入死信的事件数
	batchSizeSum  int64   // 批次大小累计值
	costMsSum     float64 // Processor 批处理耗时累计毫秒
	maxCostMs     float64 // Processor 批处理最大耗时毫秒
	lastEventUnix int64   // 最近一次指标更新时间
}

// newCollectorRuntimeStats 创建固定大小滑动窗口统计器。
func newCollectorRuntimeStats() *collectorRuntimeStats {
	return &collectorRuntimeStats{
		startedAt: time.Now(),
		buckets:   make([]collectorRuntimeBucket, collectorRuntimeWindowSeconds),
	}
}

// recordRuntimeKafkaPublish 记录 Kafka 投递结果。
func (m *Manager) recordRuntimeKafkaPublish(bizType string, success bool) {
	if m == nil || m.runtimeStats == nil {
		return
	}
	delta := collectorRuntimeCounters{}
	if success {
		delta.published = 1
	} else {
		delta.publishFailed = 1
	}
	m.runtimeStats.add(time.Now(), bizType, delta)
}

// recordRuntimeKafkaConsume 记录 Kafka 拉取和坏消息数量。
func (m *Manager) recordRuntimeKafkaConsume(bizType string, consumed int, invalid int) {
	if m == nil || m.runtimeStats == nil {
		return
	}
	delta := collectorRuntimeCounters{
		consumed: int64(consumed),
		invalid:  int64(invalid),
	}
	m.runtimeStats.add(time.Now(), bizType, delta)
}

// recordRuntimeDuplicate 记录 Collector 幂等去重跳过的事件数量。
func (m *Manager) recordRuntimeDuplicate(bizType string, count int) {
	if m == nil || m.runtimeStats == nil || count <= 0 {
		return
	}
	m.runtimeStats.add(time.Now(), bizType, collectorRuntimeCounters{duplicate: int64(count)})
}

// recordProcessorBatchMetrics 同步记录 Prometheus 和运行态 Processor 批处理指标。
func (m *Manager) recordProcessorBatchMetrics(bizType string, batchSize int, successCount int, failCount int, duration time.Duration) {
	recordProcessorBatch(bizType, batchSize, successCount, failCount, duration)
	if m == nil || m.runtimeStats == nil || batchSize <= 0 {
		return
	}
	delta := collectorRuntimeCounters{
		processed:    int64(batchSize),
		succeeded:    int64(successCount),
		failed:       int64(failCount),
		batches:      1,
		batchSizeSum: int64(batchSize),
		costMsSum:    float64(duration.Microseconds()) / 1000,
		maxCostMs:    float64(duration.Microseconds()) / 1000,
	}
	if failCount > 0 {
		delta.failedBatches = 1
	}
	m.runtimeStats.add(time.Now(), bizType, delta)
}

// recordRuntimeDead 记录进入死信的事件数量。
func (m *Manager) recordRuntimeDead(bizType string, count int) {
	if m == nil || m.runtimeStats == nil || count <= 0 {
		return
	}
	m.runtimeStats.add(time.Now(), bizType, collectorRuntimeCounters{dead: int64(count)})
}

// kafkaBatchBizType 返回 Kafka 批次的代表业务类型，用于运行态消费指标聚合。
func kafkaBatchBizType(batch kafkaBatch) string {
	if len(batch.events) > 0 {
		return batch.events[0].BizType
	}
	if len(batch.invalid) > 0 {
		return batch.invalid[0].event.BizType
	}
	return ""
}

// RuntimeMetricsSnapshot 返回 Collector 当前进程运行态指标快照。
func (m *Manager) RuntimeMetricsSnapshot() RuntimeMetricsSnapshot {
	now := time.Now()
	if m == nil || m.runtimeStats == nil {
		return emptyRuntimeMetricsSnapshot(now)
	}
	snapshot := m.runtimeStats.snapshot(now)
	snapshot.Enabled = m.cfg.Enabled
	return snapshot
}

// add 将一条指标增量写入总计数和当前秒窗口桶。
func (s *collectorRuntimeStats) add(now time.Time, bizType string, delta collectorRuntimeCounters) {
	if s == nil || delta.empty() {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	unix := now.Unix()
	delta.lastEventUnix = unix

	s.mu.Lock()
	defer s.mu.Unlock()

	bucket := &s.buckets[unix%int64(len(s.buckets))]
	if bucket.unix != unix {
		bucket.unix = unix
		bucket.total = collectorRuntimeCounters{}
		bucket.biz = nil
	}
	bucket.total.add(delta)
	s.totals.add(delta)

	if bizType = normalizeMetricLabel(bizType, ""); bizType == "" {
		return
	}
	if bucket.biz == nil {
		bucket.biz = make(map[string]collectorRuntimeCounters)
	}
	if _, ok := bucket.biz[bizType]; !ok && len(bucket.biz) >= collectorRuntimeMaxBizTypesBucket {
		bizType = collectorMetricBizTypeOther
	}
	current := bucket.biz[bizType]
	current.add(delta)
	bucket.biz[bizType] = current
}

// snapshot 生成当前运行态指标快照。
func (s *collectorRuntimeStats) snapshot(now time.Time) RuntimeMetricsSnapshot {
	if s == nil {
		return emptyRuntimeMetricsSnapshot(now)
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return RuntimeMetricsSnapshot{
		Enabled:       true,
		Scope:         collectorRuntimeScopeCurrent,
		StartedAt:     s.startedAt,
		GeneratedAt:   now,
		Totals:        runtimeTotalsFromCounters(s.totals),
		Recent1m:      s.windowLocked(now, time.Minute, 1),
		Recent5m:      s.windowLocked(now, 5*time.Minute, 5),
		Recent15m:     s.windowLocked(now, 15*time.Minute, 15),
		BizTypeTop15m: s.bizTypeTopLocked(now, 15*time.Minute),
	}
}

// windowLocked 聚合指定时间窗口，调用方必须持有读锁。
func (s *collectorRuntimeStats) windowLocked(now time.Time, window time.Duration, minutes int) RuntimeMetricWindow {
	total := collectorRuntimeCounters{}
	since := now.Add(-window).Unix() + 1
	until := now.Unix()
	for i := range s.buckets {
		bucket := s.buckets[i]
		if bucket.unix < since || bucket.unix > until {
			continue
		}
		total.add(bucket.total)
	}
	return runtimeWindowFromCounters(minutes, total)
}

// bizTypeTopLocked 聚合最近窗口内最活跃的 bizType，调用方必须持有读锁。
func (s *collectorRuntimeStats) bizTypeTopLocked(now time.Time, window time.Duration) []RuntimeBizTypeMetric {
	since := now.Add(-window).Unix() + 1
	until := now.Unix()
	totals := make(map[string]collectorRuntimeCounters)
	for i := range s.buckets {
		bucket := s.buckets[i]
		if bucket.unix < since || bucket.unix > until {
			continue
		}
		for bizType, counter := range bucket.biz {
			current := totals[bizType]
			current.add(counter)
			totals[bizType] = current
		}
	}
	items := make([]RuntimeBizTypeMetric, 0, len(totals))
	for bizType, counter := range totals {
		items = append(items, runtimeBizTypeMetricFromCounters(bizType, counter))
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.Processed != right.Processed {
			return left.Processed > right.Processed
		}
		if left.Published != right.Published {
			return left.Published > right.Published
		}
		if left.Failed != right.Failed {
			return left.Failed > right.Failed
		}
		if left.Batches != right.Batches {
			return left.Batches > right.Batches
		}
		return left.BizType < right.BizType
	})
	if len(items) > collectorRuntimeTopLimit {
		items = items[:collectorRuntimeTopLimit]
	}
	return items
}

// add 累加计数并维护最大耗时与最近事件时间。
func (c *collectorRuntimeCounters) add(delta collectorRuntimeCounters) {
	c.published += delta.published
	c.publishFailed += delta.publishFailed
	c.consumed += delta.consumed
	c.invalid += delta.invalid
	c.duplicate += delta.duplicate
	c.processed += delta.processed
	c.succeeded += delta.succeeded
	c.failed += delta.failed
	c.batches += delta.batches
	c.failedBatches += delta.failedBatches
	c.dead += delta.dead
	c.batchSizeSum += delta.batchSizeSum
	c.costMsSum += delta.costMsSum
	if delta.maxCostMs > c.maxCostMs {
		c.maxCostMs = delta.maxCostMs
	}
	if delta.lastEventUnix > c.lastEventUnix {
		c.lastEventUnix = delta.lastEventUnix
	}
}

// empty 判断计数增量是否为空。
func (c collectorRuntimeCounters) empty() bool {
	return c.published == 0 &&
		c.publishFailed == 0 &&
		c.consumed == 0 &&
		c.invalid == 0 &&
		c.duplicate == 0 &&
		c.processed == 0 &&
		c.succeeded == 0 &&
		c.failed == 0 &&
		c.batches == 0 &&
		c.failedBatches == 0 &&
		c.dead == 0
}

// runtimeTotalsFromCounters 转换累计计数。
func runtimeTotalsFromCounters(counter collectorRuntimeCounters) RuntimeMetricTotals {
	return RuntimeMetricTotals{
		Published:     counter.published,
		PublishFailed: counter.publishFailed,
		Consumed:      counter.consumed,
		Invalid:       counter.invalid,
		Duplicate:     counter.duplicate,
		Processed:     counter.processed,
		Succeeded:     counter.succeeded,
		Failed:        counter.failed,
		Batches:       counter.batches,
		FailedBatches: counter.failedBatches,
		Dead:          counter.dead,
	}
}

// runtimeWindowFromCounters 转换滑动窗口计数。
func runtimeWindowFromCounters(minutes int, counter collectorRuntimeCounters) RuntimeMetricWindow {
	window := RuntimeMetricWindow{
		WindowMinutes:       minutes,
		RuntimeMetricTotals: runtimeTotalsFromCounters(counter),
		MaxCostMs:           counter.maxCostMs,
		LastEventAt:         runtimeLastEventTime(counter.lastEventUnix),
	}
	if counter.batches > 0 {
		window.AvgBatchSize = float64(counter.batchSizeSum) / float64(counter.batches)
		window.AvgCostMs = counter.costMsSum / float64(counter.batches)
	}
	return window
}

// runtimeBizTypeMetricFromCounters 转换 bizType 热点计数。
func runtimeBizTypeMetricFromCounters(bizType string, counter collectorRuntimeCounters) RuntimeBizTypeMetric {
	item := RuntimeBizTypeMetric{
		BizType:             bizType,
		RuntimeMetricTotals: runtimeTotalsFromCounters(counter),
		MaxCostMs:           counter.maxCostMs,
		LastEventAt:         runtimeLastEventTime(counter.lastEventUnix),
	}
	if counter.batches > 0 {
		item.AvgBatchSize = float64(counter.batchSizeSum) / float64(counter.batches)
		item.AvgCostMs = counter.costMsSum / float64(counter.batches)
	}
	return item
}

// runtimeLastEventTime 把内部 Unix 秒转换为时间。
func runtimeLastEventTime(unix int64) time.Time {
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

// emptyRuntimeMetricsSnapshot 返回空运行态快照，便于 API 层稳定序列化。
func emptyRuntimeMetricsSnapshot(now time.Time) RuntimeMetricsSnapshot {
	if now.IsZero() {
		now = time.Now()
	}
	return RuntimeMetricsSnapshot{
		Scope:         collectorRuntimeScopeCurrent,
		GeneratedAt:   now,
		Recent1m:      RuntimeMetricWindow{WindowMinutes: 1},
		Recent5m:      RuntimeMetricWindow{WindowMinutes: 5},
		Recent15m:     RuntimeMetricWindow{WindowMinutes: 15},
		BizTypeTop15m: make([]RuntimeBizTypeMetric, 0),
	}
}
