//go:build integration

package collectorx

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/model"

	"github.com/segmentio/kafka-go"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestPoisonKafkaMessagePersistsSafeDeadSeedOnMySQL 验证异常大消息会缩减为可真实落库的有界死信。
func TestPoisonKafkaMessagePersistsSafeDeadSeedOnMySQL(t *testing.T) {
	db := prepareCollectorIntegrationTable(t)
	manager := newIntegrationManager(db, 30)
	msg := kafka.Message{
		Topic:     strings.Repeat("主题", 100),
		Partition: 8,
		Offset:    42,
		Key:       bytes.Repeat([]byte{0xff}, maxInvalidKafkaKeyBytes+100),
		Value:     bytes.Repeat([]byte{0xff}, maxCollectorKafkaMessageBytes+100),
	}
	item := manager.kafkaWorkItem(msg)
	if item.failure == nil || item.failure.state != model.CollectorFailedEventStateDead {
		t.Fatalf("poison message 未转为 dead seed: %+v", item)
	}
	if err := manager.saveFailureEvents(context.Background(), []failureEventSeed{*item.failure}); err != nil {
		t.Fatalf("saveFailureEvents() error = %v", err)
	}
	var row model.CollectorFailedEvent
	if err := db.Where("event_id = ?", item.failure.event.EventID).First(&row).Error; err != nil {
		t.Fatalf("查询 poison dead seed error = %v", err)
	}
	if len(row.EventID) > maxCollectorEventIDBytes || len(row.BizType) > maxCollectorBizTypeBytes || len(row.PartitionKey) > maxCollectorPartitionKeyBytes || len(row.Payload) > maxCollectorPayloadBytes {
		t.Fatalf("poison dead seed 落库后字段超限: %+v", row)
	}
	if !json.Valid([]byte(row.Payload)) {
		t.Fatalf("poison dead seed payload 不是有效 JSON: %q", row.Payload)
	}
}

// TestFailureClaimTokenRejectsLateWriterOnMySQL 验证旧 Worker 不能覆盖回收后的新领取者。
func TestFailureClaimTokenRejectsLateWriterOnMySQL(t *testing.T) {
	db := prepareCollectorIntegrationTable(t)
	now := time.Now().Truncate(time.Millisecond)
	insertCollectorFailure(t, db, "event-fence", now)
	first := newIntegrationManager(db, 30)
	firstRows, err := first.claimDue(context.Background(), 1, now)
	if err != nil || len(firstRows) != 1 {
		t.Fatalf("first claimDue() rows=%+v err=%v", firstRows, err)
	}
	if err = db.Model(&model.CollectorFailedEvent{}).Where("id = ?", firstRows[0].ID).
		Update("lease_until", now.Add(-time.Second)).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second := newIntegrationManager(db, 30)
	if err = second.recoverExpiredRunning(context.Background(), 1, now); err != nil {
		t.Fatalf("recoverExpiredRunning() error = %v", err)
	}
	if err = db.Model(&model.CollectorFailedEvent{}).Where("id = ?", firstRows[0].ID).
		Update("next_run_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatalf("make recovered row due: %v", err)
	}
	secondRows, err := second.claimDue(context.Background(), 1, now)
	if err != nil || len(secondRows) != 1 {
		t.Fatalf("second claimDue() rows=%+v err=%v", secondRows, err)
	}
	if firstRows[0].ClaimToken == secondRows[0].ClaimToken {
		t.Fatal("重新领取复用了旧 claim_token")
	}
	if err = first.markDone(context.Background(), firstRows, now.Add(time.Second)); err == nil {
		t.Fatal("旧 Worker 晚到回写应被 token CAS 拒绝")
	}
	firstRows[0].LastError = "late failure"
	if err = first.markFailed(context.Background(), firstRows, context.Canceled, now.Add(time.Second)); err == nil {
		t.Fatal("旧 Worker 晚到失败回写应被 token CAS 拒绝")
	}
	if err = second.markDone(context.Background(), secondRows, now.Add(time.Second)); err != nil {
		t.Fatalf("新 Worker markDone() error = %v", err)
	}
}

// TestFailureClaimRenewPreventsRecoveryOnMySQL 验证续租后的运行事件不会按旧到期时间被第二个 Worker 回收。
func TestFailureClaimRenewPreventsRecoveryOnMySQL(t *testing.T) {
	db := prepareCollectorIntegrationTable(t)
	now := time.Now().Truncate(time.Millisecond)
	insertCollectorFailure(t, db, "event-renew", now)
	owner := newIntegrationManager(db, 2)
	rows, err := owner.claimDue(context.Background(), 1, now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("claimDue() rows=%+v err=%v", rows, err)
	}
	if err = owner.renewFailureClaims(context.Background(), rows, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatalf("renewFailureClaims() error = %v", err)
	}
	other := newIntegrationManager(db, 2)
	if err = other.recoverExpiredRunning(context.Background(), 1, now.Add(2100*time.Millisecond)); err != nil {
		t.Fatalf("recoverExpiredRunning() error = %v", err)
	}
	var row model.CollectorFailedEvent
	if err = db.First(&row, rows[0].ID).Error; err != nil {
		t.Fatalf("load claimed row: %v", err)
	}
	if row.State != model.CollectorFailedEventStateRunning || row.ClaimToken != rows[0].ClaimToken {
		t.Fatalf("续租后的 claim 被错误回收: %+v", row)
	}
}

