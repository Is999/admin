package collectorx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"admin/internal/infra/loggerx"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// reportLeaseRenewFailure 上报所有权续租异常，便于区分 Processor 业务失败和基础设施失权。
func (m *Manager) reportLeaseRenewFailure(ctx context.Context, scope string, bizType string, count int, cause error) {
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	m.reportRuntimeAlert(ctx, RuntimeAlert{
		Kind:      RuntimeAlertKindWorkerFailed,
		Title:     "【P1 Collector 租约续期失败】",
		Status:    "当前 Processor 已取消，旧领取者不会回写终态",
		Component: "collector",
		Operation: "lease_renew_" + strings.TrimSpace(scope),
		BizType:   bizType,
		Channel:   collectorRuntimeChannelKafka,
		UniqueKey: strings.TrimSpace(scope),
		Reason:    reason,
		Count:     count,
		Advice:    "请检查 Redis 或 MySQL 连接、租约配置和 Processor 耗时；确认依赖恢复后由 Kafka 重投或失败账本自动回收继续处理。",
	})
}

// failureSeedsFromResults 将 Processor 失败结果转成失败账本写入快照。
func (m *Manager) failureSeedsFromResults(events []Event, failed map[eventKey]string, now time.Time) []failureEventSeed {
	rows := make([]failureEventSeed, 0, len(failed))
	for _, event := range events {
		msg, ok := failed[eventKeyOf(event)]
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
		reason = truncateErr(cause.Error(), maxCollectorLastErrorBytes)
	}
	payload := invalidKafkaSnapshot{
		Topic:              truncateTextBytes(strings.TrimSpace(msg.Topic), maxInvalidKafkaTopicBytes),
		Partition:          msg.Partition,
		Offset:             msg.Offset,
		Key:                truncateTextBytes(string(msg.Key), maxInvalidKafkaKeyBytes),
		Raw:                truncateTextBytes(string(msg.Value), maxInvalidKafkaRawBytes),
		OriginalKeyBytes:   len(msg.Key),
		OriginalValueBytes: len(msg.Value),
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > maxCollectorPayloadBytes || !utf8.Valid(body) || !json.Valid(body) {
		body = []byte(`{"raw":""}`)
	}
	return failureEventSeed{
		event: Event{
			EventID:      invalidKafkaEventID(msg),
			BizType:      "collector.invalid",
			PartitionKey: invalidKafkaPartitionKey(msg),
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
	return "collector:invalid:" + hex.EncodeToString(sum[:16])
}

// invalidKafkaPartitionKey 生成固定长度的坏消息分区键，避免超长 Topic 击穿失败账本字段。
func invalidKafkaPartitionKey(msg kafka.Message) string {
	sum := sha256.Sum256([]byte(msg.Topic))
	return fmt.Sprintf("collector.invalid:%s:%d", hex.EncodeToString(sum[:8]), msg.Partition)
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
		event := seed.event
		if err := normalizeAndValidateEvent(&event); err != nil {
			return errors.Wrap(err, "collector 失败账本事件不符合存储契约")
		}
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
			EventID:      event.EventID,
			BizType:      event.BizType,
			PartitionKey: event.PartitionKey,
			Payload:      string(event.Payload),
			State:        seed.state,
			Attempt:      seed.attempt,
			NextRunAt:    nextRunAt,
			StartedAt:    nil,
			FinishedAt:   finishedAt,
			LastError:    truncateErr(seed.lastError, maxCollectorLastErrorBytes),
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
	processCtx, cancelProcess := context.WithCancel(ctx)
	heartbeat := m.startFailureHeartbeat(ctx, rows, cancelProcess)
	events := make([]Event, 0, len(rows))
	rowByEventKey := make(map[eventKey]model.CollectorFailedEvent, len(rows))
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
		rowByEventKey[eventKeyOf(event)] = row
	}
	success, failed := m.processBatch(processCtx, events)
	if err := heartbeat.StopAndRenew(); err != nil {
		cancelProcess()
		m.reportLeaseRenewFailure(ctx, "failure_ledger", firstFailureBizType(rows), len(rows), err)
		return 0, errors.Wrap(err, "collector 失败账本租约续期失败")
	}
	cancelProcess()
	successRows := make([]model.CollectorFailedEvent, 0, len(success))
	for key := range success {
		row, ok := rowByEventKey[key]
		if !ok {
			continue
		}
		successRows = append(successRows, row)
	}
	for key, msg := range failed {
		row, ok := rowByEventKey[key]
		if !ok {
			continue
		}
		row.LastError = msg
		failRows = append(failRows, row)
	}
	if len(successRows) > 0 {
		if err := m.markDone(ctx, successRows, time.Now()); err != nil {
			return len(successRows), errors.Tag(err)
		}
	}
	if len(failRows) > 0 {
		if err := m.markFailed(ctx, failRows, errors.Errorf("collector 批量消费存在失败"), time.Now()); err != nil {
			return len(successRows), errors.Tag(err)
		}
	}
	if len(failRows) > 0 {
		m.logSlowBatch(ctx, "failure_retry_failed", len(rows), beginAt)
		return len(successRows), errors.Errorf("collector 批量消费存在失败 success=%d failed=%d", len(successRows), len(failRows))
	}
	m.logSlowBatch(ctx, "failure_retry_batch", len(rows), beginAt)
	return len(successRows), nil
}

// startFailureHeartbeat 在失败账本 Processor 运行期间续期当前 claim_token。
func (m *Manager) startFailureHeartbeat(ctx context.Context, rows []model.CollectorFailedEvent, cancelProcess context.CancelFunc) *leaseHeartbeat {
	if len(rows) == 0 {
		return nil
	}
	return startLeaseHeartbeat(ctx, leaseRenewInterval(m.failureRunningLease()), cancelProcess, func(renewCtx context.Context) error {
		return errors.Tag(m.renewFailureClaims(renewCtx, rows, time.Now()))
	})
}

// firstFailureBizType 返回失败账本批次首个业务类型，用于低基数告警定位。
func firstFailureBizType(rows []model.CollectorFailedEvent) string {
	if len(rows) == 0 {
		return ""
	}
	return strings.TrimSpace(rows[0].BizType)
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
		claimToken, err := newFailureClaimToken()
		if err != nil {
			return errors.Wrap(err, "生成 collector 失败账本 claim token 失败")
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		startedAt := now
		leaseUntil := startedAt.Add(m.failureRunningLease())
		updates := map[string]any{
			"state":       model.CollectorFailedEventStateRunning,
			"started_at":  startedAt,
			"claim_token": claimToken,
			"lease_until": leaseUntil,
			"updated_at":  startedAt,
		}
		result := tx.Model(&model.CollectorFailedEvent{}).
			Where("id IN ? AND state IN ?", ids, []model.CollectorFailedEventState{
				model.CollectorFailedEventStatePending,
				model.CollectorFailedEventStateRetry,
			}).
			Updates(updates)
		if result.Error != nil {
			return errors.Tag(result.Error)
		}
		if result.RowsAffected != int64(len(rows)) {
			return errors.Errorf("collectorx.claimDue 领取行数不一致 expect=%d actual=%d", len(rows), result.RowsAffected)
		}
		for i := range rows {
			rows[i].State = model.CollectorFailedEventStateRunning
			rows[i].StartedAt = &startedAt
			rows[i].ClaimToken = claimToken
			rows[i].LeaseUntil = &leaseUntil
		}
		return nil
	})
	return rows, errors.Tag(err)
}

// renewFailureClaims 批量续期当前 token 持有的失败账本记录，租约过期后禁止复活旧领取。
func (m *Manager) renewFailureClaims(ctx context.Context, rows []model.CollectorFailedEvent, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	token := strings.TrimSpace(rows[0].ClaimToken)
	if token == "" {
		return errors.Errorf("collector 失败账本 claim_token 为空")
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ClaimToken) != token {
			return errors.Errorf("collector 失败账本批次包含多个 claim_token")
		}
		ids = append(ids, row.ID)
	}
	leaseUntil := now.Add(m.failureRunningLease())
	result := m.failures.WithContext(ctx).
		Model(&model.CollectorFailedEvent{}).
		Where("id IN ? AND state = ? AND claim_token = ? AND lease_until > ?", ids, model.CollectorFailedEventStateRunning, token, now).
		Updates(map[string]any{"lease_until": leaseUntil, "updated_at": now})
	metricResult := "success"
	if result.Error != nil || result.RowsAffected != int64(len(rows)) {
		metricResult = "failed"
	}
	recordLeaseRenew("failure_ledger", metricResult)
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != int64(len(rows)) {
		return errors.Errorf("collector 失败账本 claim 已失效 expect=%d actual=%d", len(rows), result.RowsAffected)
	}
	return nil
}

