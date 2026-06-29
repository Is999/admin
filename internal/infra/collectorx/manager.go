package collectorx

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"admin/internal/config"
	"admin/internal/infra/loggerx"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Collector 默认运行参数限制单轮处理规模、租约和慢批次日志阈值。
const (
	defaultDBRunnerBatchSize       = 500                    // DB outbox 单轮领取的默认事件数量
	defaultDBRunnerIntervalSeconds = 1                      // DB outbox 空轮询后的默认等待秒数
	defaultDBRunningLeaseSeconds   = 600                    // DB outbox 运行中事件的默认租约秒数
	defaultDBMaxRetryTimes         = 8                      // DB outbox 事件默认最大重试次数
	defaultRedisClaimIdle          = 30 * time.Second       // Redis Stream pending 消息可重新 claim 的默认空闲时长
	defaultKafkaFetchBatchSize     = 500                    // Kafka 单轮最多拉取的默认消息数量
	defaultKafkaFetchWait          = 20 * time.Millisecond  // Kafka 聚合批次的默认等待时长
	maxCollectorCarrierBatchSize   = 5000                   // Collector 单批载体处理数量上限
	slowCollectorBatchThreshold    = 200 * time.Millisecond // Collector 批量处理慢日志阈值
)

// Manager 负责通用收集器的事件投递、可靠落地、批量消费和失败重试。
type Manager struct {
	cfg    config.CollectorConfig // 收集器运行配置
	outbox *gorm.DB               // DB outbox 连接，用于兜底、领取和状态回写
	redis  redis.UniversalClient  // Redis 客户端，用于 Redis Stream 载体
	writer *kafka.Writer          // Kafka 写入器，用于优先投递到 Kafka

	mu         sync.RWMutex         // 保护 processors 注册表
	processors map[string]Processor // bizType 到批量处理器的映射
	alertHook  AlertHook            // 后台运行异常告警钩子
	instanceID string               // 当前实例唯一 ID，用于 Redis consumer 名称隔离

	ctx    context.Context    // 后台 worker 生命周期上下文
	cancel context.CancelFunc // 后台 worker 停止函数
	wg     sync.WaitGroup     // 等待后台 worker 退出

	kafkaReader *kafka.Reader // Kafka 消费器，用于把 Kafka 消息可靠落地到 DB outbox
}

// eventDelivery 表示从 Kafka/Redis/DB 载体中解析出的待落地事件。
type eventDelivery struct {
	event     Event  // 结构化业务事件
	transport string // 来源载体：kafka/redis/db
	messageID string // Redis Stream 消息 ID，非 Redis 来源为空
}

// New 创建通用收集器管理器，并按配置初始化可用的 Kafka 写入器。
func New(cfg config.CollectorConfig, outboxDB *gorm.DB, rds redis.UniversalClient) (*Manager, error) {
	if outboxDB == nil {
		return nil, errors.Errorf("collector outboxDB 为空")
	}
	ensureMetricsRegistered()
	cfg.Transport = normalizeCollectorTransport(cfg.Transport)
	cfg.Kafka.Topic = strings.TrimSpace(cfg.Kafka.Topic)
	cfg.Kafka.GroupID = strings.TrimSpace(cfg.Kafka.GroupID)
	cfg.Redis.Stream = strings.TrimSpace(cfg.Redis.Stream)
	cfg.Redis.Group = strings.TrimSpace(cfg.Redis.Group)
	cfg.Redis.Consumer = strings.TrimSpace(cfg.Redis.Consumer)

	m := &Manager{
		cfg:        cfg,
		outbox:     outboxDB,
		redis:      rds,
		processors: make(map[string]Processor),
		instanceID: strings.ReplaceAll(uuid.NewString(), "-", ""),
	}

	if cfg.Kafka.Enabled && len(cfg.Kafka.Brokers) > 0 && cfg.Kafka.Topic != "" {
		timeout := time.Duration(cfg.Kafka.WriteTimeout) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		batchSize := cfg.Kafka.BatchSize
		if batchSize <= 0 {
			batchSize = 500
		}
		m.writer = &kafka.Writer{
			Addr:         kafka.TCP(cfg.Kafka.Brokers...),
			Topic:        cfg.Kafka.Topic,
			Balancer:     &kafka.Hash{},
			BatchSize:    batchSize,
			WriteTimeout: timeout,
		}
	}

	return m, nil
}

// SetAlertHook 设置 Collector 后台运行异常告警钩子。
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
		m.runDBWorker()
	}()

	if m.cfg.Kafka.Enabled && len(m.cfg.Kafka.Brokers) > 0 && m.cfg.Kafka.Topic != "" && m.cfg.Kafka.GroupID != "" {
		m.kafkaReader = kafka.NewReader(kafka.ReaderConfig{
			Brokers: m.cfg.Kafka.Brokers,
			GroupID: m.cfg.Kafka.GroupID,
			Topic:   m.cfg.Kafka.Topic,
		})
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.runKafkaWorker()
		}()
	}

	if m.cfg.Redis.Enabled && m.redis != nil && m.cfg.Redis.Stream != "" && m.cfg.Redis.Group != "" && m.cfg.Redis.Consumer != "" {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.runRedisWorker()
		}()
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
	if m.kafkaReader != nil {
		_ = m.kafkaReader.Close()
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
	if m.writer != nil {
		_ = m.writer.Close()
	}
	return nil
}