// TestFailureRetryHeartbeatKeepsLongProcessorLeaseOnMySQL 验证长批处理期间续租可阻止双 Worker 并发领取。
func TestFailureRetryHeartbeatKeepsLongProcessorLeaseOnMySQL(t *testing.T) {
	db := prepareCollectorIntegrationTable(t)
	insertCollectorFailure(t, db, "event-long", time.Now())
	manager := newIntegrationManager(db, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := manager.RegisterProcessorFunc("biz", func(ctx context.Context, _ []Event) ([]ProcessResult, error) {
		close(started)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}); err != nil {
		t.Fatalf("RegisterProcessorFunc() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := manager.RunNow(context.Background(), 1)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("等待长 Processor 启动超时")
	}
	time.Sleep(1200 * time.Millisecond)
	other := newIntegrationManager(db, 1)
	if err := other.recoverExpiredRunning(context.Background(), 1, time.Now()); err != nil {
		t.Fatalf("other recoverExpiredRunning() error = %v", err)
	}
	var running int64
	if err := db.Model(&model.CollectorFailedEvent{}).Where("state = ?", model.CollectorFailedEventStateRunning).Count(&running).Error; err != nil {
		t.Fatalf("count running row: %v", err)
	}
	if running != 1 {
		t.Fatalf("长 Processor 续租期间运行记录数=%d, want 1", running)
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("RunNow() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待长 Processor 完成超时")
	}
}

// TestFailureRetryCancelsProcessorAfterClaimLossOnMySQL 验证 claim_token 失权会取消 Processor 且旧 Worker 不回写。
func TestFailureRetryCancelsProcessorAfterClaimLossOnMySQL(t *testing.T) {
	db := prepareCollectorIntegrationTable(t)
	insertCollectorFailure(t, db, "event-cancel", time.Now())
	manager := newIntegrationManager(db, 1)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	if err := manager.RegisterProcessorFunc("biz", func(ctx context.Context, _ []Event) ([]ProcessResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("RegisterProcessorFunc() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := manager.RunNow(context.Background(), 1)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("等待 Processor 启动超时")
	}
	if err := db.Model(&model.CollectorFailedEvent{}).
		Where("state = ?", model.CollectorFailedEventStateRunning).
		Update("claim_token", "replacement-owner").Error; err != nil {
		t.Fatalf("replace claim token: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("claim 失权后 Processor 未收到取消")
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "claim") {
			t.Fatalf("RunNow() err=%v, want claim lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待失权 RunNow 返回超时")
	}
	var row model.CollectorFailedEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("load claim-lost row: %v", err)
	}
	if row.State != model.CollectorFailedEventStateRunning || row.ClaimToken != "replacement-owner" {
		t.Fatalf("旧 Worker 覆盖了新 claim: %+v", row)
	}
}

// newIntegrationManager 创建真实 MySQL 并发测试 Manager。
func newIntegrationManager(db *gorm.DB, leaseSeconds int) *Manager {
	return &Manager{
		cfg: config.CollectorConfig{
			Enabled: true,
			DefaultTask: config.CollectorTaskConfig{
				ProcessorTimeoutSeconds: 5,
			},
			FailureRetry: config.CollectorFailureRetryConfig{
				RunningLeaseSeconds: leaseSeconds,
				MaxRetryTimes:       8,
			},
		},
		failures:   db,
		processors: make(map[string]Processor),
	}
}

// insertCollectorFailure 写入一条立即可领取的失败事件。
func insertCollectorFailure(t *testing.T, db *gorm.DB, eventID string, now time.Time) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"id": eventID})
	row := model.CollectorFailedEvent{
		EventID:      eventID,
		BizType:      "biz",
		PartitionKey: "partition",
		Payload:      string(payload),
		State:        model.CollectorFailedEventStateRetry,
		Attempt:      1,
		NextRunAt:    now.Add(-time.Second),
		LastError:    "retry",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert collector failure: %v", err)
	}
}

// prepareCollectorIntegrationTable 重建失败账本测试表。
func prepareCollectorIntegrationTable(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("INTEGRATION_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("INTEGRATION_MYSQL_DSN 未配置")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.Migrator().DropTable(&model.CollectorFailedEvent{}); err != nil {
		t.Fatalf("drop collector_failed_event: %v", err)
	}
	if err = db.AutoMigrate(&model.CollectorFailedEvent{}); err != nil {
		t.Fatalf("migrate collector_failed_event: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.CollectorFailedEvent{}) })
	return db
}
