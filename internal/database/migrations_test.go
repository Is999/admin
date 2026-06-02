package database

import (
	"regexp"
	"strings"
	"testing"
)

// TestValidateDefaultMigrations 确保默认迁移清单完整、版本递增且资产存在。
func TestValidateDefaultMigrations(t *testing.T) {
	if err := ValidateDefaultMigrations(); err != nil {
		t.Fatalf("ValidateDefaultMigrations() error = %v", err)
	}
}

// TestDefaultMigrationsCoverDatabaseSQLAssets 确保 database 下的 SQL 快照都纳入迁移治理。
func TestDefaultMigrationsCoverDatabaseSQLAssets(t *testing.T) {
	assets, err := MigrationAssetNames()
	if err != nil {
		t.Fatalf("MigrationAssetNames() error = %v", err)
	}
	covered := make(map[string]struct{}, len(DefaultMigrations()))
	for _, item := range DefaultMigrations() {
		covered[item.Asset] = struct{}{}
		if strings.HasPrefix(item.Name, "bootstrap_") && (!item.BootstrapOnly || item.Destructive) {
			t.Fatalf("admin baseline migration must be bootstrap-only and non-destructive: %+v", item)
		}
		if !strings.HasPrefix(item.Name, "bootstrap_") && item.BootstrapOnly {
			t.Fatalf("incremental migration should not be bootstrap-only: %+v", item)
		}
		if len(item.Checksum) != 64 {
			t.Fatalf("migration checksum length = %d, want 64: %+v", len(item.Checksum), item)
		}
		if strings.Contains(item.SQL, "Navicat Premium Dump SQL") || strings.Contains(item.SQL, "用户标签工作流骨架运行时表结构") {
			t.Fatalf("migration SQL header should be stripped: %s", item.Asset)
		}
		for _, forbidden := range []string{"DROP TABLE", "SET FOREIGN_KEY_CHECKS", "SET NAMES", "BEGIN;", "COMMIT;"} {
			if strings.Contains(strings.ToUpper(item.SQL), forbidden) {
				t.Fatalf("migration SQL contains dump-only statement %q: %s", forbidden, item.Asset)
			}
		}
	}
	for _, asset := range assets {
		if _, ok := covered[asset]; !ok {
			t.Fatalf("database SQL asset missing migration manifest: %s", asset)
		}
	}
}

// TestSchemaMigrationsSQL 确保迁移版本表 DDL 会剥离文件头。
func TestSchemaMigrationsSQL(t *testing.T) {
	sql := SchemaMigrationsSQL()
	if sql == "" {
		t.Fatal("SchemaMigrationsSQL() is empty")
	}
	if strings.Contains(sql, "代码资产") {
		t.Fatalf("SchemaMigrationsSQL() should strip header comments: %q", sql)
	}
	if !strings.Contains(sql, "schema_migrations") {
		t.Fatalf("SchemaMigrationsSQL() missing schema_migrations DDL: %q", sql)
	}
}

// TestDatabaseSQLAssetsCreateSingleTable 确保迁移 SQL 资产保持一张表一个文件。
func TestDatabaseSQLAssetsCreateSingleTable(t *testing.T) {
	re := regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\b`)
	for _, item := range DefaultMigrations() {
		got := len(re.FindAllString(item.SQL, -1))
		if got > 1 {
			t.Fatalf("migration asset %s CREATE TABLE count = %d, want at most 1", item.Asset, got)
		}
		if item.BootstrapOnly && got != 1 {
			t.Fatalf("bootstrap migration asset %s CREATE TABLE count = %d, want 1", item.Asset, got)
		}
	}
}

// TestUserTagRuntimeMigrationStartsSinglePhysicalShard 确保用户标签基线不会一次创建多张结果和快照表。
func TestUserTagRuntimeMigrationStartsSinglePhysicalShard(t *testing.T) {
	sql := migrationSQLByAssets(t,
		"user_tag_0.sql",
		"user_tag_0_tmp.sql",
		"user_tag_runtime_uid.sql",
		"user_tag_runtime_checkpoint.sql",
		"user_tag_event_outbox.sql",
	)
	for _, forbidden := range []string{
		"`user_tag_1`",
		"`user_tag_1_tmp`",
		"`user_tag_" + "sync_0`",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("user tag migration assets should not create upfront table %s", forbidden)
		}
	}
	if !strings.Contains(sql, "`user_tag_0`") || !strings.Contains(sql, "`user_tag_0_tmp`") {
		t.Fatalf("user tag migration assets missing base result table or tmp table")
	}
	for _, want := range []string{
		"`shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模1000分片'",
		"KEY `idx_shard_uid` (`shard_no`, `uid`)",
		"KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("user tag migration assets missing shard_no baseline DDL %q", want)
		}
	}
}