// RunNow 手动执行一轮 DB outbox 任务，便于管理端立即消费已落地事件。
func (m *Manager) RunNow(ctx context.Context, limit int) (int, error) {
	if m == nil || !m.cfg.Enabled {
		return 0, errors.Errorf("collector 未启用")
	}
	return m.runDBOnce(ctx, limit)
}

// Enqueue 投递一条结构化业务事件。
// 投递顺序按 Kafka -> Redis Stream -> DB outbox 逐级兜底；只有 DB outbox 也失败时才返回入队失败。
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

	body, err := json.Marshal(event)
	if err != nil {
		return "", errors.Tag(err)
	}

	transport := normalizeCollectorTransport(m.cfg.Transport)
	if (transport == collectorTransportAuto || transport == collectorTransportKafka) && m.kafkaAvailable() {
		if err := m.publishKafka(ctx, event.PartitionKey, body); err == nil {
			return event.EventID, nil
		} else {
			loggerx.Errorw(ctx, "采集器 消息队列投递降级", err,
				logx.Field("event_id", event.EventID),
				logx.Field("biz_type", event.BizType),
			)
		}
	} else if transport == collectorTransportKafka {
		loggerx.ErrorTextw(ctx, "采集器 消息队列投递降级", "消息队列未配置",
			logx.Field("event_id", event.EventID),
			logx.Field("biz_type", event.BizType),
		)
	}

	if (transport == collectorTransportAuto || transport == collectorTransportKafka || transport == collectorTransportRedis) && m.redisAvailable() {
		if err := m.publishRedis(ctx, body); err == nil {
			return event.EventID, nil
		} else {
			loggerx.Errorw(ctx, "采集器 缓存流投递降级", err,
				logx.Field("event_id", event.EventID),
				logx.Field("biz_type", event.BizType),
			)
		}
	} else if transport == collectorTransportRedis {
		loggerx.ErrorTextw(ctx, "采集器 缓存流投递降级", "缓存流未配置",
			logx.Field("event_id", event.EventID),
			logx.Field("biz_type", event.BizType),
		)
	}

	if err := m.saveOutbox(ctx, event, collectorTransportDB, model.CollectorOutboxStatePending, 0, time.Now(), ""); err != nil {
		m.reportRuntimeAlert(ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindEnqueueFailed,
			Title:     "【P1 Collector 事件入队失败】",
			Status:    "Kafka/Redis 降级后 DB outbox 兜底也失败，该事件不会进入后台重试链路",
			Component: "collector",
			Operation: "enqueue_save_outbox",
			BizType:   event.BizType,
			Transport: collectorTransportDB,
			UniqueKey: event.BizType,
			Reason:    collectorAlertReason("eventId", event.EventID, err.Error()),
			Advice:    "请优先检查 Collector outbox 主库写入、唯一键冲突和磁盘/连接池状态；确认调用方是否有同步重试或补偿队列，避免事件丢失。",
		})
		return "", errors.Tag(err)
	}
	return event.EventID, nil
}

// kafkaAvailable 判断 Kafka 载体是否具备写入条件。
func (m *Manager) kafkaAvailable() bool {
	return m != nil && m.writer != nil
}

// redisAvailable 判断 Redis Stream 载体是否具备写入条件。
func (m *Manager) redisAvailable() bool {
	return m != nil && m.redis != nil && m.cfg.Redis.Enabled && m.cfg.Redis.Stream != ""
}

// normalizeCollectorTransport 归一化配置中的 transport，保证大小写和空值不会改变载体选择语义。
func normalizeCollectorTransport(transport string) string {
	value := strings.ToLower(strings.TrimSpace(transport))
	if value == "" {
		return collectorTransportAuto
	}
	return value
}

