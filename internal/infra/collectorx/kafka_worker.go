package collectorx

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"admin/internal/config"
	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

// runKafkaWorker 持续消费 Kafka 消息，并按分区顺序交给批处理链路。
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
	if len(msg.Value) > maxCollectorKafkaMessageBytes {
		seed := m.invalidKafkaFailure(msg, errors.Errorf("collector Kafka 消息超过 %d 字节", maxCollectorKafkaMessageBytes))
		return kafkaWorkItem{message: msg, failure: &seed}
	}
	if !utf8.Valid(msg.Value) {
		seed := m.invalidKafkaFailure(msg, errors.Errorf("collector Kafka 消息不是有效 UTF-8"))
		return kafkaWorkItem{message: msg, failure: &seed}
	}
	if !json.Valid(msg.Value) {
		seed := m.invalidKafkaFailure(msg, errors.Errorf("collector Kafka 消息不是有效 JSON"))
		return kafkaWorkItem{message: msg, failure: &seed}
	}
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
	processEvents, claims, duplicateCount, err := m.beginIdempotentEvents(ctx, batch.events)
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
	processCtx, cancelProcess := context.WithCancel(ctx)
	heartbeat := m.startIdempotencyHeartbeat(ctx, claims, cancelProcess)
	success, failed := m.processBatch(processCtx, processEvents)
	if err := heartbeat.StopAndRenew(); err != nil {
		cancelProcess()
		m.reportLeaseRenewFailure(ctx, "redis_idempotency", kafkaBatchBizType(batch), len(claims), err)
		return errors.Wrap(err, "collector Redis 幂等租约续期失败")
	}
	cancelProcess()
	if len(failed) == 0 {
		if err := m.idempotency.done(ctx, claims); err != nil {
			return errors.Tag(err)
		}
		m.logSlowBatch(ctx, "kafka_processor_batch", len(processEvents), beginAt)
		recordKafkaConsume("processed", len(processEvents))
		return nil
	}
	successClaims := idempotencyClaimsFromSet(claims, success)
	failedClaims := idempotencyClaimsFromFailures(claims, failed)
	if err := m.idempotency.done(ctx, successClaims); err != nil {
		return errors.Tag(err)
	}
	rows := m.failureSeedsFromResults(processEvents, failed, time.Now())
	if err := m.saveFailureEvents(ctx, rows); err != nil {
		if releaseErr := m.idempotency.release(ctx, failedClaims); releaseErr != nil {
			return errors.Wrap(releaseErr, "collector 失败账本写入失败后释放幂等占用失败")
		}
		return errors.Tag(err)
	}
	if err := m.idempotency.failed(ctx, failedClaims); err != nil {
		return errors.Tag(err)
	}
	m.logSlowBatch(ctx, "kafka_processor_batch_failed", len(processEvents), beginAt)
	recordKafkaConsume("processed", len(success))
	recordKafkaConsume("failed", len(failed))
	return nil
}

// startIdempotencyHeartbeat 在 Processor 运行期间续期 Redis processing token。
func (m *Manager) startIdempotencyHeartbeat(ctx context.Context, claims []idempotencyClaim, cancelProcess context.CancelFunc) *leaseHeartbeat {
	if len(claims) == 0 {
		return nil
	}
	store := m.idempotency
	if store == nil {
		return nil
	}
	return startLeaseHeartbeat(ctx, leaseRenewInterval(store.processingTTL), cancelProcess, func(renewCtx context.Context) error {
		return errors.Tag(m.renewIdempotentClaims(renewCtx, claims))
	})
}
