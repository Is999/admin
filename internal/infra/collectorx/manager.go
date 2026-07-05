package collectorx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"admin/internal/config"
	"admin/internal/infra/loggerx"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Collector 默认运行参数限制单轮处理规模、租约和慢批次日志阈值。
const (
	defaultFailureRetryBatchSize        = 500                    // 失败账本单轮领取的默认事件数量
	defaultFailureRetryIntervalSeconds  = 1                      // 失败账本空轮询后的默认等待秒数
	defaultFailureRunningLeaseSeconds   = 600                    // 失败账本运行中事件的默认租约秒数
	defaultFailureMaxRetryTimes         = 8                      // 失败事件默认最大重试次数
	defaultCollectorBatchSize           = 500                    // Collector 默认本地批量条数
	defaultCollectorBatchWait           = 20 * time.Millisecond  // Collector 默认本地聚合等待时长
	defaultKafkaWriteTimeout            = 10 * time.Second       // Kafka 默认写入超时时间
	defaultIdempotencyTTL               = time.Hour              // Collector Redis 幂等终态保留时间
	defaultIdempotencyProcessingTTL     = 10 * time.Minute       // Collector Redis 处理中占用租约时间
	defaultIdempotencyPipelineBatchSize = 200                    // Collector Redis 幂等 Pipeline 默认命令数
	maxCollectorTaskBatchWait           = time.Minute            // 单个 Collector 任务最大聚合等待时间
	maxCollectorCarrierBatchSize        = 5000                   // Collector 单批载体处理数量上限
	maxIdempotencyPipelineBatchSize     = 1000                   // Collector Redis 幂等 Pipeline 命令数上限
	maxInvalidKafkaPayloadBytes         = 4000                   // 坏消息失败账本保留的最大原始内容字节数
	slowCollectorBatchThreshold         = 200 * time.Millisecond // Collector 批量处理慢日志阈值
)

// kafkaMessageWriter 约束 Kafka 写入器，便于单测替换真实 broker。
type kafkaMessageWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

// Manager 负责通用收集器的 Kafka 投递、批量消费和失败账本重试。
type Manager struct {
	cfg          config.CollectorConfig        // 收集器运行配置
	failures     *gorm.DB                      // 失败账本连接，仅保存失败、死信和人工重试记录
	writers      map[string]kafkaMessageWriter // Kafka Topic 到写入器的映射
	runtimeStats *collectorRuntimeStats        // 当前进程运行态指标，仅保存聚合快照
	idempotency  *idempotencyStore             // Redis 单任务 EventID 幂等去重存储

	mu         sync.RWMutex         // 保护 processors 注册表
	processors map[string]Processor // bizType 到批量处理器的映射
	alertHook  AlertHook            // 后台运行异常告警钩子

	ctx    context.Context    // 后台 worker 生命周期上下文
	cancel context.CancelFunc // 后台 worker 停止函数
	wg     sync.WaitGroup     // 等待后台 worker 退出

	kafkaReaders []*kafka.Reader // Kafka 消费器列表，每个任务 Topic 一个 Reader
}

// kafkaBatch 表示一批 Kafka 消息及其解析后的有效事件和坏消息失败记录。
type kafkaBatch struct {
	messages []kafka.Message    // 本批需要统一提交的 Kafka 消息
	events   []Event            // 可进入 Processor 的有效事件
	invalid  []failureEventSeed // 不可恢复的坏消息，落失败账本后提交 offset
}

// kafkaWorkItem 表示 Kafka partition lane 内按 offset 排队的单条消息。
type kafkaWorkItem struct {
	message kafka.Message     // 原始 Kafka 消息，用于成功后提交 offset
	event   Event             // 解析成功的业务事件
	failure *failureEventSeed // 解析失败时写入失败账本的死信记录
}

// kafkaCommitFunc 串行提交 Kafka offset，避免多个 partition lane 并发调用 reader。
type kafkaCommitFunc func(context.Context, ...kafka.Message) error

// eventTaskGroup 表示按 bizType 聚合后的稳定任务批次。
type eventTaskGroup struct {
	bizType string  // 业务类型
	events  []Event // 同一业务类型下待处理事件
}

// collectorBatchPolicy 表示单个 bizType 在 partition lane 内的聚合触发策略。
type collectorBatchPolicy struct {
	batchSize int           // 同任务最大聚合条数
	batchWait time.Duration // 同任务未满批时最大等待时间
}

// collectorKafkaRoute 表示单个业务任务的 Kafka 投递和消费路由。
type collectorKafkaRoute struct {
	topic   string // Kafka Topic
	groupID string // Kafka 消费组
}

// failureEventSeed 表示待写入失败账本的事件快照。
type failureEventSeed struct {
	event     Event                           // 结构化事件；坏消息使用框架生成的合成事件
	state     model.CollectorFailedEventState // 写入失败账本的初始状态
	attempt   int                             // 已失败次数
	nextRunAt time.Time                       // 下次允许重试时间
	lastError string                          // 失败原因摘要
}

// collectorDeadSummary 表示一次回写中进入死信的事件摘要。
type collectorDeadSummary struct {
	count   int    // 进入死信的事件数量
	bizType string // 首个死信事件业务类型
	eventID string // 首个死信事件 ID
}

// New 创建通用收集器管理器，并按配置初始化 Kafka 写入器。
func New(cfg config.CollectorConfig, failureDB *gorm.DB, redisClient redis.UniversalClient) (*Manager, error) {
	if failureDB == nil {
		return nil, errors.Errorf("collector failureDB 为空")
	}
	if cfg.Enabled && collectorAnyIdempotencyEnabled(cfg) && redisClient == nil {
		return nil, errors.Errorf("collector.enabled=true 时必须提供 Redis 幂等存储")
	}
	ensureMetricsRegistered()
	normalizeCollectorTaskRoutes(&cfg)

	m := &Manager{
		cfg:          cfg,
		failures:     failureDB,
		writers:      make(map[string]kafkaMessageWriter),
		runtimeStats: newCollectorRuntimeStats(),
		idempotency: newIdempotencyStore(
			redisClient,
			collectorIdempotencyTTL(cfg.Idempotency),
			collectorIdempotencyProcessingTTL(cfg.Idempotency),
			collectorIdempotencyPipelineBatchSize(cfg.Idempotency),
		),
		processors: make(map[string]Processor),
	}

	if cfg.Enabled && len(cfg.Kafka.Brokers) > 0 {
		timeout := time.Duration(cfg.Kafka.WriteTimeout) * time.Second
		if timeout <= 0 {
			timeout = defaultKafkaWriteTimeout
		}
		batchSize := boundedPositiveInt(cfg.Kafka.WriteBatchSize, defaultCollectorBatchSize, maxCollectorCarrierBatchSize)
		for _, topic := range collectorConfiguredTopics(cfg) {
			m.writers[topic] = &kafka.Writer{
				Addr:         kafka.TCP(cfg.Kafka.Brokers...),
				Topic:        topic,
				Balancer:     &kafka.Hash{},
				BatchSize:    batchSize,
				BatchTimeout: m.kafkaWriteBatchWait(),
				WriteTimeout: timeout,
				RequiredAcks: kafka.RequireAll,
				Async:        false,
			}
		}
	}

	return m, nil
}

// SetAlertHook 设置 Collector 后台运行异常告警钩子，可接入 Lark 等外部告警系统。
func (m *Manager) SetAlertHook(hook AlertHook) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertHook = hook
}