// publishKafka 将事件写入 Kafka，失败后由 Enqueue 继续降级。
func (m *Manager) publishKafka(ctx context.Context, key string, body []byte) error {
	if m.writer == nil {
		return errors.Errorf("collector kafka 未配置")
	}
	timeout := time.Duration(m.cfg.Kafka.WriteTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return errors.Tag(m.writer.WriteMessages(writeCtx, kafka.Message{Key: []byte(key), Value: body}))
}

// publishRedis 将事件写入 Redis Stream，失败后由 Enqueue 继续降级。
func (m *Manager) publishRedis(ctx context.Context, body []byte) error {
	if m.redis == nil || !m.cfg.Redis.Enabled || m.cfg.Redis.Stream == "" {
		return errors.Errorf("collector redis 未配置")
	}
	args := &redis.XAddArgs{
		Stream: m.cfg.Redis.Stream,
		Values: map[string]any{
			"body": string(body),
		},
	}
	if m.cfg.Redis.MaxLen > 0 {
		args.MaxLen = m.cfg.Redis.MaxLen
		args.Approx = true
	}
	return errors.Tag(m.redis.XAdd(ctx, args).Err())
}

// saveOutbox 将单条事件写入 DB outbox，用于最终可靠兜底。
func (m *Manager) saveOutbox(ctx context.Context, event Event, transport string, state model.CollectorOutboxState, attempt int, nextRunAt time.Time, lastErr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	row := model.CollectorOutbox{
		EventID:      event.EventID,
		BizType:      event.BizType,
		PartitionKey: event.PartitionKey,
		Payload:      string(event.Payload),
		Transport:    transport,
		State:        state,
		Attempt:      attempt,
		NextRunAt:    nextRunAt,
		StartedAt:    nil,
		FinishedAt:   nil,
		LastError:    truncateErr(lastErr, 1000),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	beginAt := time.Now()
	result := m.outbox.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error == nil && result.RowsAffected > 0 {
		recordOutboxPersistBatch(transport, int(result.RowsAffected), time.Since(beginAt))
	}
	m.logSlowBatch(ctx, "outbox_insert", 1, beginAt)
	return errors.Tag(result.Error)
}

// saveOutboxBatch 将一批事件批量写入 DB outbox，使用 event_id 唯一键完成幂等去重。
func (m *Manager) saveOutboxBatch(ctx context.Context, deliveries []eventDelivery, state model.CollectorOutboxState, attempt int, nextRunAt time.Time, lastErr string) error {
	if len(deliveries) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	rows := make([]model.CollectorOutbox, 0, len(deliveries))
	for i := range deliveries {
		rows = append(rows, model.CollectorOutbox{
			EventID:      deliveries[i].event.EventID,
			BizType:      deliveries[i].event.BizType,
			PartitionKey: deliveries[i].event.PartitionKey,
			Payload:      string(deliveries[i].event.Payload),
			Transport:    deliveries[i].transport,
			State:        state,
			Attempt:      attempt,
			NextRunAt:    nextRunAt,
			StartedAt:    nil,
			FinishedAt:   nil,
			LastError:    truncateErr(lastErr, 1000),
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	beginAt := time.Now()
	transportLabel := deliveriesTransportLabel(deliveries)
	result := m.outbox.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(&rows, m.outboxInsertBatchSize())
	if result.Error == nil && result.RowsAffected > 0 {
		recordOutboxPersistBatch(transportLabel, int(result.RowsAffected), time.Since(beginAt))
	}
	m.logSlowBatch(ctx, "outbox_insert", len(rows), beginAt)
	return errors.Tag(result.Error)
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
	grouped := groupEventsByBizType(events)
	for bizType, group := range grouped {
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
				Status:    "该业务类型事件无法消费，已按重试策略回写 outbox",
				Component: "collector",
				Operation: "process_missing_processor",
				BizType:   bizType,
				Transport: collectorTransportDB,
				UniqueKey: bizType,
				Reason:    "collector 未注册 processor bizType=" + bizType,
				Count:     len(group),
				Advice:    "请确认当前镜像是否注册了该 bizType 的 Processor，以及 collector.enabled、CDC 场景配置和 worker 部署版本是否一致。",
			})
			recordProcessorBatch(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
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
				Status:    "本批事件已按重试策略回写 outbox，后续会继续重试",
				Component: "collector",
				Operation: "process_batch",
				BizType:   bizType,
				Transport: collectorTransportDB,
				UniqueKey: bizType,
				Reason:    err.Error(),
				Count:     len(group),
				Advice:    "请检查对应 Processor 的业务依赖、数据格式和批量写入状态；若 outbox 重试持续上涨，需要先暂停上游放量再处理死信。",
			})
			recordProcessorBatch(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
			continue
		}
		if len(results) == 0 {
			for _, event := range group {
				success[event.EventID] = struct{}{}
			}
			groupSuccessCount = len(group)
			recordProcessorBatch(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
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
				Status:    "失败事件已按重试策略回写 outbox，成功事件继续完成",
				Component: "collector",
				Operation: "process_partial_failed",
				BizType:   bizType,
				Transport: collectorTransportDB,
				UniqueKey: bizType,
				Reason:    "collector processor 返回部分失败或遗漏结果",
				Count:     groupFailCount,
				Advice:    "请检查 Processor 返回结果是否覆盖所有输入事件，并确认失败事件的错误原因；若 retry/dead 数量持续上升，请优先处理该 bizType。",
			})
		}
		recordProcessorBatch(bizType, len(group), groupSuccessCount, groupFailCount, time.Since(beginAt))
	}
	return success, failed
}

// processBizBatch 执行单个 bizType 的业务 Processor，并把业务 panic 转成错误，避免 DB worker 异常退出。
func (m *Manager) processBizBatch(ctx context.Context, bizType string, p Processor, group []Event) (results []ProcessResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("collector processor panic bizType=%s panic=%v", bizType, recovered)
		}
	}()
	return p.ProcessBatch(ctx, group)
}

// groupEventsByBizType 按业务类型聚合事件，保证每个 Processor 只处理自己的数据批次。
func groupEventsByBizType(events []Event) map[string][]Event {
	grouped := make(map[string][]Event)
	for _, event := range events {
		grouped[event.BizType] = append(grouped[event.BizType], event)
	}
	return grouped
}

// deliveriesTransportLabel 归一化一批事件的来源通道标签，便于 metrics 聚合。
func deliveriesTransportLabel(deliveries []eventDelivery) string {
	if len(deliveries) == 0 {
		return collectorTransportUnknown
	}
	label := strings.TrimSpace(deliveries[0].transport)
	if label == "" {
		label = collectorTransportUnknown
	}
	for i := 1; i < len(deliveries); i++ {
		current := strings.TrimSpace(deliveries[i].transport)
		if current == "" {
			current = collectorTransportUnknown
		}
		if current != label {
			return collectorTransportMixed
		}
	}
	return label
}

// normalizeEvent 规范化事件关键字段，确保后续幂等、分组和回写状态都有稳定键。
func normalizeEvent(event *Event) {
	if event == nil {
		return
	}
	event.EventID = strings.TrimSpace(event.EventID)
	event.BizType = strings.TrimSpace(event.BizType)
	event.PartitionKey = strings.TrimSpace(event.PartitionKey)
	if event.EventID == "" {
		event.EventID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("null")
	}
}

