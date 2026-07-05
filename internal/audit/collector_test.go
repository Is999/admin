package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"admin/internal/infra/collectorx"
	"admin/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type fakeCollector struct {
	events []collectorx.Event // 已投递的 Collector 事件
}

func (f *fakeCollector) Enqueue(_ context.Context, event collectorx.Event) (string, error) {
	f.events = append(f.events, event)
	return event.EventID, nil
}

// TestCollectorWriterEnqueueAdminLog 验证审计日志会被封装为稳定 bizType 的 Collector 事件。
func TestCollectorWriterEnqueueAdminLog(t *testing.T) {
	collector := &fakeCollector{}
	eventID := "admin_log:audit:1"
	row := model.AdminLog{
		UserID:    7,
		UserName:  "tester",
		Action:    string(model.ActionAdminLogQuery),
		Route:     "admin.log.query",
		CreatedAt: time.Now(),
	}
	if err := NewCollectorWriter(collector).EnqueueAdminLog(context.Background(), eventID, row); err != nil {
		t.Fatalf("EnqueueAdminLog() error = %v", err)
	}
	if len(collector.events) != 1 {
		t.Fatalf("期望投递 1 条事件，实际 %d", len(collector.events))
	}
	event := collector.events[0]
	if event.EventID != eventID || event.BizType != AdminLogCollectorBizType || event.PartitionKey != adminLogPartitionKey {
		t.Fatalf("collector event 不符合预期: %+v", event)
	}
	var payload model.AdminLog
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload 解析失败: %v", err)
	}
	if payload.UserID != row.UserID {
		t.Fatalf("payload 不符合预期: %+v", payload)
	}
}

// TestAdminLogBatchProcessorEmptyNoop 验证空批次不会触发调用方 DB 写入。
func TestAdminLogBatchProcessorEmptyNoop(t *testing.T) {
	results, err := (*AdminLogBatchProcessor)(nil).ProcessBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessBatch(empty) error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("ProcessBatch(empty) results = %+v, want empty", results)
	}
}

// TestAdminLogBatchProcessorWritesBatch 验证 Processor 会批量写入有效审计事件并返回逐条成功结果。
func TestAdminLogBatchProcessorWritesBatch(t *testing.T) {
	db := newDryRunMySQL(t)
	payload, _ := json.Marshal(model.AdminLog{
		UserID:    7,
		UserName:  "tester",
		Action:    string(model.ActionAdminLogQuery),
		Route:     "admin.log.query",
		CreatedAt: time.Now(),
	})
	results, err := NewAdminLogBatchProcessor(db).ProcessBatch(context.Background(), []collectorx.Event{
		{EventID: "admin_log:audit:ok", BizType: AdminLogCollectorBizType, Payload: payload},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(results) != 1 || !results[0].Success || results[0].EventID != "admin_log:audit:ok" {
		t.Fatalf("ProcessBatch() results = %+v", results)
	}
}

// TestAdminLogBatchProcessorRejectsBadPayload 验证坏消息只返回单条失败，不会伪装成功。
func TestAdminLogBatchProcessorRejectsBadPayload(t *testing.T) {
	db := newDryRunMySQL(t)
	results, err := NewAdminLogBatchProcessor(db).ProcessBatch(context.Background(), []collectorx.Event{
		{EventID: "bad", BizType: AdminLogCollectorBizType, Payload: []byte(`{`)},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(results) != 1 || results[0].Success || !strings.Contains(results[0].Error, "解析") {
		t.Fatalf("ProcessBatch() results = %+v", results)
	}
}

func newDryRunMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