// recoverExpiredRunning 回收租约超时的重试中事件，避免 worker 异常退出造成永久卡死。
func (m *Manager) recoverExpiredRunning(ctx context.Context, limit int, now time.Time) error {
	if limit <= 0 {
		limit = m.failureRetryBatchSize()
	}
	recoverErr := errors.Errorf("collector running 失败事件租约超时，已回收等待重试")
	rows := make([]model.CollectorFailedEvent, 0, limit)
	summary := collectorDeadSummary{}
	err := m.failures.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&model.CollectorFailedEvent{}).
			Where("state = ? AND lease_until IS NOT NULL AND lease_until <= ?", model.CollectorFailedEventStateRunning, now).
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
		summary, err = m.markFailedInTx(tx, rows, recoverErr, now, false)
		return errors.Tag(err)
	})
	if err != nil {
		return errors.Tag(err)
	}
	m.reportDeadEvents(ctx, summary, recoverErr)
	return nil
}

// markDone 批量标记重试成功事件，使用 token 和有效租约阻止旧领取者回写。
func (m *Manager) markDone(ctx context.Context, rows []model.CollectorFailedEvent, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	token := strings.TrimSpace(rows[0].ClaimToken)
	if token == "" {
		return errors.Errorf("collectorx.markDone claim_token 为空")
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ClaimToken) != token {
			return errors.Errorf("collectorx.markDone 批次包含多个 claim_token")
		}
		ids = append(ids, row.ID)
	}
	updates := map[string]any{
		"state":       model.CollectorFailedEventStateDone,
		"finished_at": now,
		"updated_at":  now,
		"last_error":  "",
		"claim_token": "",
		"lease_until": nil,
	}
	result := m.failures.WithContext(ctx).
		Model(&model.CollectorFailedEvent{}).
		Where("id IN ? AND state = ? AND claim_token = ? AND lease_until > ?", ids, model.CollectorFailedEventStateRunning, token, now).
		Updates(updates)
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != int64(len(rows)) {
		return errors.Errorf("collectorx.markDone claim 已失效 expect=%d actual=%d", len(rows), result.RowsAffected)
	}
	return nil
}