// normalizeAndValidateEvent 规范化并校验事件，确保无 bizType 的非法事件不会进入 outbox 队列。
func normalizeAndValidateEvent(event *Event) error {
	normalizeEvent(event)
	if event == nil {
		return errors.Errorf("collector event 为空")
	}
	if event.BizType == "" {
		return errors.Errorf("collector event bizType 为空")
	}
	return nil
}

// persistDeliveries 将来自 Kafka/Redis 的事件先可靠写入 DB outbox。
// 只有写入成功或事件本身不可恢复时，调用方才允许提交 Kafka offset 或 ACK Redis 消息。
func (m *Manager) persistDeliveries(ctx context.Context, deliveries []eventDelivery) ([]eventDelivery, error) {
	if len(deliveries) == 0 {
		return nil, nil
	}
	valid := make([]eventDelivery, 0, len(deliveries))
	invalidCount := 0
	firstInvalidMessageID := ""
	firstInvalidError := ""
	for i := range deliveries {
		event := deliveries[i].event
		if err := normalizeAndValidateEvent(&event); err != nil {
			invalidCount++
			if firstInvalidMessageID == "" {
				firstInvalidMessageID = strings.TrimSpace(deliveries[i].messageID)
			}
			if firstInvalidError == "" {
				firstInvalidError = err.Error()
			}
			continue
		}
		deliveries[i].event = event
		valid = append(valid, deliveries[i])
	}
	if invalidCount > 0 {
		fields := []logx.LogField{logx.Field("count", invalidCount)}
		if firstInvalidMessageID != "" {
			fields = append(fields, logx.Field("message_id", firstInvalidMessageID))
		}
		loggerx.ErrorTextw(ctx, "采集器 事件无效", firstInvalidError, fields...)
		m.reportRuntimeAlert(ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindInvalidEvent,
			Title:     "【P1 Collector 事件无效】",
			Status:    "无效事件不会写入 DB outbox，已跳过以避免阻塞消费位点",
			Component: "collector",
			Operation: "persist_invalid_event",
			Transport: deliveriesTransportLabel(deliveries),
			UniqueKey: deliveriesTransportLabel(deliveries),
			Reason:    collectorAlertReason("messageId", firstInvalidMessageID, firstInvalidError),
			Count:     invalidCount,
			Advice:    "请检查上游投递的事件 JSON、bizType 和 payload 结构；无效事件已被跳过，必要时从 Kafka/Redis 原始载体或业务日志补偿。",
		})
	}
	if err := m.saveOutboxBatch(ctx, valid, model.CollectorOutboxStatePending, 0, time.Now(), ""); err != nil {
		return nil, errors.Tag(err)
	}
	return deliveries, nil
}

// runKafkaWorker 从 Kafka 批量拉取事件，先写入 DB outbox，再提交 offset。
func (m *Manager) runKafkaWorker() {
	reader := m.kafkaReader
	if reader == nil {
		return
	}
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		msg, err := reader.FetchMessage(m.ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			loggerx.Errorw(m.ctx, "采集器 消息队列拉取失败", err)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka 拉取失败】",
				Status:    "Kafka worker 暂停本轮拉取，500ms 后继续重试",
				Component: "collector",
				Operation: "kafka_fetch_message",
				Transport: collectorTransportKafka,
				UniqueKey: m.cfg.Kafka.Topic,
				Reason:    err.Error(),
				Advice:    "请检查 Kafka broker、topic、consumer group 和网络连接；若持续失败，Collector 将无法把 Kafka 事件落入 DB outbox。",
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		messages, deliveries, err := m.collectKafkaBatch(reader, msg)
		if err != nil {
			loggerx.Errorw(m.ctx, "采集器 消息队列批量收集失败", err)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka 批量收集失败】",
				Status:    "Kafka worker 暂停本轮批量收集，500ms 后继续重试",
				Component: "collector",
				Operation: "kafka_collect_batch",
				Transport: collectorTransportKafka,
				UniqueKey: m.cfg.Kafka.Topic,
				Reason:    err.Error(),
				Advice:    "请检查 Kafka 消息格式、批量拉取超时和 worker 上下文状态；持续失败会导致 Kafka 消费延迟增加。",
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if _, err := m.persistDeliveries(m.ctx, deliveries); err != nil {
			loggerx.Errorw(m.ctx, "采集器 消息队列落库失败", err)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka 落库失败】",
				Status:    "Kafka offset 暂不提交，500ms 后继续重试本批消息",
				Component: "collector",
				Operation: "kafka_persist_deliveries",
				Transport: collectorTransportKafka,
				UniqueKey: m.cfg.Kafka.Topic,
				Reason:    err.Error(),
				Count:     len(deliveries),
				Advice:    "请检查 collector_outbox 主库写入、索引冲突和连接池状态；修复前 Kafka 消费位点会停留在当前批次。",
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err := reader.CommitMessages(m.ctx, messages...); err != nil {
			loggerx.Errorw(m.ctx, "采集器 消息队列提交失败", err)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Kafka Offset 提交失败】",
				Status:    "事件已落 DB outbox，但 Kafka offset 提交失败，可能产生重复消费",
				Component: "collector",
				Operation: "kafka_commit_messages",
				Transport: collectorTransportKafka,
				UniqueKey: m.cfg.Kafka.Topic,
				Reason:    err.Error(),
				Count:     len(messages),
				Advice:    "请检查 Kafka consumer group 状态和 broker 连接；DB outbox 依赖 event_id 幂等，恢复后关注重复消费和 RowsAffected。",
			})
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// collectKafkaBatch 收集一小批 Kafka 消息，减少频繁 DB insert 的调用次数。
func (m *Manager) collectKafkaBatch(reader *kafka.Reader, first kafka.Message) ([]kafka.Message, []eventDelivery, error) {
	if reader == nil {
		return nil, nil, errors.Errorf("collector kafka reader 未初始化")
	}
	batchSize := m.kafkaFetchBatchSize()
	messages := make([]kafka.Message, 0, batchSize)
	deliveries := make([]eventDelivery, 0, batchSize)
	decodeFailedCount := 0
	firstDecodeError := ""
	appendMessage := func(msg kafka.Message) {
		messages = append(messages, msg)
		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			decodeFailedCount++
			if firstDecodeError == "" {
				firstDecodeError = err.Error()
			}
			return
		}
		deliveries = append(deliveries, eventDelivery{
			event:     event,
			transport: collectorTransportKafka,
		})
	}
	appendMessage(first)
	for len(messages) < batchSize {
		fetchCtx, cancel := context.WithTimeout(m.ctx, defaultKafkaFetchWait)
		next, err := reader.FetchMessage(fetchCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				break
			}
			if errors.Is(err, context.Canceled) && m.ctx.Err() != nil {
				break
			}
			return nil, nil, errors.Tag(err)
		}
		appendMessage(next)
	}
	if decodeFailedCount > 0 {
		fields := []logx.LogField{logx.Field("count", decodeFailedCount)}
		loggerx.ErrorTextw(m.ctx, "采集器 消息队列解码失败", firstDecodeError, fields...)
		m.reportRuntimeAlert(m.ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindInvalidEvent,
			Title:     "【P1 Collector Kafka 消息解码失败】",
			Status:    "解码失败消息不会进入 DB outbox，后续会提交 Kafka offset",
			Component: "collector",
			Operation: "kafka_decode_message",
			Transport: collectorTransportKafka,
			Reason:    firstDecodeError,
			Count:     decodeFailedCount,
			Advice:    "请检查 Kafka 生产方消息格式是否为 Collector Event JSON；如消息已提交 offset，需要按原始 Kafka 日志或业务源头补偿。",
		})
	}
	return messages, deliveries, nil
}