// RegisterProcessor 注册指定 bizType 的批量消费处理器。
func (m *Manager) RegisterProcessor(bizType string, p Processor) error {
	if m == nil {
		return errors.Errorf("collector 未初始化")
	}
	bizType = strings.TrimSpace(bizType)
	if bizType == "" {
		return errors.Errorf("collectorx.RegisterProcessor bizType 为空")
	}
	if p == nil {
		return errors.Errorf("collectorx.RegisterProcessor processor 为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.processors[bizType]; ok {
		return errors.Errorf("collectorx.RegisterProcessor 重复注册 bizType=%s", bizType)
	}
	m.processors[bizType] = p
	allowMetricBizTypeLabel(bizType)
	return nil
}

// RegisterProcessorFunc 允许业务方直接传入批量消费函数，避免额外定义结构体。
func (m *Manager) RegisterProcessorFunc(bizType string, fn ProcessorFunc) error {
	if fn == nil {
		return errors.Errorf("collectorx.RegisterProcessorFunc processor 为空")
	}
	return errors.Tag(m.RegisterProcessor(bizType, fn))
}

// Start 启动通用收集器后台 worker。
func (m *Manager) Start() {
	if m == nil || !m.cfg.Enabled {
		return
	}
	if m.cancel != nil {
		return
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runFailureRetryWorker()
	}()

	if len(m.cfg.Kafka.Brokers) > 0 {
		for _, route := range collectorConfiguredReadRoutes(m.cfg) {
			reader := kafka.NewReader(kafka.ReaderConfig{
				Brokers: m.cfg.Kafka.Brokers,
				GroupID: route.groupID,
				Topic:   route.topic,
			})
			m.kafkaReaders = append(m.kafkaReaders, reader)
			topic := route.topic
			groupID := route.groupID
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				m.runKafkaWorker(reader, topic, groupID)
			}()
		}
	}
}

// Stop 停止通用收集器后台 worker，并关闭 Kafka 读写资源。
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.cancel != nil {
		m.cancel()
	}
	for _, reader := range m.kafkaReaders {
		_ = reader.Close()
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return errors.Tag(ctx.Err())
	}
	for _, writer := range m.writers {
		_ = writer.Close()
	}
	return nil
}

// RunNow 手动执行一轮失败账本重试，便于管理端立即处理已修复事件。
func (m *Manager) RunNow(ctx context.Context, limit int) (int, error) {
	if m == nil || !m.cfg.Enabled {
		return 0, errors.Errorf("collector 未启用")
	}
	return m.runFailureRetryOnce(ctx, limit)
}

// Enqueue 投递一条结构化业务事件。
// 正常链路只写 Kafka；Kafka 写入失败直接返回错误，由调用方决定是否同步重试或补偿。
func (m *Manager) Enqueue(ctx context.Context, event Event) (string, error) {
	if m == nil || !m.cfg.Enabled {
		return "", errors.Errorf("collector 未启用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := normalizeAndValidateEvent(&event); err != nil {
		return "", errors.Tag(err)
	}
	route := m.collectorTaskKafkaRoute(event.BizType)

	body, err := json.Marshal(event)
	if err != nil {
		return "", errors.Tag(err)
	}
	if !m.kafkaAvailable(route) {
		err = errors.Errorf("collector kafka topic 未配置 bizType=%s", event.BizType)
		recordKafkaPublish("failed", 1)
		m.recordRuntimeKafkaPublish(event.BizType, false)
		m.reportRuntimeAlert(ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindEnqueueFailed,
			Title:     "【P1 Collector Kafka 投递不可用】",
			Status:    "Kafka 未配置，事件未进入 Collector 正常链路",
			Component: "collector",
			Operation: "enqueue_kafka_unavailable",
			BizType:   event.BizType,
			Channel:   collectorRuntimeChannelKafka,
			UniqueKey: event.BizType,
			Reason:    collectorAlertReason("eventId", event.EventID, err.Error()),
			Advice:    "请检查 collector.kafka.brokers 以及 collector.tasks.<bizType>.topic 或 collector.default_task.topic。",
		})
		return "", err
	}
	if err = m.publishKafka(ctx, route.topic, kafkaMessageKey(event), body); err != nil {
		recordKafkaPublish("failed", 1)
		m.recordRuntimeKafkaPublish(event.BizType, false)
		m.reportRuntimeAlert(ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindEnqueueFailed,
			Title:     "【P1 Collector Kafka 投递失败】",
			Status:    "Kafka 写入失败，事件未进入 Collector 正常链路",
			Component: "collector",
			Operation: "enqueue_kafka_publish",
			BizType:   event.BizType,
			Channel:   collectorRuntimeChannelKafka,
			UniqueKey: route.topic,
			Reason:    collectorAlertReason("eventId", event.EventID, err.Error()),
			Advice:    "请检查 Kafka broker、topic、ACK 和网络状态；调用方应按自身语义重试或补偿。",
		})
		return "", errors.Tag(err)
	}
	recordKafkaPublish("success", 1)
	m.recordRuntimeKafkaPublish(event.BizType, true)
	return event.EventID, nil
}

// kafkaAvailable 判断指定任务 Kafka 路由是否具备写入条件。
func (m *Manager) kafkaAvailable(route collectorKafkaRoute) bool {
	if m == nil || strings.TrimSpace(route.topic) == "" {
		return false
	}
	return m.writers[route.topic] != nil
}

// publishKafka 将事件写入 Kafka，并等待 broker ACK。
func (m *Manager) publishKafka(ctx context.Context, topic string, key string, body []byte) error {
	writer := m.writers[strings.TrimSpace(topic)]
	if writer == nil {
		return errors.Errorf("collector kafka topic 未配置 topic=%s", topic)
	}
	timeout := time.Duration(m.cfg.Kafka.WriteTimeout) * time.Second
	if timeout <= 0 {
		timeout = defaultKafkaWriteTimeout
	}
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return errors.Tag(writer.WriteMessages(writeCtx, kafka.Message{Key: []byte(key), Value: body}))
}

// kafkaMessageKey 返回 Kafka 分区键，保证同任务同业务分区稳定进入同一 Kafka partition。
func kafkaMessageKey(event Event) string {
	bizType := strings.TrimSpace(event.BizType)
	partitionKey := strings.TrimSpace(event.PartitionKey)
	if partitionKey == "" {
		return bizType
	}
	return bizType + ":" + partitionKey
}

// processorFor 根据 bizType 查找已注册的批量处理器。
func (m *Manager) processorFor(bizType string) (Processor, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.processors[bizType]
	return p, p != nil
}

