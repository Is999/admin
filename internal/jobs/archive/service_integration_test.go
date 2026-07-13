//go:build integration

package archive

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/svc"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestDeleteLeaseAttemptFencesExpiredOwnerOnMySQL 使用真实 MySQL 验证租约过期被接管后旧领取轮次无法续租或写入终态。
func TestDeleteLeaseAttemptFencesExpiredOwnerOnMySQL(t *testing.T) {
	db := openArchiveIntegrationMySQL(t)
	if err := db.Migrator().DropTable(&Segment{}); err != nil {
		t.Fatalf("drop archive_segment: %v", err)
	}
	if err := db.AutoMigrate(&Segment{}); err != nil {
		t.Fatalf("migrate archive_segment: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&Segment{}) })

	now := time.Now().Truncate(time.Microsecond)
	row := Segment{
		JobName:          "integration-delete-fence",
		SourceTableName:  "integration_hot",
		HistoryTableName: "integration_history",
		RangeStart:       now.Add(-2 * time.Hour),
		RangeEnd:         now.Add(-time.Hour),
		Status:           statusDone,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create archive_segment: %v", err)
	}
	service := NewService(svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{SafeDelayMinutes: 1, LeaseTTLSeconds: 30},
	}, svc.Dependencies{}))
	job := jobConfig{Name: row.JobName}
	first, err := service.claimNextDeleteSegment(context.Background(), job, db, "same-worker")
	if err != nil || first == nil || first.AttemptCount != 1 {
		t.Fatalf("first claim = %+v err=%v", first, err)
	}
	if err = db.Model(&Segment{}).Where("id = ?", row.ID).
		Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second, err := service.claimNextDeleteSegment(context.Background(), job, db, "same-worker")
	if err != nil || second == nil || second.AttemptCount != 2 {
		t.Fatalf("second claim = %+v err=%v", second, err)
	}

	if err = service.renewSegmentDeleteLease(context.Background(), db, first, "same-worker"); err == nil {
		t.Fatal("旧领取轮次续租应被 attempt_count fencing 拒绝")
	}
	if err = service.markSegmentDeleted(context.Background(), db, first, "same-worker"); err == nil {
		t.Fatal("旧领取轮次终态写入应被 attempt_count fencing 拒绝")
	}
	if err = service.renewSegmentDeleteLease(context.Background(), db, second, "same-worker"); err != nil {
		t.Fatalf("当前领取轮次续租失败: %v", err)
	}
	if err = service.markSegmentDeleted(context.Background(), db, second, "same-worker"); err != nil {
		t.Fatalf("当前领取轮次写入终态失败: %v", err)
	}
	var stored Segment
	if err = db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("load archive_segment: %v", err)
	}
	if stored.Status != statusDeleted || stored.AttemptCount != 2 || stored.WorkerID != "" || stored.LeaseExpiresAt.Valid {
		t.Fatalf("删除终态不符合预期: %+v", stored)
	}
}

// TestArchiveLeaseAttemptFencesExpiredOwnerOnMySQL 使用真实 MySQL 验证复制租约被接管后旧轮次无法推进 checkpoint 或写入终态。
func TestArchiveLeaseAttemptFencesExpiredOwnerOnMySQL(t *testing.T) {
	db := openArchiveIntegrationMySQL(t)
	if err := db.Migrator().DropTable(&Segment{}); err != nil {
		t.Fatalf("drop archive_segment: %v", err)
	}
	if err := db.AutoMigrate(&Segment{}); err != nil {
		t.Fatalf("migrate archive_segment: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&Segment{}) })

	now := time.Now().Truncate(time.Microsecond)
	row := Segment{
		JobName:          "integration-archive-fence",
		SourceTableName:  "integration_hot",
		HistoryTableName: "integration_history",
		RangeStart:       now.Add(-2 * time.Hour),
		RangeEnd:         now.Add(-time.Hour),
		Status:           statusPending,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create archive_segment: %v", err)
	}
	service := NewService(svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{SafeDelayMinutes: 1, LeaseTTLSeconds: 30},
	}, svc.Dependencies{}))
	job := jobConfig{Name: row.JobName}
	first, err := service.claimNextSegment(context.Background(), job, db, "same-worker")
	if err != nil || first == nil || first.AttemptCount != 1 {
		t.Fatalf("first claim = %+v err=%v", first, err)
	}
	if err = db.Model(&Segment{}).Where("id = ?", row.ID).
		Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second, err := service.claimNextSegment(context.Background(), job, db, "same-worker")
	if err != nil || second == nil || second.AttemptCount != 2 {
		t.Fatalf("second claim = %+v err=%v", second, err)
	}

	if err = service.updateSegmentCheckpoint(context.Background(), db, first, 1, now.Add(-90*time.Minute), 1, "same-worker"); err == nil {
		t.Fatal("旧领取轮次 checkpoint 应被 attempt_count fencing 拒绝")
	}
	if err = service.markSegmentDone(context.Background(), db, first, "same-worker"); err == nil {
		t.Fatal("旧领取轮次完成终态应被 attempt_count fencing 拒绝")
	}
	if err = service.markSegmentFailed(context.Background(), db, first, "same-worker", archiveLeaseTestError{}); err == nil {
		t.Fatal("旧领取轮次失败终态应被 attempt_count fencing 拒绝")
	}
	if err = service.updateSegmentCheckpoint(context.Background(), db, second, 1, now.Add(-90*time.Minute), 1, "same-worker"); err != nil {
		t.Fatalf("当前领取轮次 checkpoint 更新失败: %v", err)
	}
	if err = service.markSegmentDone(context.Background(), db, second, "same-worker"); err != nil {
		t.Fatalf("当前领取轮次完成终态失败: %v", err)
	}
	var stored Segment
	if err = db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("load archive_segment: %v", err)
	}
	if stored.Status != statusDone || stored.AttemptCount != 2 || stored.RowsArchived != 1 || stored.WorkerID != "" || stored.LeaseExpiresAt.Valid {
		t.Fatalf("归档终态不符合预期: %+v", stored)
	}
}

// openArchiveIntegrationMySQL 连接归档故障注入测试使用的 MySQL。
func openArchiveIntegrationMySQL(t *testing.T) *gorm.DB {
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