// TestAdminRolePermissionBaselineSkipsSuperRole 确保超级管理员不依赖角色权限关系种子数据。
func TestAdminRolePermissionBaselineSkipsSuperRole(t *testing.T) {
	sql := migrationSQLByAsset(t, "admin_role_permission_rel.sql")
	if strings.Contains(sql, "(1,") || strings.Contains(sql, "VALUES\n(1,") {
		t.Fatalf("admin_role_permission_rel.sql should not seed super role permissions")
	}
}

// TestRuntimeConfigBaselineSeedsDefaultRelease 确保运行配置默认发布数据不会从基线 SQL 丢失。
func TestRuntimeConfigBaselineSeedsDefaultRelease(t *testing.T) {
	sql := migrationSQLByAssets(t,
		"runtime_config_release.sql",
		"runtime_config_state.sql",
		"runtime_task_periodic.sql",
		"runtime_archive_job.sql",
	)
	for _, want := range []string{
		"INSERT IGNORE INTO `runtime_config_release`",
		"INSERT IGNORE INTO `runtime_config_state`",
		"INSERT IGNORE INTO `runtime_task_periodic`",
		"INSERT IGNORE INTO `runtime_archive_job`",
		"archive-admin-log-hourly",
		"user-tag-delta-daily",
		"baseline local runtime config seed",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("runtime config migration assets missing seed fragment %q", want)
		}
	}
	for _, forbidden := range []string{"admin_role_permission_rel", "(1, 139"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("runtime config migration assets should not seed super role permissions: %q", forbidden)
		}
	}
	re := regexp.MustCompile(`(?s)INSERT IGNORE INTO ` + "`runtime_config_release`" + `.*?VALUES \(\s*1, '1', 'dev', 1,\s*'(\{.*?\})',`)
	matches := re.FindStringSubmatch(sql)
	if len(matches) != 2 {
		t.Fatal("runtime config migration assets missing default release snapshot JSON")
	}
	if got := sha256Hex(matches[1]); got != "b1dbf79448b31e09700ba3765613936264a2c23254b1270b83a9c3e6c6c2342f" {
		t.Fatalf("default release snapshot checksum = %s", got)
	}
	if !strings.Contains(matches[1], `"enabled":false`) {
		t.Fatalf("default release snapshot should keep user tag periodic tasks disabled: %s", matches[1])
	}
}

// TestRuntimeArchiveJobRepairMigration 确保本地运行配置脏草稿修复 SQL 覆盖已知错误形态。
func TestRuntimeArchiveJobRepairMigration(t *testing.T) {
	sql := migrationSQLByAsset(t, "runtime_archive_job_repair.sql")
	for _, want := range []string{
		"UPDATE `runtime_archive_job` AS bad",
		"archive-admin-log-hourly",
		"bad.`name` = 'admin_log'",
		"bad.`table_name` = 'admin_log'",
		"CONCAT(bad.`app_id`, ':legacy')",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("runtime archive job repair migration missing %q", want)
		}
	}
}

// TestPendingMigrations 确保已登记版本不会再次进入待执行列表。
func TestPendingMigrations(t *testing.T) {
	migrations := DefaultMigrations()
	if len(migrations) < 2 {
		t.Fatal("default migrations too small")
	}
	pending := PendingMigrations(map[string]struct{}{migrations[0].Version: {}})
	if len(pending) != len(migrations)-1 {
		t.Fatalf("PendingMigrations() len = %d, want %d", len(pending), len(migrations)-1)
	}
	if pending[0].Version == migrations[0].Version {
		t.Fatalf("applied migration still pending: %+v", pending[0])
	}
}

// migrationSQLByAssets 返回多个迁移资产拼接后的 SQL。
func migrationSQLByAssets(t *testing.T, assets ...string) string {
	t.Helper()
	parts := make([]string, 0, len(assets))
	for _, asset := range assets {
		parts = append(parts, migrationSQLByAsset(t, asset))
	}
	return strings.Join(parts, "\n")
}

// migrationSQLByAsset 返回指定迁移资产的 SQL。
func migrationSQLByAsset(t *testing.T, asset string) string {
	t.Helper()
	for _, item := range DefaultMigrations() {
		if item.Asset == asset {
			return item.SQL
		}
	}
	t.Fatalf("migration asset not found: %s", asset)
	return ""
}