// kafkaFetchBatchSize 返回 Kafka 消费批次大小，未配置时使用安全默认值。
func (m *Manager) kafkaFetchBatchSize() int {
	if m == nil {
		return defaultKafkaFetchBatchSize
	}
	return boundedPositiveInt(m.cfg.Kafka.BatchSize, defaultKafkaFetchBatchSize, maxCollectorCarrierBatchSize)
}

// outboxInsertBatchSize 返回 outbox 批量写入大小，取各载体批次中的较大值以降低 DB 调用次数。
func (m *Manager) outboxInsertBatchSize() int {
	size := defaultKafkaFetchBatchSize
	if m != nil {
		if m.cfg.Kafka.BatchSize > size {
			size = m.cfg.Kafka.BatchSize
		}
		if m.cfg.Redis.Count > int64(size) {
			size = int(boundedPositiveInt64(m.cfg.Redis.Count, int64(size), maxCollectorCarrierBatchSize))
		}
		if m.cfg.DB.RunnerBatchSize > size {
			size = m.cfg.DB.RunnerBatchSize
		}
	}
	if size <= 0 {
		size = defaultKafkaFetchBatchSize
	}
	return boundedPositiveInt(size, defaultKafkaFetchBatchSize, maxCollectorCarrierBatchSize)
}

