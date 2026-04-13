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

func resetIntegrationTables(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatalf("drop integration table %s: %v", table, err)
		}
	}
}