// processBatch 按 bizType 分组调用 Processor，并返回成功和失败事件集合。
func (m *Manager) processBatch(ctx context.Context, events []Event) (map[string]struct{}, map[string]string) {
	success := make(map[string]struct{}, len(events))
	failed := make(map[string]string)
	for _, task := range groupEventsByBizType(events) {
		bizType := task.bizType
		group := task.events
		beginAt := time.Now()
		groupSuccessCount := 0
		groupFailCount := 0
		p, ok := m.processorFor(bizType)
		if !ok {
			for _, event := range group {
				failed[event.EventID] = "collector 未注册 processor bizType=" + bizType
			}
			groupFailCount = len(group)
			m.reportRuntimeAlert(ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Processor 缺失】",
				Status:    "该业务类型事件无法消费，失败事件会写入失败账本",
				Component: "collector",
				Operation: "process_missing_processor",
				BizType:   bizType,
				Channel:   collectorRuntimeChannelKafka,
				UniqueKey: bizType,
				Reason:    "collector 未注册 processor bizType=" + bizType,
				Count:     len(group),
				Advice:    "请确认当前镜像是否注册了该 bizType 的 Processor，以及 collector.enabled、业务接入配置和 worker 部署版本是否一致。",
			})
			m.recordProcessorBatchMetrics(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
			continue
		}
		results, err := m.processBizBatch(ctx, bizType, p, group)
		if err != nil {
			for _, event := range group {
				failed[event.EventID] = err.Error()
			}
			groupFailCount = len(group)
			m.reportRuntimeAlert(ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector 批量消费失败】",
				Status:    "本批事件会写入失败账本，后续按重试策略处理",
				Component: "collector",
				Operation: "process_batch",
				BizType:   bizType,
				Channel:   collectorRuntimeChannelKafka,
				UniqueKey: bizType,
				Reason:    err.Error(),
				Count:     len(group),
				Advice:    "请检查对应 Processor 的业务依赖、数据格式和批量写入状态；若失败账本持续上涨，需要先暂停上游放量再处理死信。",
			})
			m.recordProcessorBatchMetrics(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
			continue
		}
		if len(results) == 0 {
			for _, event := range group {
				success[event.EventID] = struct{}{}
			}
			groupSuccessCount = len(group)
			m.recordProcessorBatchMetrics(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
			continue
		}
		pending := make(map[string]struct{}, len(group))
		for _, event := range group {
			pending[event.EventID] = struct{}{}
		}
		for _, result := range results {
			eventID := strings.TrimSpace(result.EventID)
			if eventID == "" {
				continue
			}
			if _, ok := pending[eventID]; !ok {
				continue
			}
			delete(pending, eventID)
			if result.Success {
				success[eventID] = struct{}{}
				delete(failed, eventID)
				groupSuccessCount++
				continue
			}
			msg := strings.TrimSpace(result.Error)
			if msg == "" {
				msg = "collector processor 返回失败"
			}
			failed[eventID] = msg
			groupFailCount++
		}
		for eventID := range pending {
			failed[eventID] = "collector processor 未返回事件处理结果"
			groupFailCount++
		}
		if groupFailCount > 0 {
			m.reportRuntimeAlert(ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector 部分事件处理失败】",
				Status:    "失败事件会写入失败账本，成功事件直接完成",
				Component: "collector",
				Operation: "process_partial_failed",
				BizType:   bizType,
				Channel:   collectorRuntimeChannelKafka,
				UniqueKey: bizType,
				Reason:    "collector processor 返回部分失败或遗漏结果",
				Count:     groupFailCount,
				Advice:    "请检查 Processor 返回结果是否覆盖所有输入事件，并确认失败事件的错误原因；若 retry/dead 数量持续上升，请优先处理该 bizType。",
			})
		}
		m.recordProcessorBatchMetrics(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
	}
	return success, failed
}

// processBizBatch 执行单个 bizType 的业务 Processor，并把业务 panic 转成错误，避免 worker 异常退出。
func (m *Manager) processBizBatch(ctx context.Context, bizType string, p Processor, group []Event) (results []ProcessResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("collector processor panic bizType=%s panic=%v", bizType, recovered)
		}
	}()
	return p.ProcessBatch(ctx, group)
}

// groupEventsByBizType 按业务类型稳定聚合事件，保证不同 Processor 的批次隔离且调度顺序可预测。
func groupEventsByBizType(events []Event) []eventTaskGroup {
	groups := make([]eventTaskGroup, 0)
	indexByBizType := make(map[string]int)
	for _, event := range events {
		bizType := event.BizType
		if index, ok := indexByBizType[bizType]; ok {
			groups[index].events = append(groups[index].events, event)
			continue
		}
		indexByBizType[bizType] = len(groups)
		groups = append(groups, eventTaskGroup{
			bizType: bizType,
			events:  []Event{event},
		})
	}
	return groups
}

// idempotencyStore 返回 Collector Redis 幂等存储。
func (m *Manager) idempotencyStore() *idempotencyStore {
	if m == nil {
		return nil
	}
	return m.idempotency
}

// beginIdempotentEvents 按 BizType+EventID 过滤已处理、处理中或失败账本接管的重复事件。
func (m *Manager) beginIdempotentEvents(ctx context.Context, events []Event, now time.Time) ([]Event, []idempotencyClaim, int, error) {
	if len(events) == 0 {
		return events, nil, 0, nil
	}
	if !m.idempotencyEnabledForEvents(events) {
		return events, nil, 0, nil
	}
	store := m.idempotencyStore()
	if store == nil {
		return nil, nil, 0, errors.Errorf("collector Redis 幂等存储未初始化")
	}
	return store.beginBatch(ctx, events, now)
}

// markIdempotentDone 标记已成功处理的事件，避免 Kafka 重投重复调用 Processor。
func (m *Manager) markIdempotentDone(ctx context.Context, events []idempotencyStateEvent, now time.Time) error {
	if len(events) == 0 {
		return nil
	}
	if store := m.idempotencyStore(); store != nil {
		return errors.Tag(store.done(ctx, events, now))
	}
	return errors.Errorf("collector Redis 幂等存储未初始化")
}

// markIdempotentFailed 标记已进入失败账本的事件，避免正常 Kafka 重投重复调用 Processor。
func (m *Manager) markIdempotentFailed(ctx context.Context, events []idempotencyStateEvent, now time.Time) error {
	if len(events) == 0 {
		return nil
	}
	if store := m.idempotencyStore(); store != nil {
		return errors.Tag(store.failed(ctx, events, now))
	}
	return errors.Errorf("collector Redis 幂等存储未初始化")
}

// releaseIdempotent 释放处理中事件，通常用于失败账本写入失败后等待 Kafka 重投。
func (m *Manager) releaseIdempotent(ctx context.Context, claims []idempotencyClaim) error {
	if len(claims) == 0 {
		return nil
	}
	if store := m.idempotencyStore(); store != nil {
		return errors.Tag(store.release(ctx, claims))
	}
	return errors.Errorf("collector Redis 幂等存储未初始化")
}

// idempotencyStateEventsFromEvents 提取已启用 Collector 幂等去重的事件终态写入键。
func (m *Manager) idempotencyStateEventsFromEvents(events []Event) []idempotencyStateEvent {
	items := make([]idempotencyStateEvent, 0, len(events))
	for _, event := range events {
		if !m.idempotencyEnabledForBizType(event.BizType) {
			continue
		}
		items = append(items, idempotencyStateEvent{BizType: event.BizType, EventID: event.EventID})
	}
	return items
}

// idempotencyStateEventsFromSet 按成功结果提取已启用幂等去重的事件终态写入键。
func (m *Manager) idempotencyStateEventsFromSet(events []Event, values map[string]struct{}) []idempotencyStateEvent {
	items := make([]idempotencyStateEvent, 0, len(values))
	for _, event := range events {
		if _, ok := values[event.EventID]; !ok {
			continue
		}
		if !m.idempotencyEnabledForBizType(event.BizType) {
			continue
		}
		items = append(items, idempotencyStateEvent{BizType: event.BizType, EventID: event.EventID})
	}
	return items
}

// idempotencyStateEventsFromFailed 按失败结果提取已启用幂等去重的事件终态写入键。
func (m *Manager) idempotencyStateEventsFromFailed(events []Event, values map[string]string) []idempotencyStateEvent {
	items := make([]idempotencyStateEvent, 0, len(values))
	for _, event := range events {
		if _, ok := values[event.EventID]; !ok {
			continue
		}
		if !m.idempotencyEnabledForBizType(event.BizType) {
			continue
		}
		items = append(items, idempotencyStateEvent{BizType: event.BizType, EventID: event.EventID})
	}
	return items
}

// idempotencyStateEventsFromFailureRows 提取已启用幂等去重的失败账本事件终态写入键。
func (m *Manager) idempotencyStateEventsFromFailureRows(rows []model.CollectorFailedEvent) []idempotencyStateEvent {
	items := make([]idempotencyStateEvent, 0, len(rows))
	for _, row := range rows {
		if !m.idempotencyEnabledForBizType(row.BizType) {
			continue
		}
		items = append(items, idempotencyStateEvent{BizType: row.BizType, EventID: row.EventID})
	}
	return items
}

// idempotencyEnabledForEvents 判断当前同任务批次是否需要 Redis 单任务 EventID 去重。
func (m *Manager) idempotencyEnabledForEvents(events []Event) bool {
	if len(events) == 0 {
		return false
	}
	return m.idempotencyEnabledForBizType(events[0].BizType)
}

// idempotencyEnabledForBizType 返回指定 bizType 的 Redis 单任务 EventID 去重开关。
func (m *Manager) idempotencyEnabledForBizType(bizType string) bool {
	if m == nil {
		return false
	}
	bizType = strings.TrimSpace(bizType)
	if task, ok := m.cfg.Tasks[bizType]; ok && task.IdempotencyEnabled != nil {
		return *task.IdempotencyEnabled
	}
	if m.cfg.DefaultTask.IdempotencyEnabled != nil {
		return *m.cfg.DefaultTask.IdempotencyEnabled
	}
	return m.cfg.Idempotency.Enabled
}