// runRedisWorker 从 Redis Stream 读取和认领事件，先写入 DB outbox，再 ACK 消息。
func (m *Manager) runRedisWorker() {
	if m.redis == nil {
		return
	}
	if err := m.ensureRedisGroup(m.ctx); err != nil {
		loggerx.Errorw(m.ctx, "采集器 缓存流消费组初始化失败", err,
			logx.Field("stream", m.cfg.Redis.Stream),
			logx.Field("group", m.cfg.Redis.Group),
		)
		m.reportRuntimeAlert(m.ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindWorkerFailed,
			Title:     "【P1 Collector Redis 消费组初始化失败】",
			Status:    "Redis Stream worker 未启动，需要人工处理后重启或重新触发",
			Component: "collector",
			Operation: "redis_init_group",
			Transport: collectorTransportRedis,
			UniqueKey: m.cfg.Redis.Stream + "/" + m.cfg.Redis.Group,
			Reason:    err.Error(),
			Advice:    "请检查 Redis Stream 名称、消费组、权限和 Redis 连通性；该 worker 已退出，修复后需要重启 worker 进程。",
		})
		return
	}
	block := time.Duration(m.cfg.Redis.BlockMs) * time.Millisecond
	if block <= 0 {
		block = 2 * time.Second
	}
	count := boundedPositiveInt64(m.cfg.Redis.Count, 100, maxCollectorCarrierBatchSize)
	claimStart := "0-0"
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		claimed, next, claimErr := m.redis.XAutoClaim(m.ctx, &redis.XAutoClaimArgs{
			Stream:   m.cfg.Redis.Stream,
			Group:    m.cfg.Redis.Group,
			Consumer: m.redisConsumerName(),
			MinIdle:  defaultRedisClaimIdle,
			Start:    claimStart,
			Count:    count,
		}).Result()
		if claimErr != nil && !errors.Is(claimErr, redis.Nil) {
			loggerx.Errorw(m.ctx, "采集器 缓存流自动认领失败", claimErr,
				logx.Field("stream", m.cfg.Redis.Stream),
				logx.Field("group", m.cfg.Redis.Group),
			)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Redis Pending 认领失败】",
				Status:    "本轮 pending 消息认领失败，后续循环会继续重试",
				Component: "collector",
				Operation: "redis_auto_claim",
				Transport: collectorTransportRedis,
				UniqueKey: m.cfg.Redis.Stream + "/" + m.cfg.Redis.Group,
				Reason:    claimErr.Error(),
				Advice:    "请检查 Redis Stream pending 状态、消费者组和 Redis 连接；持续失败会导致超时消息无法回收处理。",
			})
			claimStart = "0-0"
		} else if next != "" {
			// Redis XAUTOCLAIM 会返回下一次扫描游标，持续沿游标扫描可避免每轮都从 0-0 重扫大量 pending 消息。
			claimStart = next
		}
		deliveries := m.redisMessagesToDeliveries(claimed)
		res, err := m.redis.XReadGroup(m.ctx, &redis.XReadGroupArgs{
			Group:    m.cfg.Redis.Group,
			Consumer: m.redisConsumerName(),
			Streams:  []string{m.cfg.Redis.Stream, ">"},
			Count:    count,
			Block:    block,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			loggerx.Errorw(m.ctx, "采集器 缓存流读取失败", err,
				logx.Field("stream", m.cfg.Redis.Stream),
				logx.Field("group", m.cfg.Redis.Group),
			)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Redis 读取失败】",
				Status:    "Redis Stream 本轮读取失败，500ms 后继续重试",
				Component: "collector",
				Operation: "redis_read_group",
				Transport: collectorTransportRedis,
				UniqueKey: m.cfg.Redis.Stream + "/" + m.cfg.Redis.Group,
				Reason:    err.Error(),
				Advice:    "请检查 Redis 连接、Stream/Group 配置和实例负载；持续失败会导致 Redis 载体事件积压。",
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		deliveries = append(deliveries, m.redisStreamsToDeliveries(res)...)
		if len(deliveries) == 0 {
			continue
		}
		persisted, err := m.persistDeliveries(m.ctx, deliveries)
		if err != nil {
			loggerx.Errorw(m.ctx, "采集器 缓存流落库失败", err,
				logx.Field("stream", m.cfg.Redis.Stream),
				logx.Field("group", m.cfg.Redis.Group),
			)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Redis 落库失败】",
				Status:    "Redis 消息暂不 ACK，500ms 后继续重试本批消息",
				Component: "collector",
				Operation: "redis_persist_deliveries",
				Transport: collectorTransportRedis,
				UniqueKey: m.cfg.Redis.Stream + "/" + m.cfg.Redis.Group,
				Reason:    err.Error(),
				Count:     len(deliveries),
				Advice:    "请检查 collector_outbox 主库写入、索引冲突和连接池状态；修复前 Redis Stream pending 会增长。",
			})
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err := m.ackRedisDeliveries(m.ctx, persisted); err != nil {
			loggerx.Errorw(m.ctx, "采集器 缓存流确认失败", err,
				logx.Field("stream", m.cfg.Redis.Stream),
				logx.Field("group", m.cfg.Redis.Group),
			)
			m.reportRuntimeAlert(m.ctx, RuntimeAlert{
				Kind:      RuntimeAlertKindWorkerFailed,
				Title:     "【P1 Collector Redis ACK 失败】",
				Status:    "事件已落 DB outbox，但 Redis ACK/删除失败，可能产生重复消费",
				Component: "collector",
				Operation: "redis_ack_deliveries",
				Transport: collectorTransportRedis,
				UniqueKey: m.cfg.Redis.Stream + "/" + m.cfg.Redis.Group,
				Reason:    err.Error(),
				Count:     len(persisted),
				Advice:    "请检查 Redis Stream ACK/XDEL 权限和连接状态；DB outbox 依赖 event_id 幂等，恢复后关注重复消费。",
			})
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// redisConsumerName 返回当前实例独占的 Redis Stream consumer 名称。
func (m *Manager) redisConsumerName() string {
	if m == nil {
		return "collector"
	}
	base := strings.TrimSpace(m.cfg.Redis.Consumer)
	if base == "" {
		base = "collector"
	}
	if m.instanceID == "" {
		return base
	}
	return base + "-" + m.instanceID[:8]
}

// redisMessagesToDeliveries 将 Redis 消息转换为待落地事件，并丢弃不可解析消息。
func (m *Manager) redisMessagesToDeliveries(messages []redis.XMessage) []eventDelivery {
	deliveries := make([]eventDelivery, 0, len(messages))
	invalidCount := 0
	firstInvalidID := ""
	firstInvalidError := ""
	for _, msg := range messages {
		body, ok := msg.Values["body"]
		if !ok {
			invalidCount++
			if firstInvalidID == "" {
				firstInvalidID = msg.ID
				firstInvalidError = "Redis Stream 消息缺少 body 字段"
			}
			m.ackAndDeleteRedisMessage(msg.ID)
			continue
		}
		var text string
		switch v := body.(type) {
		case string:
			text = v
		case []byte:
			text = string(v)
		default:
			text = ""
		}
		var event Event
		if err := json.Unmarshal([]byte(text), &event); err != nil {
			invalidCount++
			if firstInvalidID == "" {
				firstInvalidID = msg.ID
				firstInvalidError = err.Error()
			}
			m.ackAndDeleteRedisMessage(msg.ID)
			continue
		}
		deliveries = append(deliveries, eventDelivery{
			event:     event,
			transport: collectorTransportRedis,
			messageID: msg.ID,
		})
	}
	if invalidCount > 0 {
		m.reportRuntimeAlert(m.ctx, RuntimeAlert{
			Kind:      RuntimeAlertKindInvalidEvent,
			Title:     "【P1 Collector Redis 消息无效】",
			Status:    "无效 Redis Stream 消息已 ACK 并删除，避免长期阻塞消费组",
			Component: "collector",
			Operation: "redis_decode_message",
			Transport: collectorTransportRedis,
			UniqueKey: collectorTransportRedis,
			Reason:    collectorAlertReason("messageId", firstInvalidID, firstInvalidError),
			Count:     invalidCount,
			Advice:    "请检查 Redis Stream 生产方是否写入 body 字段且内容为 Collector Event JSON；已删除的坏消息需要从业务源头补偿。",
		})
	}
	return deliveries
}

// redisStreamsToDeliveries 将 Redis Stream 读取结果展开为待落地事件。
func (m *Manager) redisStreamsToDeliveries(streams []redis.XStream) []eventDelivery {
	deliveries := make([]eventDelivery, 0)
	for _, stream := range streams {
		deliveries = append(deliveries, m.redisMessagesToDeliveries(stream.Messages)...)
	}
	return deliveries
}

// ackRedisDeliveries ACK 已经可靠落地或明确不可恢复的 Redis 消息。
func (m *Manager) ackRedisDeliveries(ctx context.Context, deliveries []eventDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(deliveries))
	for i := range deliveries {
		if deliveries[i].transport != collectorTransportRedis || strings.TrimSpace(deliveries[i].messageID) == "" {
			continue
		}
		ids = append(ids, deliveries[i].messageID)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := m.redis.XAck(ctx, m.cfg.Redis.Stream, m.cfg.Redis.Group, ids...).Err(); err != nil {
		return errors.Tag(err)
	}
	if err := m.redis.XDel(ctx, m.cfg.Redis.Stream, ids...).Err(); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// ackAndDeleteRedisMessage 确认并清理不可恢复的 Redis Stream 消息，避免坏消息长期占用 Stream。
func (m *Manager) ackAndDeleteRedisMessage(messageID string) {
	if m == nil || m.redis == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = m.redis.XAck(m.ctx, m.cfg.Redis.Stream, m.cfg.Redis.Group, messageID).Err()
	_ = m.redis.XDel(m.ctx, m.cfg.Redis.Stream, messageID).Err()
}

// ensureRedisGroup 确保 Redis Stream 消费组存在。
func (m *Manager) ensureRedisGroup(ctx context.Context) error {
	if m.redis == nil {
		return errors.Errorf("collector redis 未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.cfg.Redis.Stream == "" || m.cfg.Redis.Group == "" {
		return errors.Errorf("collector redis stream/group 未配置")
	}
	err := m.redis.XGroupCreateMkStream(ctx, m.cfg.Redis.Stream, m.cfg.Redis.Group, "0").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return errors.Tag(err)
}

// runDBWorker 定时从 DB outbox 批量领取事件并投送给 Processor。
func (m *Manager) runDBWorker() {
	intervalSeconds := m.cfg.DB.RunnerIntervalSeconds
	if intervalSeconds <= 0 {
		intervalSeconds = defaultDBRunnerIntervalSeconds
	}
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.runDBOnce(m.ctx, 0); err != nil {
				loggerx.Errorw(m.ctx, "采集器 数据库执行失败", err)
				if strings.Contains(err.Error(), "collector 批量消费存在失败") {
					continue
				}
				m.reportRuntimeAlert(m.ctx, RuntimeAlert{
					Kind:      RuntimeAlertKindWorkerFailed,
					Title:     "【P1 Collector DB Worker 执行失败】",
					Status:    "DB outbox 本轮消费失败，下一轮会继续重试",
					Component: "collector",
					Operation: "db_run_once",
					Transport: collectorTransportDB,
					Reason:    err.Error(),
					Advice:    "请检查 collector_outbox 查询、状态回写、Processor 业务依赖和数据库连接池；若失败持续出现，请查看 Collector 管理页积压与死信。",
				})
			}
		}
	}
}

// runDBOnce 执行一轮 DB outbox 批量消费。
func (m *Manager) runDBOnce(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	beginAt := time.Now()
	limit = boundedPositiveInt(limit, m.dbRunnerBatchSize(), maxCollectorCarrierBatchSize)
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
	rowByEventID := make(map[string]model.CollectorOutbox, len(rows))
	failRows := make([]model.CollectorOutbox, 0, len(rows))
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
	for eventID := range success {
		row, ok := rowByEventID[eventID]
		if !ok {
			continue
		}
		successIDs = append(successIDs, row.ID)
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
	}
	if len(failRows) > 0 {
		if err := m.markFailed(ctx, failRows, errors.Errorf("collector 批量消费存在失败"), time.Now()); err != nil {
			return len(successIDs), errors.Tag(err)
		}
	}
	if len(failRows) > 0 {
		m.logSlowBatch(ctx, "processor_batch_failed", len(rows), beginAt)
		return len(successIDs), errors.Errorf("collector 批量消费存在失败 success=%d failed=%d", len(successIDs), len(failRows))
	}
	m.logSlowBatch(ctx, "processor_batch", len(rows), beginAt)
	return len(successIDs), nil
}

// claimDue 使用 SKIP LOCKED 批量领取到期事件，保证多实例并发安全。
func (m *Manager) claimDue(ctx context.Context, limit int, now time.Time) ([]model.CollectorOutbox, error) {
	var rows []model.CollectorOutbox
	err := m.outbox.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&model.CollectorOutbox{}).
			Where("state IN ? AND next_run_at <= ?", []model.CollectorOutboxState{
				model.CollectorOutboxStatePending,
				model.CollectorOutboxStateRetry,
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
			"state":      model.CollectorOutboxStateRunning,
			"started_at": startedAt,
			"updated_at": startedAt,
		}
		if err := tx.Model(&model.CollectorOutbox{}).Where("id IN ?", ids).Updates(updates).Error; err != nil {
			return errors.Tag(err)
		}
		for i := range rows {
			rows[i].State = model.CollectorOutboxStateRunning
			rows[i].StartedAt = &startedAt
		}
		return nil
	})
	return rows, errors.Tag(err)
}

