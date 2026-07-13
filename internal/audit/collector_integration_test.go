//go:build integration

package audit

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"admin/internal/infra/collectorx"
	"admin/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestAdminLogReplayAfterRedisFailureIsIdempotent 使用真实 MySQL 验证 DB 成功后重放不会重复插入。
func TestAdminLogReplayAfterRedisFailureIsIdempotent(t *testing.T) {
	db := openAuditIntegrationMySQL(t)
	if err := db.Migrator().DropTable(&model.AdminLog{}); err != nil {
		t.Fatalf("drop admin_log: %v", err)
	}
	if err := db.AutoMigrate(&model.AdminLog{}); err != nil {
		t.Fatalf("migrate admin_log: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.AdminLog{}) })

	payload, _ := json.Marshal(model.AdminLog{ID: 999, UserID: 7, UserName: "tester", CreatedAt: time.Now()})
	event := collectorx.Event{EventID: "admin_log:audit:replay", BizType: AdminLogCollectorBizType, Payload: payload}
	processor := NewAdminLogBatchProcessor(db)
	for attempt := 0; attempt < 2; attempt++ {
		results, err := processor.ProcessBatch(context.Background(), []collectorx.Event{event, event})
		if err != nil {
			t.Fatalf("ProcessBatch(attempt=%d) error = %v", attempt, err)
		}
		if len(results) != 1 || !results[0].Success {
			t.Fatalf("ProcessBatch(attempt=%d) results = %+v", attempt, results)
		}
	}
	var count int64
	if err := db.Model(&model.AdminLog{}).Where("event_id = ?", event.EventID).Count(&count).Error; err != nil {
		t.Fatalf("count replayed admin_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("DB 成功后重放产生重复记录 count=%d", count)
	}
	var row model.AdminLog
	if err := db.Where("event_id = ?", event.EventID).First(&row).Error; err != nil {
		t.Fatalf("load replayed admin_log: %v", err)
	}
	if row.ID == 999 {
		t.Fatal("Collector payload 主键不应写入目标 admin_log")
	}
}

// openAuditIntegrationMySQL 连接测试 MySQL。
func openAuditIntegrationMySQL(t *testing.T) *gorm.DB {
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
	return db
}