// collectorTaskKafkaRoute 返回指定 bizType 继承后的 Kafka 路由。
func (m *Manager) collectorTaskKafkaRoute(bizType string) collectorKafkaRoute {
	if m == nil {
		return collectorKafkaRoute{}
	}
	bizType = strings.TrimSpace(bizType)
	task := config.CollectorTaskConfig{}
	if candidate, ok := m.cfg.Tasks[bizType]; ok {
		task = candidate
	}
	return collectorKafkaRoute{
		topic:   firstNonEmpty(task.Topic, m.cfg.DefaultTask.Topic),
		groupID: firstNonEmpty(task.GroupID, m.cfg.DefaultTask.GroupID),
	}
}

// normalizeEvent 规范化事件关键字段，确保后续幂等、分组和失败账本都有稳定键。
func normalizeEvent(event *Event) {
	if event == nil {
		return
	}
	event.EventID = strings.TrimSpace(event.EventID)
	event.BizType = strings.TrimSpace(event.BizType)
	event.PartitionKey = strings.TrimSpace(event.PartitionKey)
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("null")
	}
}

// normalizeAndValidateEvent 规范化并校验事件，确保无 bizType 的非法事件不会进入正常消费。
func normalizeAndValidateEvent(event *Event) error {
	normalizeEvent(event)
	if event == nil {
		return errors.Errorf("collector event 为空")
	}
	if event.BizType == "" {
		return errors.Errorf("collector event bizType 为空")
	}
	if event.EventID == "" {
		return errors.Errorf("collector event eventId 为空")
	}
	return nil
}

// runKafkaWorker 从指定 Kafka Topic 拉取事件，并按 partition 分发到独立顺序 lane。
func (m *Manager) runKafkaWorker(reader *kafka.Reader, topic string, groupID string) {
	if reader == nil {
		return
	}
	lanes := make(map[int]chan kafkaWorkItem)
	var lanesWG sync.WaitGroup
	var commitMu sync.Mutex
	commit := func(ctx context.Context, messages ...kafka.Message) error {
		commitMu.Lock()
		defer commitMu.Unlock()
		return errors.Tag(reader.CommitMessages(ctx, messages...))
	}
	defer func() {
		for _, lane := range lanes {
			close(lane)
		}
		lanesWG.Wait()
	}()
	laneFor := func(partition int) chan kafkaWorkItem {
		if lane := lanes[partition]; lane != nil {
			return lane
		}
		lane := make(chan kafkaWorkItem, m.kafkaFetchBatchSize())
		lanes[partition] = lane
		lanesWG.Add(1)
		go func() {
			defer lanesWG.Done()
			m.runKafkaPartitionLane(partition, lane, commit)
		}()
		return lane
	}
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		readCtx, cancel := context.WithTimeout(m.ctx, m.kafkaReadTimeout())
		msg, err := reader.FetchMessage(readCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			if errors.Is(err, context.Canceled) && m.ctx.Err() != nil {
				return
			}
			loggerx.Errorw(m.ctx, "采集器 Kafka 拉取失败", err)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka 拉取失败】",
				Status:    "Kafka worker 暂停本轮拉取，500ms 后继续重试",
				Component: "collector",
				Operation: "kafka_fetch_message",
				Channel:   collectorRuntimeChannelKafka,
				UniqueKey: topic,
				Reason:    err.Error(),
				Advice:    "请检查 Kafka broker、topic、consumer group 和网络连接；持续失败会导致 Collector 消费延迟增加。group_id=" + groupID,
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		item := m.kafkaWorkItem(msg)
		select {
		case laneFor(msg.Partition) <- item:
		case <-m.ctx.Done():
			return
		}
	}
}

// kafkaWorkItem 解析 Kafka 消息，坏消息转为失败账本死信记录。
func (m *Manager) kafkaWorkItem(msg kafka.Message) kafkaWorkItem {
	var event Event
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		seed := m.invalidKafkaFailure(msg, err)
		return kafkaWorkItem{message: msg, failure: &seed}
	}
	if err := normalizeAndValidateEvent(&event); err != nil {
		seed := m.invalidKafkaFailure(msg, err)
		return kafkaWorkItem{message: msg, failure: &seed}
	}
	return kafkaWorkItem{message: msg, event: event}
}

// runKafkaPartitionLane 在单个 Kafka partition 内按 offset 顺序聚合同任务批次。
func (m *Manager) runKafkaPartitionLane(partition int, input <-chan kafkaWorkItem, commit kafkaCommitFunc) {
	var pending []kafkaWorkItem
	policy := collectorBatchPolicy{}
	deadline := time.Time{}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	flush := func() bool {
		if len(pending) == 0 {
			return true
		}
		items := pending
		pending = nil
		policy = collectorBatchPolicy{}
		deadline = time.Time{}
		return m.flushKafkaWorkItems(partition, items, commit)
	}
	resetTimer := func(wait time.Duration) <-chan time.Time {
		if timer == nil {
			timer = time.NewTimer(wait)
			return timer.C
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		return timer.C
	}
	appendFirst := func(item kafkaWorkItem) {
		pending = append(pending, item)
		policy = m.collectorTaskPolicyForItem(item)
		deadline = time.Now().Add(policy.batchWait)
	}
	for {
		if len(pending) == 0 {
			select {
			case <-m.ctx.Done():
				return
			case item, ok := <-input:
				if !ok {
					return
				}
				appendFirst(item)
			}
			continue
		}
		if len(pending) >= policy.batchSize {
			if !flush() {
				return
			}
			continue
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			if !flush() {
				return
			}
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case item, ok := <-input:
			if !ok {
				flush()
				return
			}
			if kafkaWorkItemSameTask(pending[0], item) {
				pending = append(pending, item)
				continue
			}
			if !flush() {
				return
			}
			appendFirst(item)
		case <-resetTimer(wait):
			if !flush() {
				return
			}
		}
	}
}

// collectorTaskPolicyForItem 返回当前消息所属任务的聚合策略。
func (m *Manager) collectorTaskPolicyForItem(item kafkaWorkItem) collectorBatchPolicy {
	if item.failure != nil {
		return m.collectorTaskPolicy("collector.invalid")
	}
	return m.collectorTaskPolicy(item.event.BizType)
}

// collectorTaskPolicy 返回指定 bizType 的聚合策略，未配置时回退到默认任务策略。
func (m *Manager) collectorTaskPolicy(bizType string) collectorBatchPolicy {
	var task config.CollectorTaskConfig
	if m != nil {
		if candidate, ok := m.cfg.Tasks[strings.TrimSpace(bizType)]; ok {
			task = candidate
		}
	}
	defaultTask := config.CollectorTaskConfig{}
	if m != nil {
		defaultTask = m.cfg.DefaultTask
	}
	batchSize := task.BatchSize
	if batchSize <= 0 {
		batchSize = defaultTask.BatchSize
	}
	if batchSize <= 0 {
		batchSize = m.kafkaFetchBatchSize()
	}
	batchWait := task.BatchWaitMilliseconds
	if batchWait <= 0 {
		batchWait = defaultTask.BatchWaitMilliseconds
	}
	if batchWait <= 0 {
		return collectorBatchPolicy{
			batchSize: boundedPositiveInt(batchSize, defaultCollectorBatchSize, maxCollectorCarrierBatchSize),
			batchWait: defaultCollectorBatchWait,
		}
	}
	return collectorBatchPolicy{
		batchSize: boundedPositiveInt(batchSize, defaultCollectorBatchSize, maxCollectorCarrierBatchSize),
		batchWait: time.Duration(boundedPositiveInt(batchWait, int(defaultCollectorBatchWait/time.Millisecond), int(maxCollectorTaskBatchWait/time.Millisecond))) * time.Millisecond,
	}
}

// kafkaWorkItemSameTask 判断两条消息能否在同一 partition lane 内合并为同任务批次。
func kafkaWorkItemSameTask(first kafkaWorkItem, next kafkaWorkItem) bool {
	if first.failure != nil || next.failure != nil {
		return first.failure != nil && next.failure != nil
	}
	return first.event.BizType == next.event.BizType
}

// flushKafkaWorkItems 处理一个同 partition、同任务的批次，成功后再提交对应 offset。
func (m *Manager) flushKafkaWorkItems(partition int, items []kafkaWorkItem, commit kafkaCommitFunc) bool {
	batch := kafkaBatchFromWorkItems(items)
	for {
		if err := m.handleKafkaBatch(m.ctx, batch); err != nil {
			if m.ctx.Err() != nil {
				return false
			}
			loggerx.Errorw(m.ctx, "采集器 Kafka 批量处理失败", err,
				logx.Field("partition", partition),
				logx.Field("batch_size", len(batch.messages)),
			)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka 批量处理失败】",
				Status:    "Kafka offset 暂不提交，500ms 后继续重试本批消息",
				Component: "collector",
				Operation: "kafka_handle_batch",
				Channel:   collectorRuntimeChannelKafka,
				UniqueKey: kafkaBatchTopic(batch),
				Reason:    err.Error(),
				Count:     len(batch.messages),
				Advice:    "请检查 Processor、失败账本写入和数据库连接池；修复前 Kafka 消费位点会停留在当前批次。",
			})
			if !sleepWithContext(m.ctx, 500*time.Millisecond) {
				return false
			}
			continue
		}
		break
	}
	if err := commit(m.ctx, batch.messages...); err != nil {
		return m.retryCommitKafkaBatch(partition, batch, commit, err)
	}
	return true
}

