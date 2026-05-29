package database

import (
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
		if item.Version < "202606050017" && (!item.BootstrapOnly || !item.Destructive) {
			t.Fatalf("admin baseline migration must be bootstrap-only and destructive: %+v", item)
		}
		if item.Version >= "202606050017" && (item.BootstrapOnly || item.Destructive) {
			t.Fatalf("online migration must be non-destructive: %+v", item)
		}
		if len(item.Checksum) != 64 {
			t.Fatalf("migration checksum length = %d, want 64: %+v", len(item.Checksum), item)
		}
		if strings.Contains(item.SQL, "Navicat Premium Dump SQL") || strings.Contains(item.SQL, "用户标签工作流骨架运行时表结构") {
			t.Fatalf("migration SQL header should be stripped: %s", item.Asset)
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

// TestSchemaMigrationsExistsSQL 确保版本表存在性检查 SQL 来自资产文件。
func TestSchemaMigrationsExistsSQL(t *testing.T) {
	sql := SchemaMigrationsExistsSQL()
	if sql == "" {
		t.Fatal("SchemaMigrationsExistsSQL() is empty")
	}
	if strings.Contains(sql, "用途：") {
		t.Fatalf("SchemaMigrationsExistsSQL() should strip header comments: %q", sql)
	}
	for _, want := range []string{"information_schema.tables", "DATABASE()", "table_name"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SchemaMigrationsExistsSQL() missing %q: %q", want, sql)
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