// markFailed 按指数退避策略回写失败事件，超过最大次数后进入死信。
func (m *Manager) markFailed(ctx context.Context, rows []model.CollectorFailedEvent, execErr error, now time.Time) error {
	summary := collectorDeadSummary{}
	err := m.failures.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		summary, err = m.markFailedInTx(tx, rows, execErr, now, true)
		return errors.Tag(err)
	})
	if err != nil {
		return errors.Tag(err)
	}
	m.reportDeadEvents(ctx, summary, execErr)
	return nil
}

// markFailedInTx 在同一事务内把 running 事件回写为 retry/dead。
func (m *Manager) markFailedInTx(tx *gorm.DB, rows []model.CollectorFailedEvent, execErr error, now time.Time, requireActiveLease bool) (collectorDeadSummary, error) {
	summary := collectorDeadSummary{}
	if tx == nil {
		return summary, errors.Errorf("collectorx.markFailedInTx tx 为空")
	}
	maxRetry := m.failureMaxRetryTimes()
	for _, row := range rows {
		claimToken := strings.TrimSpace(row.ClaimToken)
		if claimToken == "" {
			return summary, errors.Errorf("collectorx.markFailed claim_token 为空 id=%d", row.ID)
		}
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
			"claim_token": "",
			"lease_until": nil,
			"updated_at":  now,
			"last_error":  truncateErr(lastError, maxCollectorLastErrorBytes),
		}
		if finishedAt != nil {
			updates["finished_at"] = *finishedAt
		} else {
			updates["finished_at"] = nil
		}
		query := tx.Model(&model.CollectorFailedEvent{}).
			Where("id = ? AND state = ? AND claim_token = ?", row.ID, model.CollectorFailedEventStateRunning, claimToken)
		if requireActiveLease {
			query = query.Where("lease_until > ?", now)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return summary, errors.Tag(result.Error)
		}
		if result.RowsAffected == 0 {
			return summary, errors.Errorf("collectorx.markFailed claim 已失效 id=%d", row.ID)
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