// retryCommitKafkaBatch 重试提交 offset，确认成功前不推进同 partition 后续批次。
func (m *Manager) retryCommitKafkaBatch(partition int, batch kafkaBatch, commit kafkaCommitFunc, firstErr error) bool {
	err := firstErr
	for {
		if m.ctx.Err() != nil {
			return false
		}
		loggerx.Errorw(m.ctx, "采集器 Kafka offset 提交失败", err,
			logx.Field("partition", partition),
			logx.Field("batch_size", len(batch.messages)),
		)
		m.reportRuntimeAlert(m.ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindWorkerFailed,
			Title:     "【P1 Collector Kafka Offset 提交失败】",
			Status:    "事件已完成或失败账本已写入，Kafka offset 确认前不推进后续批次",
			Component: "collector",
			Operation: "kafka_commit_messages",
			Channel:   collectorRuntimeChannelKafka,
			UniqueKey: kafkaBatchTopic(batch),
			Reason:    err.Error(),
			Count:     len(batch.messages),
			Advice:    "请检查 Kafka consumer group 状态和 broker 连接；Collector 会按 BizType+EventID 跳过运行期重复事件，确认 offset 前不会推进同 partition 后续批次。",
		})
		if !sleepWithContext(m.ctx, 500*time.Millisecond) {
			return false
		}
		if err = commit(m.ctx, batch.messages...); err == nil {
			return true
		}
	}
}

// kafkaBatchFromWorkItems 把 partition lane 内的消息转换成 Processor 批次。
func kafkaBatchFromWorkItems(items []kafkaWorkItem) kafkaBatch {
	batch := kafkaBatch{
		messages: make([]kafka.Message, 0, len(items)),
		events:   make([]Event, 0, len(items)),
		invalid:  make([]failureEventSeed, 0),
	}
	for _, item := range items {
		batch.messages = append(batch.messages, item.message)
		if item.failure != nil {
			batch.invalid = append(batch.invalid, *item.failure)
			continue
		}
		batch.events = append(batch.events, item.event)
	}
	return batch
}

// kafkaBatchTopic 返回当前批次所属 Topic，用于告警定位。
func kafkaBatchTopic(batch kafkaBatch) string {
	if len(batch.messages) == 0 {
		return ""
	}
	return strings.TrimSpace(batch.messages[0].Topic)
}

// sleepWithContext 在重试等待期间响应 worker 停止信号。
func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// handleKafkaBatch 执行 Kafka 批次，只有成功或失败账本写入成功后调用方才允许提交 offset。
func (m *Manager) handleKafkaBatch(ctx context.Context, batch kafkaBatch) error {
	if len(batch.messages) == 0 {
		return nil
	}
	recordKafkaConsume("received", len(batch.messages))
	m.recordRuntimeKafkaConsume(kafkaBatchBizType(batch), len(batch.messages), 0)
	if len(batch.invalid) > 0 {
		if err := m.saveFailureEvents(ctx, batch.invalid); err != nil {
			return errors.Tag(err)
		}
		recordKafkaConsume("invalid", len(batch.invalid))
		m.recordRuntimeKafkaConsume(kafkaBatchBizType(batch), 0, len(batch.invalid))
		first := batch.invalid[0]
		m.reportRuntimeAlert(ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindInvalidEvent,
			Title:     "【P1 Collector Kafka 消息无效】",
			Status:    "坏消息已写入失败账本死信，后续会提交 Kafka offset",
			Component: "collector",
			Operation: "kafka_invalid_message",
			BizType:   first.event.BizType,
			Channel:   collectorRuntimeChannelKafka,
			UniqueKey: first.event.EventID,
			Reason:    first.lastError,
			Count:     len(batch.invalid),
			Advice:    "请检查 Kafka 生产方消息格式是否为 Collector Event JSON；坏消息不会进入正常 Processor。",
		})
	}
	if len(batch.events) == 0 {
		return nil
	}
	processEvents, claims, duplicateCount, err := m.beginIdempotentEvents(ctx, batch.events, time.Now())
	if err != nil {
		return errors.Tag(err)
	}
	if duplicateCount > 0 {
		recordKafkaConsume("duplicate", duplicateCount)
		m.recordRuntimeDuplicate(kafkaBatchBizType(batch), duplicateCount)
	}
	if len(processEvents) == 0 {
		return nil
	}
	beginAt := time.Now()
	success, failed := m.processBatch(ctx, processEvents)
	if len(failed) == 0 {
		if err := m.markIdempotentDone(ctx, m.idempotencyStateEventsFromEvents(processEvents), time.Now()); err != nil {
			return errors.Tag(err)
		}
		m.logSlowBatch(ctx, "kafka_processor_batch", len(processEvents), beginAt)
		recordKafkaConsume("processed", len(processEvents))
		return nil
	}
	rows := m.failureSeedsFromResults(processEvents, failed, time.Now())
	if err := m.saveFailureEvents(ctx, rows); err != nil {
		if releaseErr := m.releaseIdempotent(ctx, claims); releaseErr != nil {
			return errors.Wrap(releaseErr, "collector 失败账本写入失败后释放幂等占用失败")
		}
		return errors.Tag(err)
	}
	if err := m.markIdempotentDone(ctx, m.idempotencyStateEventsFromSet(processEvents, success), time.Now()); err != nil {
		return errors.Tag(err)
	}
	if err := m.markIdempotentFailed(ctx, m.idempotencyStateEventsFromFailed(processEvents, failed), time.Now()); err != nil {
		return errors.Tag(err)
	}
	m.logSlowBatch(ctx, "kafka_processor_batch_failed", len(processEvents), beginAt)
	recordKafkaConsume("processed", len(success))
	recordKafkaConsume("failed", len(failed))
	return nil
}

// failureSeedsFromResults 将 Processor 失败结果转成失败账本写入快照。
func (m *Manager) failureSeedsFromResults(events []Event, failed map[string]string, now time.Time) []failureEventSeed {
	rows := make([]failureEventSeed, 0, len(failed))
	for _, event := range events {
		msg, ok := failed[event.EventID]
		if !ok {
			continue
		}
		rows = append(rows, failureEventSeed{
			event:     event,
			state:     model.CollectorFailedEventStateRetry,
			attempt:   1,
			nextRunAt: now.Add(nextRetryDelay(1)),
			lastError: msg,
		})
	}
	return rows
}

