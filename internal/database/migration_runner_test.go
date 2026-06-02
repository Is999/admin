package database

import (
	"context"
	"testing"
)

// TestRunMigrationsExecutesPending 确保待执行迁移会按顺序执行并登记。
func TestRunMigrationsExecutesPending(t *testing.T) {
	store := newFakeMigrationStore(nil)
	migrations := []Migration{testMigration("202606050001", "create_demo")}

	results, err := RunMigrations(context.Background(), store, migrations, MigrationRunOptions{})
	if err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if !store.schemaEnsured {
		t.Fatal("期望执行前初始化 schema_migrations")
	}
	if len(store.executed) != 1 || store.executed[0].Version != migrations[0].Version {
		t.Fatalf("执行迁移不符合预期: %+v", store.executed)
	}
	if len(results) != 1 || results[0].Status != MigrationStatusExecuted {
		t.Fatalf("迁移结果不符合预期: %+v", results)
	}
}

// TestRunMigrationsRejectsBlockedMigration 确保危险迁移默认不会在线执行。
func TestRunMigrationsRejectsBlockedMigration(t *testing.T) {
	store := newFakeMigrationStore(nil)
	migrations := []Migration{testMigration("202606050001", "bootstrap_demo")}
	migrations[0].BootstrapOnly = true
	migrations[0].Destructive = true

	results, err := RunMigrations(context.Background(), store, migrations, MigrationRunOptions{})
	if err == nil {
		t.Fatal("期望危险迁移返回错误，实际为 nil")
	}
	if len(store.executed) != 0 {
		t.Fatalf("危险迁移不应被执行: %+v", store.executed)
	}
	if len(results) != 1 || results[0].Status != MigrationStatusBlocked {
		t.Fatalf("迁移结果应为 blocked: %+v", results)
	}
}

// TestRunMigrationsDryRunReportsBlockedMigration 确保 dry-run 只报告拦截原因，不执行 SQL。
func TestRunMigrationsDryRunReportsBlockedMigration(t *testing.T) {
	store := newFakeMigrationStore(nil)
	migrations := []Migration{testMigration("202606050001", "bootstrap_demo")}
	migrations[0].BootstrapOnly = true
	migrations[0].Destructive = true

	results, err := RunMigrations(context.Background(), store, migrations, MigrationRunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("RunMigrations(dry-run) error = %v", err)
	}
	if store.schemaEnsured {
		t.Fatal("dry-run 不应创建 schema_migrations")
	}
	if len(results) != 1 || results[0].Status != MigrationStatusBlocked {
		t.Fatalf("dry-run 结果应为 blocked: %+v", results)
	}
}

// TestRunMigrationsDetectsChecksumMismatch 确保历史版本 SQL 被改动时会被拒绝。
func TestRunMigrationsDetectsChecksumMismatch(t *testing.T) {
	migration := testMigration("202606050001", "create_demo")
	store := newFakeMigrationStore(map[string]AppliedMigration{
		migration.Version: {Version: migration.Version, Name: migration.Name, Checksum: "changed"},
	})

	if _, err := RunMigrations(context.Background(), store, []Migration{migration}, MigrationRunOptions{DryRun: true, StrictChecksum: true}); err == nil {
		t.Fatal("期望 checksum 不一致返回错误，实际为 nil")
	}
}

// TestRunMigrationsIgnoresChecksumMismatchByDefault 确保未发布迁移漂移默认不阻断增量迁移。
func TestRunMigrationsIgnoresChecksumMismatchByDefault(t *testing.T) {
	migration := testMigration("202606050001", "create_demo")
	store := newFakeMigrationStore(map[string]AppliedMigration{
		migration.Version: {Version: migration.Version, Name: migration.Name, Checksum: "changed"},
	})

	results, err := RunMigrations(context.Background(), store, []Migration{migration}, MigrationRunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if len(results) != 1 || results[0].Status != MigrationStatusApplied || results[0].Reason == "" {
		t.Fatalf("checksum drift should be applied with reason: %+v", results)
	}
}

// TestRunMigrationsRejectsUnmarkedDestructiveSQL 确保破坏性 SQL 必须显式标记。
func TestRunMigrationsRejectsUnmarkedDestructiveSQL(t *testing.T) {
	migration := testMigration("202606050001", "drop_demo")
	migration.SQL = "DROP TABLE demo"
	migration.Checksum = sha256Hex(migration.SQL)

	if _, err := RunMigrations(context.Background(), newFakeMigrationStore(nil), []Migration{migration}, MigrationRunOptions{DryRun: true}); err == nil {
		t.Fatal("期望未标记 destructive 的 DROP SQL 返回错误，实际为 nil")
	}
}

// TestDefaultAdminBaselineMigrationsBlockedByDefault 确保后台基线默认 blocked，非破坏性增量迁移可进入 pending。
func TestDefaultAdminBaselineMigrationsBlockedByDefault(t *testing.T) {
	results, err := RunMigrations(context.Background(), newFakeMigrationStore(nil), DefaultMigrations(), MigrationRunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("RunMigrations(dry-run default) error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("期望默认迁移结果非空")
	}
	migrations := map[string]Migration{}
	for _, migration := range DefaultMigrations() {
		migrations[migration.Version] = migration
	}
	for _, item := range results {
		migration, ok := migrations[item.Version]
		if !ok {
			t.Fatalf("迁移结果版本未出现在默认清单: %+v", item)
		}
		if migration.BootstrapOnly || migration.Destructive {
			if item.Status != MigrationStatusBlocked {
				t.Fatalf("后台基线迁移默认应被拦截: %+v", item)
			}
			continue
		}
		if item.Status != MigrationStatusPending {
			t.Fatalf("非破坏性增量迁移应进入 pending: %+v", item)
		}
	}
}

func testMigration(version string, name string) Migration {
	return Migration{
		Version:  version,
		Name:     name,
		Asset:    name + ".sql.tmpl",
		SQL:      "CREATE TABLE demo (id int)",
		Checksum: sha256Hex("CREATE TABLE demo (id int)"),
	}
}

type fakeMigrationStore struct {
	applied       map[string]AppliedMigration
	schemaEnsured bool
	executed      []Migration
}

func newFakeMigrationStore(applied map[string]AppliedMigration) *fakeMigrationStore {
	if applied == nil {
		applied = map[string]AppliedMigration{}
	}
	return &fakeMigrationStore{applied: applied}
}

func (s *fakeMigrationStore) EnsureSchema(context.Context, string) error {
	s.schemaEnsured = true
	return nil
}

func (s *fakeMigrationStore) AppliedMigrations(context.Context) (map[string]AppliedMigration, error) {
	return s.applied, nil
}

func (s *fakeMigrationStore) ExecuteMigration(_ context.Context, migration Migration) error {
	s.executed = append(s.executed, migration)
	s.applied[migration.Version] = AppliedMigration{
		Version:  migration.Version,
		Name:     migration.Name,
		Asset:    migration.Asset,
		Checksum: migration.Checksum,
	}
	return nil
}
