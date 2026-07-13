//go:build integration

package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// integrationMySQLDSNEnv 表示测试使用的常量。
const integrationMySQLDSNEnv = "INTEGRATION_MYSQL_DSN"

// TestAdminMigrationBlocksBootstrapOnMySQL 使用真实 MySQL 校验后台基线默认被拦截。
func TestAdminMigrationBlocksBootstrapOnMySQL(t *testing.T) {
	db := openIntegrationMySQL(t)
	resetIntegrationTables(t, db, "schema_migrations")

	results, err := RunMigrations(context.Background(), NewGormMigrationStore(db), DefaultMigrations(), MigrationRunOptions{})
	if err == nil {
		t.Fatal("期望后台 bootstrap-only/destructive 基线默认被拦截")
	}
	if len(results) == 0 || results[0].Status != MigrationStatusBlocked {
		t.Fatalf("期望第一条迁移为 blocked，实际: %+v", results)
	}
	if !strings.Contains(err.Error(), "安全策略") {
		t.Fatalf("期望错误说明安全策略拦截，实际: %v", err)
	}

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 0 {
		t.Fatalf("默认拦截后不应登记版本，实际 count=%d", count)
	}
}

// TestCollectorBaselineSchemaOnMySQL 使用真实 MySQL 校验 Collector 领取权和审计幂等基线结构。
func TestCollectorBaselineSchemaOnMySQL(t *testing.T) {
	db := openIntegrationMySQL(t)
	resetIntegrationTables(t, db, "schema_migrations", "admin_log", "collector_failed_event")
	t.Cleanup(func() { resetIntegrationTables(t, db, "schema_migrations", "admin_log", "collector_failed_event") })
	migrations := make([]Migration, 0, 2)
	for _, migration := range DefaultMigrations() {
		if migration.Asset == "admin_log.sql" || migration.Asset == "collector_failed_event.sql" {
			migrations = append(migrations, migration)
		}
	}
	results, err := RunMigrations(context.Background(), NewGormMigrationStore(db), migrations, MigrationRunOptions{AllowBootstrap: true})
	if err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if len(results) != 2 || results[0].Status != MigrationStatusExecuted || results[1].Status != MigrationStatusExecuted {
		t.Fatalf("Collector baseline migration results = %+v", results)
	}
	for table, columns := range map[string][]string{
		"admin_log":              {"event_id"},
		"collector_failed_event": {"claim_token", "lease_until"},
	} {
		for _, column := range columns {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("%s missing column %s", table, column)
			}
		}
	}
	if !db.Migrator().HasIndex("admin_log", "uk_event_id") {
		t.Fatal("admin_log missing uk_event_id")
	}
	if !db.Migrator().HasIndex("collector_failed_event", "idx_state_lease") {
		t.Fatal("collector_failed_event missing idx_state_lease")
	}
}

// TestAdminBaselineSchemaOnMySQL 使用真实 MySQL 校验管理员基线 DDL 可保存完整 IPv6 文本。
func TestAdminBaselineSchemaOnMySQL(t *testing.T) {
	db := openIntegrationMySQL(t)
	resetIntegrationTables(t, db, "schema_migrations", "admin")
	t.Cleanup(func() { resetIntegrationTables(t, db, "schema_migrations", "admin") })
	var migration Migration
	for _, item := range DefaultMigrations() {
		if item.Asset == "admin.sql" {
			migration = item
			break
		}
	}
	if migration.Asset == "" {
		t.Fatal("缺少管理员基线 DDL")
	}
	results, err := RunMigrations(context.Background(), NewGormMigrationStore(db), []Migration{migration}, MigrationRunOptions{AllowBootstrap: true})
	if err != nil {
		t.Fatalf("执行管理员基线 DDL 失败: %v", err)
	}
	if len(results) != 1 || results[0].Status != MigrationStatusExecuted {
		t.Fatalf("管理员基线 DDL 执行结果异常: %+v", results)
	}

	var maxLength int64
	if err = db.Raw("SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", "admin", "last_login_ip").Scan(&maxLength).Error; err != nil {
		t.Fatalf("查询管理员最后登录 IP 字段长度失败: %v", err)
	}
	if maxLength != 45 {
		t.Fatalf("管理员最后登录 IP 字段长度=%d, want 45", maxLength)
	}
	const ipv6 = "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255"
	if len(ipv6) != 45 {
		t.Fatalf("IPv6 边界测试值长度=%d, want 45", len(ipv6))
	}
	if err = db.Exec("INSERT INTO `admin` (`name`, `last_login_time`, `last_login_ip`) VALUES (?, NOW(), ?)", "ipv6-baseline-test", ipv6).Error; err != nil {
		t.Fatalf("管理员基线 DDL 无法写入完整 IPv6: %v", err)
	}
}

// openIntegrationMySQL 表示测试辅助逻辑。
func openIntegrationMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(integrationMySQLDSNEnv))
	if dsn == "" {
		t.Skipf("%s 未配置，跳过 MySQL 集成测试", integrationMySQLDSNEnv)
	}
	var lastErr error
	for i := 0; i < 30; i++ {
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err == nil {
			sqlDB, dbErr := db.DB()
			if dbErr == nil && sqlDB.Ping() == nil {
				t.Cleanup(func() { _ = sqlDB.Close() })
				return db
			}
			if dbErr != nil {
				lastErr = dbErr
			}
			if sqlDB != nil {
				_ = sqlDB.Close()
			}
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("连接集成 MySQL 失败: %v", lastErr)
	return nil
}

// resetIntegrationTables 表示测试辅助逻辑。
func resetIntegrationTables(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatalf("drop integration table %s: %v", table, err)
		}
	}
}