// invalidKafkaFailure 把无法进入 Processor 的 Kafka 消息转换成死信账本记录。
func (m *Manager) invalidKafkaFailure(msg kafka.Message, cause error) failureEventSeed {
	now := time.Now()
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	payload := map[string]any{
		"topic":     msg.Topic,
		"partition": msg.Partition,
		"offset":    msg.Offset,
		"key":       string(msg.Key),
		"raw":       truncateErr(string(msg.Value), maxInvalidKafkaPayloadBytes),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"raw":""}`)
	}
	return failureEventSeed{
		event: Event{
			EventID:      invalidKafkaEventID(msg),
			BizType:      "collector.invalid",
			PartitionKey: fmt.Sprintf("%s:%d", msg.Topic, msg.Partition),
			Payload:      json.RawMessage(body),
		},
		state:     model.CollectorFailedEventStateDead,
		attempt:   m.failureMaxRetryTimes(),
		nextRunAt: now,
		lastError: reason,
	}
}

// invalidKafkaEventID 基于 Kafka 位点生成坏消息幂等键。
func invalidKafkaEventID(msg kafka.Message) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)))
	return "collector:invalid:" + hex.EncodeToString(sum[:8])
}

// saveFailureEvents 批量写入失败账本，成功后调用方才允许提交 Kafka offset。
func (m *Manager) saveFailureEvents(ctx context.Context, seeds []failureEventSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	rows := make([]model.CollectorFailedEvent, 0, len(seeds))
	for _, seed := range seeds {
		finishedAt := (*time.Time)(nil)
		if seed.state == model.CollectorFailedEventStateDead || seed.state == model.CollectorFailedEventStateDone {
			t := now
			finishedAt = &t
		}
		nextRunAt := seed.nextRunAt
		if nextRunAt.IsZero() {
			nextRunAt = now
		}
		rows = append(rows, model.CollectorFailedEvent{
			EventID:      seed.event.EventID,
			BizType:      seed.event.BizType,
			PartitionKey: seed.event.PartitionKey,
			Payload:      string(seed.event.Payload),
			State:        seed.state,
			Attempt:      seed.attempt,
			NextRunAt:    nextRunAt,
			StartedAt:    nil,
			FinishedAt:   finishedAt,
			LastError:    truncateErr(seed.lastError, 1000),
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	beginAt := time.Now()
	result := m.failures.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(&rows, m.failureWriteBatchSize())
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected > 0 {
		recordFailurePersistBatch(failureSeedsStateLabel(seeds), int(result.RowsAffected), time.Since(beginAt))
	}
	m.logSlowBatch(ctx, "failure_insert", len(rows), beginAt)
	return nil
}

// runFailureRetryWorker 定时从失败账本批量领取事件并投送给 Processor。
func (m *Manager) runFailureRetryWorker() {
	intervalSeconds := m.cfg.FailureRetry.RunnerIntervalSeconds
	if intervalSeconds <= 0 {
		intervalSeconds = defaultFailureRetryIntervalSeconds
	}
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.runFailureRetryOnce(m.ctx, 0); err != nil {
				if strings.Contains(err.Error(), "collector 批量消费存在失败") {
					continue
				}
				loggerx.Errorw(m.ctx, "采集器 失败账本重试失败", err)
				m.reportRuntimeAlert(m.ctx, RuntimeAlert{
					Kind:      RuntimeAlertKindWorkerFailed,
					Title:     "【P1 Collector 失败账本重试失败】",
					Status:    "失败账本本轮重试失败，下一轮会继续重试",
					Component: "collector",
					Operation: "failure_retry_run_once",
					Channel:   collectorRuntimeChannelKafka,
					Reason:    err.Error(),
					Advice:    "请检查 collector_failed_event 查询、状态回写、Processor 业务依赖和数据库连接池；若失败持续出现，请查看 Collector 管理页积压与死信。",
				})
			}
		}
	}
}

// runFailureRetryOnce 执行一轮失败账本批量重试。
func (m *Manager) runFailureRetryOnce(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	beginAt := time.Now()
	limit = boundedPositiveInt(limit, m.failureRetryBatchSize(), maxCollectorCarrierBatchSize)
	now := time.Now()
	if err := m.recoverExpiredRunning(ctx, limit, now); err != nil {
		return 0, errors.Tag(err)
	}
	rows, err := m.claimDue(ctx, limit, now)
	if err != nil {
		return 0, errors.Tag(err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	events := make([]Event, 0, len(rows))
	rowByEventID := make(map[string]model.CollectorFailedEvent, len(rows))
	failRows := make([]model.CollectorFailedEvent, 0, len(rows))
	for _, row := range rows {
		event := Event{
			EventID:      row.EventID,
			BizType:      row.BizType,
			PartitionKey: row.PartitionKey,
			Payload:      json.RawMessage(row.Payload),
		}
		if err := normalizeAndValidateEvent(&event); err != nil {
			row.LastError = err.Error()
			failRows = append(failRows, row)
			continue
		}
		events = append(events, event)
		rowByEventID[event.EventID] = row
	}
	success, failed := m.processBatch(ctx, events)
	successIDs := make([]int64, 0, len(success))
	successRows := make([]model.CollectorFailedEvent, 0, len(success))
	for eventID := range success {
		row, ok := rowByEventID[eventID]
		if !ok {
			continue
		}
		successIDs = append(successIDs, row.ID)
		successRows = append(successRows, row)
	}
	for eventID, msg := range failed {
		row, ok := rowByEventID[eventID]
		if !ok {
			continue
		}
		row.LastError = msg
		failRows = append(failRows, row)
	}
	if len(successIDs) > 0 {
		if err := m.markDone(ctx, successIDs, time.Now()); err != nil {
			return len(successIDs), errors.Tag(err)
		}
		if err := m.markIdempotentDone(ctx, m.idempotencyStateEventsFromFailureRows(successRows), time.Now()); err != nil {
			return len(successIDs), errors.Tag(err)
		}
	}
	if len(failRows) > 0 {
		if err := m.markFailed(ctx, failRows, errors.Errorf("collector 批量消费存在失败"), time.Now()); err != nil {
			return len(successIDs), errors.Tag(err)
		}
		if err := m.markIdempotentFailed(ctx, m.idempotencyStateEventsFromFailureRows(failRows), time.Now()); err != nil {
			return len(successIDs), errors.Tag(err)
		}
	}
	if len(failRows) > 0 {
		m.logSlowBatch(ctx, "failure_retry_failed", len(rows), beginAt)
		return len(successIDs), errors.Errorf("collector 批量消费存在失败 success=%d failed=%d", len(successIDs), len(failRows))
	}
	m.logSlowBatch(ctx, "failure_retry_batch", len(rows), beginAt)
	return len(successIDs), nil
}

// claimDue 使用 SKIP LOCKED 批量领取到期失败事件，保证多实例并发安全。
func (m *Manager) claimDue(ctx context.Context, limit int, now time.Time) ([]model.CollectorFailedEvent, error) {
	var rows []model.CollectorFailedEvent
	err := m.failures.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&model.CollectorFailedEvent{}).
			Where("state IN ? AND next_run_at <= ?", []model.CollectorFailedEventState{
				model.CollectorFailedEventStatePending,
				model.CollectorFailedEventStateRetry,
			}, now).
			Order("id ASC").
			Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		if err := q.Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		startedAt := time.Now()
		updates := map[string]any{
			"state":      model.CollectorFailedEventStateRunning,
			"started_at": startedAt,
			"updated_at": startedAt,
		}
		if err := tx.Model(&model.CollectorFailedEvent{}).Where("id IN ?", ids).Updates(updates).Error; err != nil {
			return errors.Tag(err)
		}
		for i := range rows {
			rows[i].State = model.CollectorFailedEventStateRunning
			rows[i].StartedAt = &startedAt
		}
		return nil
	})
	return rows, errors.Tag(err)
}

// recoverExpiredRunning 回收租约超时的重试中事件，避免 worker 异常退出造成永久卡死。
func (m *Manager) recoverExpiredRunning(ctx context.Context, limit int, now time.Time) error {
	leaseSeconds := m.cfg.FailureRetry.RunningLeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = defaultFailureRunningLeaseSeconds
	}
	if limit <= 0 {
		limit = m.failureRetryBatchSize()
	}
	cutoff := now.Add(-time.Duration(leaseSeconds) * time.Second)
	recoverErr := errors.Errorf("collector running 失败事件租约超时，已回收等待重试")
	rows := make([]model.CollectorFailedEvent, 0, limit)
	summary := collectorDeadSummary{}
	err := m.failures.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&model.CollectorFailedEvent{}).
			Where("state = ? AND started_at IS NOT NULL AND started_at <= ?", model.CollectorFailedEventStateRunning, cutoff).
			Order("id ASC").
			Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		if err := q.Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		var err error
		summary, err = m.markFailedInTx(tx, rows, recoverErr, now)
		return errors.Tag(err)
	})
	if err != nil {
		return errors.Tag(err)
	}
	m.reportDeadEvents(ctx, summary, recoverErr)
	return nil
}

// markDone 批量标记重试成功事件，使用 running 状态校验避免误更新。
func (m *Manager) markDone(ctx context.Context, ids []int64, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	updates := map[string]any{
		"state":       model.CollectorFailedEventStateDone,
		"finished_at": now,
		"updated_at":  now,
		"last_error":  "",
	}
	result := m.failures.WithContext(ctx).
		Model(&model.CollectorFailedEvent{}).
		Where("id IN ? AND state = ?", ids, model.CollectorFailedEventStateRunning).
		Updates(updates)
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != int64(len(ids)) {
		return errors.Errorf("collectorx.markDone 状态回写行数不一致 expect=%d actual=%d", len(ids), result.RowsAffected)
	}
	return nil
}

// markFailed 按指数退避策略回写失败事件，超过最大次数后进入死信。
func (m *Manager) markFailed(ctx context.Context, rows []model.CollectorFailedEvent, execErr error, now time.Time) error {
	summary := collectorDeadSummary{}
	err := m.failures.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		summary, err = m.markFailedInTx(tx, rows, execErr, now)
		return errors.Tag(err)
	})
	if err != nil {
		return errors.Tag(err)
	}
	m.reportDeadEvents(ctx, summary, execErr)
	return nil
}

// markFailedInTx 在同一事务内把 running 事件回写为 retry/dead。
func (m *Manager) markFailedInTx(tx *gorm.DB, rows []model.CollectorFailedEvent, execErr error, now time.Time) (collectorDeadSummary, error) {
	summary := collectorDeadSummary{}
	if tx == nil {
		return summary, errors.Errorf("collectorx.markFailedInTx tx 为空")
	}
	maxRetry := m.failureMaxRetryTimes()
	for _, row := range rows {
		newAttempt := row.Attempt + 1
		nextState := model.CollectorFailedEventStateRetry
		nextRunAt := now
		finishedAt := (*time.Time)(nil)
		if newAttempt >= maxRetry {
			nextState = model.CollectorFailedEventStateDead
			finishedAt = &now
			summary.count++
			if summary.bizType == "" {
				summary.bizType = strings.TrimSpace(row.BizType)
			}
			if summary.eventID == "" {
				summary.eventID = strings.TrimSpace(row.EventID)
			}
		} else {
			nextRunAt = now.Add(nextRetryDelay(newAttempt))
		}
		lastError := row.LastError
		if strings.TrimSpace(lastError) == "" && execErr != nil {
			lastError = execErr.Error()
		}
		updates := map[string]any{
			"state":       nextState,
			"attempt":     newAttempt,
			"next_run_at": nextRunAt,
			"started_at":  nil,
			"updated_at":  now,
			"last_error":  truncateErr(lastError, 1000),
		}
		if finishedAt != nil {
			updates["finished_at"] = *finishedAt
		} else {
			updates["finished_at"] = nil
		}
		result := tx.Model(&model.CollectorFailedEvent{}).
			Where("id = ? AND state = ?", row.ID, model.CollectorFailedEventStateRunning).
			Updates(updates)
		if result.Error != nil {
			return summary, errors.Tag(result.Error)
		}
		if result.RowsAffected == 0 {
			return summary, errors.Errorf("collectorx.markFailed 任务状态已变化 id=%d", row.ID)
		}
	}
	return summary, nil
}

// reportDeadEvents 上报失败账本事件进入死信的低基数告警和指标。
func (m *Manager) reportDeadEvents(ctx context.Context, summary collectorDeadSummary, execErr error) {
	if summary.count <= 0 {
		return
	}
	reason := ""
	if execErr != nil {
		reason = execErr.Error()
	}
	m.reportRuntimeAlert(ctx, RuntimeAlert{
		Kind:      RuntimeAlertKindDeadEvent,
		Title:     "【P1 Collector 事件进入死信】",
		Status:    "事件已达到最大重试次数并进入死信，不会继续自动处理",
		Component: "collector",
		Operation: "mark_dead",
		BizType:   summary.bizType,
		Channel:   collectorRuntimeChannelKafka,
		UniqueKey: summary.bizType,
		Reason:    reason,
		Count:     summary.count,
		Advice:    "请在 Collector 管理页按死信状态和 bizType 检索，确认业务影响范围后修复数据或处理器，再人工重试；必要时可用首个事件ID " + summary.eventID + " 定位。",
	})
	recordFailureDead(summary.bizType, summary.count)
	m.recordRuntimeDead(summary.bizType, summary.count)
}

// nextRetryDelay 返回指定失败次数对应的下次重试延迟。
func nextRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 1 * time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 30 * time.Second
	case 4:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// kafkaFetchBatchSize 返回 partition lane 本地缓冲大小，未配置时使用安全默认值。
func (m *Manager) kafkaFetchBatchSize() int {
	if m == nil {
		return defaultCollectorBatchSize
	}
	size := m.cfg.DefaultTask.BatchSize
	if size <= 0 {
		size = m.cfg.Kafka.WriteBatchSize
	}
	return boundedPositiveInt(size, defaultCollectorBatchSize, maxCollectorCarrierBatchSize)
}

// kafkaWriteBatchWait 返回 Kafka producer 单批写入等待时间。
func (m *Manager) kafkaWriteBatchWait() time.Duration {
	if m == nil || m.cfg.Kafka.WriteBatchWaitMilliseconds <= 0 {
		return defaultCollectorBatchWait
	}
	return time.Duration(boundedPositiveInt(m.cfg.Kafka.WriteBatchWaitMilliseconds, int(defaultCollectorBatchWait/time.Millisecond), 5000)) * time.Millisecond
}

// kafkaReadTimeout 返回 Kafka 单次读取超时。
func (m *Manager) kafkaReadTimeout() time.Duration {
	if m == nil || m.cfg.Kafka.ReadTimeout <= 0 {
		return time.Second
	}
	return time.Duration(m.cfg.Kafka.ReadTimeout) * time.Second
}

// failureRetryBatchSize 返回失败账本 worker 单轮消费批次大小。
func (m *Manager) failureRetryBatchSize() int {
	if m == nil {
		return defaultFailureRetryBatchSize
	}
	return boundedPositiveInt(m.cfg.FailureRetry.RunnerBatchSize, defaultFailureRetryBatchSize, maxCollectorCarrierBatchSize)
}

// failureWriteBatchSize 返回失败账本批量写入大小。
func (m *Manager) failureWriteBatchSize() int {
	size := defaultCollectorBatchSize
	if m != nil {
		if m.cfg.DefaultTask.BatchSize > size {
			size = m.cfg.DefaultTask.BatchSize
		}
		if m.cfg.FailureRetry.RunnerBatchSize > size {
			size = m.cfg.FailureRetry.RunnerBatchSize
		}
	}
	return boundedPositiveInt(size, defaultCollectorBatchSize, maxCollectorCarrierBatchSize)
}

// failureMaxRetryTimes 返回失败账本最大重试次数。
func (m *Manager) failureMaxRetryTimes() int {
	if m == nil || m.cfg.FailureRetry.MaxRetryTimes <= 0 {
		return defaultFailureMaxRetryTimes
	}
	return m.cfg.FailureRetry.MaxRetryTimes
}

// collectorIdempotencyTTL 返回成功或失败终态去重缓存 TTL。
func collectorIdempotencyTTL(cfg config.CollectorIdempotencyConfig) time.Duration {
	if cfg.TTLSeconds <= 0 {
		return defaultIdempotencyTTL
	}
	return time.Duration(cfg.TTLSeconds) * time.Second
}

// collectorIdempotencyProcessingTTL 返回处理中占用租约 TTL。
func collectorIdempotencyProcessingTTL(cfg config.CollectorIdempotencyConfig) time.Duration {
	if cfg.ProcessingTTLSeconds <= 0 {
		return defaultIdempotencyProcessingTTL
	}
	return time.Duration(cfg.ProcessingTTLSeconds) * time.Second
}

// collectorIdempotencyPipelineBatchSize 返回 Redis Pipeline 分块命令数。
func collectorIdempotencyPipelineBatchSize(cfg config.CollectorIdempotencyConfig) int {
	return boundedPositiveInt(cfg.PipelineBatchSize, defaultIdempotencyPipelineBatchSize, maxIdempotencyPipelineBatchSize)
}

// collectorAnyIdempotencyEnabled 判断配置中是否存在需要 Redis 去重的任务。
func collectorAnyIdempotencyEnabled(cfg config.CollectorConfig) bool {
	if cfg.Idempotency.Enabled {
		return true
	}
	if cfg.DefaultTask.IdempotencyEnabled != nil && *cfg.DefaultTask.IdempotencyEnabled {
		return true
	}
	for _, task := range cfg.Tasks {
		if task.IdempotencyEnabled != nil && *task.IdempotencyEnabled {
			return true
		}
	}
	return false
}

// normalizeCollectorTaskRoutes 清理任务 Kafka 路由字段，避免空白值参与匹配。
func normalizeCollectorTaskRoutes(cfg *config.CollectorConfig) {
	if cfg == nil {
		return
	}
	cfg.DefaultTask.Topic = strings.TrimSpace(cfg.DefaultTask.Topic)
	cfg.DefaultTask.GroupID = strings.TrimSpace(cfg.DefaultTask.GroupID)
	if len(cfg.Tasks) == 0 {
		return
	}
	tasks := make(map[string]config.CollectorTaskConfig, len(cfg.Tasks))
	for bizType, task := range cfg.Tasks {
		bizType = strings.TrimSpace(bizType)
		task.Topic = strings.TrimSpace(task.Topic)
		task.GroupID = strings.TrimSpace(task.GroupID)
		tasks[bizType] = task
	}
	cfg.Tasks = tasks
}

// collectorConfiguredTopics 返回 Collector 当前配置需要写入的 Topic 列表。
func collectorConfiguredTopics(cfg config.CollectorConfig) []string {
	topics := make(map[string]struct{})
	if cfg.DefaultTask.Topic != "" {
		topics[cfg.DefaultTask.Topic] = struct{}{}
	}
	for _, task := range cfg.Tasks {
		topic := firstNonEmpty(task.Topic, cfg.DefaultTask.Topic)
		if topic == "" {
			continue
		}
		topics[topic] = struct{}{}
	}
	return sortedKeys(topics)
}

// collectorConfiguredReadRoutes 返回按 Topic 去重后的 Kafka 消费路由。
func collectorConfiguredReadRoutes(cfg config.CollectorConfig) []collectorKafkaRoute {
	byTopic := make(map[string]string)
	if cfg.DefaultTask.Topic != "" && cfg.DefaultTask.GroupID != "" {
		byTopic[cfg.DefaultTask.Topic] = cfg.DefaultTask.GroupID
	}
	for _, task := range cfg.Tasks {
		topic := firstNonEmpty(task.Topic, cfg.DefaultTask.Topic)
		groupID := firstNonEmpty(task.GroupID, cfg.DefaultTask.GroupID)
		if topic == "" || groupID == "" {
			continue
		}
		byTopic[topic] = groupID
	}
	topics := sortedKeysFromStringMap(byTopic)
	routes := make([]collectorKafkaRoute, 0, len(topics))
	for _, topic := range topics {
		routes = append(routes, collectorKafkaRoute{topic: topic, groupID: byTopic[topic]})
	}
	return routes
}

// sortedKeys 返回字符串集合的稳定排序结果。
func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeysFromStringMap 返回字符串映射的稳定 key 顺序。
func sortedKeysFromStringMap(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

// boundedPositiveInt 返回带上限的正整数配置，避免误配置过大批次拖垮 DB 或内存。
func boundedPositiveInt(value int, fallback int, maxValue int) int {
	if fallback <= 0 {
		fallback = 1
	}
	if maxValue <= 0 {
		maxValue = fallback
	}
	if value <= 0 {
		value = fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// failureSeedsStateLabel 归一化一批失败事件的初始状态标签，便于 metrics 聚合。
func failureSeedsStateLabel(seeds []failureEventSeed) string {
	if len(seeds) == 0 {
		return "unknown"
	}
	label := failedEventStateMetricLabel(seeds[0].state)
	for i := 1; i < len(seeds); i++ {
		if current := failedEventStateMetricLabel(seeds[i].state); current != label {
			return "mixed"
		}
	}
	return label
}

// failedEventStateMetricLabel 返回失败账本状态的指标标签。
func failedEventStateMetricLabel(state model.CollectorFailedEventState) string {
	switch state {
	case model.CollectorFailedEventStatePending:
		return "pending"
	case model.CollectorFailedEventStateRunning:
		return "running"
	case model.CollectorFailedEventStateDone:
		return "done"
	case model.CollectorFailedEventStateRetry:
		return "retry"
	case model.CollectorFailedEventStateDead:
		return "dead"
	default:
		return "unknown"
	}
}

// truncateErr 截断错误摘要，避免异常文本过长撑大行记录。
func truncateErr(msg string, limit int) string {
	msg = strings.TrimSpace(msg)
	if limit <= 0 || len(msg) <= limit {
		return msg
	}
	return msg[:limit]
}

// collectorAlertReason 生成 Collector 告警摘要，保留样例 ID 但不让它参与告警指纹。
func collectorAlertReason(sampleLabel, sampleValue, reason string) string {
	parts := make([]string, 0, 2)
	sampleLabel = strings.TrimSpace(sampleLabel)
	sampleValue = strings.TrimSpace(sampleValue)
	if sampleLabel != "" && sampleValue != "" {
		parts = append(parts, sampleLabel+"="+sampleValue)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		parts = append(parts, reason)
	}
	return strings.Join(parts, "；")
}

// logSlowBatch 记录慢批次，便于压测和生产环境定位瓶颈。
func (m *Manager) logSlowBatch(ctx context.Context, stage string, size int, beginAt time.Time) {
	if size <= 0 || beginAt.IsZero() {
		return
	}
	cost := time.Since(beginAt)
	if cost < slowCollectorBatchThreshold {
		return
	}
	loggerx.Sloww(ctx, "采集器 批次耗时较高",
		logx.Field("stage", stage),
		logx.Field("batch_size", size),
		logx.Field("latency_ms", cost.Milliseconds()),
	)
}

// reportRuntimeAlert 上报 Collector 后台运行异常；未配置 hook 时保持原有日志行为。
func (m *Manager) reportRuntimeAlert(ctx context.Context, alert RuntimeAlert) {
	if m == nil {
		return
	}
	m.mu.RLock()
	hook := m.alertHook
	m.mu.RUnlock()
	if hook == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	hook(ctx, normalizeRuntimeAlert(alert))
}
