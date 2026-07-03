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
		"`shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模1024分片'",
		"KEY `idx_shard_uid` (`shard_no`, `uid`)",
		"KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("user tag migration assets missing shard_no baseline DDL %q", want)
		}
	}
}

// TestBusinessUIDMigrationAssetsCarryShardNo 确保新增业务 UID 表不会漏掉分片字段。
func TestBusinessUIDMigrationAssetsCarryShardNo(t *testing.T) {
	shardIndexRe := regexp.MustCompile("(?i)KEY\\s+`[^`]*shard[^`]*`\\s*\\([^)]*`shard_no`")
	for _, item := range DefaultMigrations() {
		if !strings.Contains(item.SQL, "`uid`") {
			continue
		}
		if !strings.Contains(item.SQL, "`shard_no`") {
			t.Fatalf("migration asset with business uid must carry shard_no: %s", item.Asset)
		}
		if !shardIndexRe.MatchString(item.SQL) {
			t.Fatalf("migration asset with business uid must keep shard_no index: %s", item.Asset)
		}
	}
}

// TestCollectorOutboxBaselineIndexes 确保收集器概览索引内置在基线 DDL。
func TestCollectorOutboxBaselineIndexes(t *testing.T) {
	sql := migrationSQLByAsset(t, "collector_outbox.sql")
	for _, want := range []string{
		"KEY `idx_state_finished` (`state`,`finished_at`)",
		"KEY `idx_state_updated` (`state`,`updated_at`)",
		"KEY `idx_transport_state` (`transport`,`state`)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("collector_outbox baseline missing overview index %q", want)
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

// TestRuntimeConfigBaselineSeedsDraftRows 确保运行配置基线只逐条种草稿明细，不写默认发布快照。
func TestRuntimeConfigBaselineSeedsDraftRows(t *testing.T) {
	sql := migrationSQLByAssets(t,
		"runtime_config_release.sql",
		"runtime_config_state.sql",
		"runtime_task_periodic.sql",
		"runtime_archive_job.sql",
	)
	for _, want := range []string{
		"INSERT IGNORE INTO `runtime_config_state`",
		"INSERT IGNORE INTO `runtime_task_periodic`",
		"INSERT IGNORE INTO `runtime_archive_job`",
		"archive-admin-log-hourly",
		"task-report-daily-summary",
		"task_report.daily_summary",
		"user-tag-delta-daily",
		"active_release_id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("runtime config migration assets missing seed fragment %q", want)
		}
	}
	releaseSQL := migrationSQLByAsset(t, "runtime_config_release.sql")
	if strings.Contains(releaseSQL, "INSERT IGNORE INTO `runtime_config_release`") || strings.Contains(releaseSQL, "snapshot_json") && strings.Contains(releaseSQL, "baseline local runtime config seed") {
		t.Fatal("runtime_config_release.sql should not seed default snapshot rows")
	}
	periodicSQL := migrationSQLByAsset(t, "runtime_task_periodic.sql")
	if got := strings.Count(periodicSQL, "INSERT IGNORE INTO `runtime_task_periodic`"); got != 5 {
		t.Fatalf("runtime_task_periodic seed count = %d, want 5", got)
	}
	archiveSQL := migrationSQLByAsset(t, "runtime_archive_job.sql")
	if got := strings.Count(archiveSQL, "INSERT IGNORE INTO `runtime_archive_job`"); got != 1 {
		t.Fatalf("runtime_archive_job seed count = %d, want 1", got)
	}
	for table, seedSQL := range map[string]string{
		"runtime_task_periodic": periodicSQL,
		"runtime_archive_job":   archiveSQL,
	} {
		if strings.Contains(seedSQL, "INSERT IGNORE INTO `"+table+"` (`id`,") {
			t.Fatalf("%s seed insert should not pin auto-increment id", table)
		}
	}
	stateSQL := migrationSQLByAsset(t, "runtime_config_state.sql")
	if !strings.Contains(stateSQL, "VALUES (1, 0, 0, ''") {
		t.Fatalf("runtime_config_state should start without active release: %s", stateSQL)
	}
	sysConfigSQL := migrationSQLByAsset(t, "sys_config.sql")
	if !strings.Contains(sysConfigSQL, "INSERT IGNORE INTO `sys_config` (`id`,") {
		t.Fatal("sys_config seed insert should keep fixed ids because pid/pids reference the hierarchy")
	}
	for _, forbidden := range []string{"admin_role_permission_rel", "(1, 139", "baseline local runtime config seed"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("runtime config migration assets should not seed super role permissions: %q", forbidden)
		}
	}
	for _, forbidden := range []string{"`app_id`", "`env`", "'1', 'dev'"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("runtime config migration assets should not keep DB scope fragment %q", forbidden)
		}
	}
}

// TestDocumentPermissionMigration 确保文档权限增量收口在单一 SQL 资产中。
func TestDocumentPermissionMigration(t *testing.T) {
	sql := migrationSQLByAsset(t, "document_permission_seed.sql")
	for _, want := range []string{
		"INSERT IGNORE INTO `admin_permission`",
		"'docs.file.文档首页.md'",
		"'docs.file.角色文档/后端开发/AI开发提示词.md'",
		"'docs.file.接口文档/后台系统/权限管理接口.md'",
		"'docs.file.api/接口文档/前台系统/系统接口.md'",
		"'docs.file.api/角色文档/后端开发/AI开发规范.md'",
		"'65,164,165'",
		"'65,164'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("document permission migration missing %q", want)
		}
	}
	if got := strings.Count(sql, "INSERT IGNORE INTO `admin_permission`"); got != 64 {
		t.Fatalf("document permission seed count = %d, want 64", got)
	}
	for _, forbidden := range []string{"admin_role_permission_rel", " SELECT ", " JOIN "} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("document permission migration should only seed permission rows, found %q", forbidden)
		}
	}
}

// TestMigrationAssetsDMLStyle 确保迁移 DML 不使用 UPDATE，且每条 DML SQL 保持单行。
func TestMigrationAssetsDMLStyle(t *testing.T) {
	dmlStartRe := regexp.MustCompile(`(?i)^(INSERT|UPDATE|DELETE|SELECT)\b`)
	for _, item := range DefaultMigrations() {
		for lineNo, line := range strings.Split(item.SQL, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if strings.HasPrefix(strings.ToUpper(trimmed), "UPDATE ") {
				t.Fatalf("%s contains UPDATE at line %d", item.Asset, lineNo+1)
			}
			if dmlStartRe.MatchString(trimmed) && !strings.HasSuffix(trimmed, ";") {
				t.Fatalf("%s DML statement should be one line at line %d: %s", item.Asset, lineNo+1, trimmed)
			}
		}
	}
}

// TestPermissionMigrationAssetsConsolidated 确保权限增量不再散落到多个 SQL 文件。
func TestPermissionMigrationAssetsConsolidated(t *testing.T) {
	documentPermissionAssets := 0
	for _, item := range DefaultMigrations() {
		if strings.HasPrefix(item.Asset, "document_permission") {
			documentPermissionAssets++
			if item.Asset != "document_permission_seed.sql" || item.Name != "sync_document_permissions" {
				t.Fatalf("document permission migration must use consolidated asset: %+v", item)
			}
		}
		if strings.Contains(item.Asset, "drop_scope") {
			t.Fatalf("runtime config scope DDL should not be a standalone migration asset: %s", item.Asset)
		}
	}
	if documentPermissionAssets != 1 {
		t.Fatalf("document permission migration asset count = %d, want 1", documentPermissionAssets)
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
