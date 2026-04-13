package database

import (
	"context"
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// MigrationStatusApplied 表示迁移版本已登记且 checksum 匹配。
	MigrationStatusApplied = "applied"
	// MigrationStatusPending 表示迁移待执行。
	MigrationStatusPending = "pending"
	// MigrationStatusExecuted 表示本轮已执行并登记。
	MigrationStatusExecuted = "executed"
	// MigrationStatusBlocked 表示迁移被安全策略拦截。
	MigrationStatusBlocked = "blocked"
)

// AppliedMigration 表示 schema_migrations 中已登记的版本。
type AppliedMigration struct {
	Version  string // 迁移版本号
	Name     string // 迁移名称
	Asset    string // 迁移资产文件名
	Checksum string // 已登记 SQL checksum
}

// MigrationRunOptions 控制迁移执行策略。
type MigrationRunOptions struct {
	DryRun           bool // 是否只输出计划，不执行 SQL
	AllowBootstrap   bool // 是否允许执行 bootstrap-only 基线迁移
	AllowDestructive bool // 是否允许执行 destructive 迁移
}

// MigrationRunItem 表示单个迁移在本轮计划中的状态。
type MigrationRunItem struct {
	Version  string // 迁移版本号
	Name     string // 迁移名称
	Asset    string // 迁移资产
	Checksum string // 当前 SQL checksum
	Status   string // applied/pending/executed/blocked
	Reason   string // 状态原因，blocked 时必填
}

// MigrationStore 抽象迁移执行所需的数据库操作，便于命令行和测试复用。
type MigrationStore interface {
	EnsureSchema(context.Context, string) error
	AppliedMigrations(context.Context) (map[string]AppliedMigration, error)
	ExecuteMigration(context.Context, Migration) error
}

// RunMigrations 根据 schema_migrations 状态执行或预览迁移。
func RunMigrations(ctx context.Context, store MigrationStore, migrations []Migration, options MigrationRunOptions) ([]MigrationRunItem, error) {
	if store == nil {
		return nil, errors.Errorf("数据库迁移 store 不能为空")
	}
	if err := validateMigrationList(migrations); err != nil {
		return nil, errors.Tag(err)
	}
	if !options.DryRun {
		if err := store.EnsureSchema(ctx, SchemaMigrationsSQL()); err != nil {
			return nil, errors.Wrap(err, "初始化数据库迁移版本表失败")
		}
	}
	applied, err := store.AppliedMigrations(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "读取数据库迁移版本表失败")
	}

	results := make([]MigrationRunItem, 0, len(migrations))
	for _, migration := range migrations {
		item := newMigrationRunItem(migration)
		if appliedItem, ok := applied[migration.Version]; ok {
			if !sameChecksum(appliedItem.Checksum, migration.Checksum) {
				return results, errors.Errorf("数据库迁移 checksum 不一致 version=%s name=%s applied=%s current=%s", migration.Version, migration.Name, appliedItem.Checksum, migration.Checksum)
			}
			item.Status = MigrationStatusApplied
			results = append(results, item)
			continue
		}

		if reason := blockMigrationReason(migration, options); reason != "" {
			item.Status = MigrationStatusBlocked
			item.Reason = reason
			results = append(results, item)
			if !options.DryRun {
				return results, errors.Errorf("数据库迁移被安全策略拦截 version=%s name=%s reason=%s", migration.Version, migration.Name, reason)
			}
			continue
		}
		if options.DryRun {
			item.Status = MigrationStatusPending
			results = append(results, item)
			continue
		}
		if err := store.ExecuteMigration(ctx, migration); err != nil {
			return results, errors.Tag(err)
		}
		item.Status = MigrationStatusExecuted
		results = append(results, item)
	}
	return results, nil
}

func newMigrationRunItem(migration Migration) MigrationRunItem {
	return MigrationRunItem{
		Version:  migration.Version,
		Name:     migration.Name,
		Asset:    migration.Asset,
		Checksum: migration.Checksum,
	}
}

func blockMigrationReason(migration Migration, options MigrationRunOptions) string {
	if migration.BootstrapOnly && !options.AllowBootstrap {
		return "bootstrap-only 迁移需要显式允许"
	}
	if migration.Destructive && !options.AllowDestructive {
		return "destructive 迁移需要显式允许"
	}
	return ""
}

func validateMigrationList(migrations []Migration) error {
	if len(migrations) == 0 {
		return errors.Errorf("数据库迁移清单不能为空")
	}
	seenVersions := make(map[string]struct{}, len(migrations))
	seenNames := make(map[string]struct{}, len(migrations))
	previousVersion := ""
	for _, item := range migrations {
		if strings.TrimSpace(item.Version) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Asset) == "" || strings.TrimSpace(item.SQL) == "" || strings.TrimSpace(item.Checksum) == "" {
			return errors.Errorf("数据库迁移清单存在空字段: %+v", item)
		}
		if _, ok := seenVersions[item.Version]; ok {
			return errors.Errorf("数据库迁移版本重复: %s", item.Version)
		}
		if _, ok := seenNames[item.Name]; ok {
			return errors.Errorf("数据库迁移名称重复: %s", item.Name)
		}
		if previousVersion != "" && item.Version <= previousVersion {
			return errors.Errorf("数据库迁移版本必须递增: %s <= %s", item.Version, previousVersion)
		}
		if item.BootstrapOnly && !item.Destructive {
			return errors.Errorf("bootstrap-only 迁移必须同时标记 destructive: %s", item.Name)
		}
		if containsDestructiveSQL(item.SQL) && !item.Destructive {
			return errors.Errorf("检测到破坏性 SQL 但迁移未标记 destructive: %s", item.Name)
		}
		seenVersions[item.Version] = struct{}{}
		seenNames[item.Name] = struct{}{}
		previousVersion = item.Version
	}
	return nil
}

func sameChecksum(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func containsDestructiveSQL(sqlText string) bool {
	for _, statement := range splitMigrationStatements(sqlText) {
		normalized := normalizeMigrationStatement(statement)
		if normalized == "" {
			continue
		}
		for _, marker := range destructiveMigrationSQLMarkers {
			if strings.Contains(normalized, marker) {
				return true
			}
		}
		if strings.HasPrefix(normalized, "ALTER TABLE ") && strings.Contains(normalized, " DROP ") {
			return true
		}
	}
	return false
}

func normalizeMigrationStatement(statement string) string {
	statement = trimLeadingSQLComments(statement)
	return strings.ToUpper(strings.Join(strings.Fields(statement), " "))
}

var destructiveMigrationSQLMarkers = []string{
	"DROP TABLE",
	"DROP DATABASE",
	"TRUNCATE TABLE",
	"DELETE FROM",
}