// recoverExpiredRunning 回收租约超时的 running 事件，避免 worker 异常退出造成永久卡死。
func (m *Manager) recoverExpiredRunning(ctx context.Context, limit int, now time.Time) error {
	leaseSeconds := m.cfg.DB.RunningLeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = defaultDBRunningLeaseSeconds
	}
	if limit <= 0 {
		limit = m.dbRunnerBatchSize()
	}
	cutoff := now.Add(-time.Duration(leaseSeconds) * time.Second)
	rows := make([]model.CollectorOutbox, 0, limit)
	if err := m.outbox.WithContext(ctx).
		Model(&model.CollectorOutbox{}).
		Where("state = ? AND started_at IS NOT NULL AND started_at <= ?", model.CollectorOutboxStateRunning, cutoff).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return errors.Tag(err)
	}
	if len(rows) == 0 {
		return nil
	}
	return errors.Tag(m.markFailed(ctx, rows, errors.Errorf("collector running 任务租约超时，已回收等待重试"), now))
}

// markDone 批量标记成功事件，使用 running 状态校验避免误更新。
func (m *Manager) markDone(ctx context.Context, ids []int64, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	updates := map[string]any{
		"state":       model.CollectorOutboxStateDone,
		"finished_at": now,
		"updated_at":  now,
		"last_error":  "",
	}
	result := m.outbox.WithContext(ctx).
		Model(&model.CollectorOutbox{}).
		Where("id IN ? AND state = ?", ids, model.CollectorOutboxStateRunning).
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
func (m *Manager) markFailed(ctx context.Context, rows []model.CollectorOutbox, execErr error, now time.Time) error {
	maxRetry := m.cfg.DB.MaxRetryTimes
	if maxRetry <= 0 {
		maxRetry = defaultDBMaxRetryTimes
	}
	deadCount := 0
	deadBizType := ""
	deadEventID := ""
	err := m.outbox.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			newAttempt := row.Attempt + 1
			nextState := model.CollectorOutboxStateRetry
			nextRunAt := now
			finishedAt := (*time.Time)(nil)
			if newAttempt >= maxRetry {
				nextState = model.CollectorOutboxStateDead
				finishedAt = &now
				deadCount++
				if deadBizType == "" {
					deadBizType = strings.TrimSpace(row.BizType)
				}
				if deadEventID == "" {
					deadEventID = strings.TrimSpace(row.EventID)
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
			result := tx.Model(&model.CollectorOutbox{}).
				Where("id = ? AND state = ?", row.ID, model.CollectorOutboxStateRunning).
				Updates(updates)
			if result.Error != nil {
				return errors.Tag(result.Error)
			}
			if result.RowsAffected == 0 {
				return errors.Errorf("collectorx.markFailed 任务状态已变化 id=%d", row.ID)
			}
			if err := result.Error; err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
	if err != nil {
		return errors.Tag(err)
	}
	if deadCount > 0 {
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
			BizType:   deadBizType,
			Transport: collectorTransportDB,
			UniqueKey: deadBizType,
			Reason:    reason,
			Count:     deadCount,
			Advice:    "请在 Collector 管理页按死信状态和 bizType 检索，确认业务影响范围后修复数据或处理器，再人工重试；必要时可用首个事件ID " + deadEventID + " 定位。",
		})
	}
	return nil
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

// dbRunnerBatchSize 返回 DB worker 单轮消费批次大小。
func (m *Manager) dbRunnerBatchSize() int {
	if m == nil {
		return defaultDBRunnerBatchSize
	}
	return boundedPositiveInt(m.cfg.DB.RunnerBatchSize, defaultDBRunnerBatchSize, maxCollectorCarrierBatchSize)
}

// boundedPositiveInt 返回带上限的正整数配置，避免误配置过大批次拖垮 DB、Redis 或内存。
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

// boundedPositiveInt64 返回带上限的正整数配置，供 Redis Count 等 int64 字段复用。
func boundedPositiveInt64(value int64, fallback int64, maxValue int64) int64 {
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
