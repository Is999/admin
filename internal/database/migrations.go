package database

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"

	"admin/common/embedasset"

	"github.com/Is999/go-utils/errors"
)

// migrationAssets 嵌入内置数据库迁移 SQL 资产。
//
//go:embed assets/*.sql assets/*.sql.tmpl
var migrationAssets embed.FS

// migrationAssetDir 表示内置迁移 SQL 资产所在目录。
const migrationAssetDir = "assets"

// Migration 描述一个数据库迁移资产。
type Migration struct {
	Version       string // 迁移版本号，必须单调递增
	Name          string // 迁移名称，必须唯一
	Asset         string // SQL 资产文件名
	SQL           string // 剥离说明后的 SQL 文本
	Checksum      string // SQL 文本 SHA256
	BootstrapOnly bool   // 是否仅允许新库初始化时人工执行
	Destructive   bool   // 是否包含 DROP/种子数据等不适合在线执行的语句
}

// migrationSpec 描述内置迁移资产元数据。
type migrationSpec struct {
	version       string // 迁移版本号
	name          string // 迁移名称
	asset         string // SQL 文件名
	bootstrapOnly bool   // 是否仅用于新库初始化
	destructive   bool   // 是否含破坏性或种子数据语句
}

// defaultMigrationSpecs 定义内置迁移清单，顺序即执行顺序。
var defaultMigrationSpecs = []migrationSpec{
	{version: "202606050001", name: "bootstrap_admin_table", asset: "admin.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050002", name: "bootstrap_ops", asset: "bootstrap.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050003", name: "bootstrap_admin_log", asset: "admin_log.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050004", name: "bootstrap_admin_message", asset: "admin_message.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050005", name: "bootstrap_admin_message_receiver", asset: "admin_message_receiver.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050006", name: "bootstrap_admin_permission", asset: "admin_permission.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050007", name: "bootstrap_admin_role", asset: "admin_role.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050008", name: "bootstrap_admin_role_permission_rel", asset: "admin_role_permission_rel.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050009", name: "bootstrap_admin_role_rel", asset: "admin_role_rel.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050010", name: "bootstrap_archive_segment", asset: "archive_segment.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050011", name: "bootstrap_archive_watermark", asset: "archive_watermark.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050012", name: "bootstrap_collector_outbox", asset: "collector_outbox.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050013", name: "bootstrap_secret_key", asset: "secret_key.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050014", name: "bootstrap_secret_key_version", asset: "secret_key_version.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050015", name: "bootstrap_sys_config", asset: "sys_config.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050016", name: "bootstrap_user_tag_runtime", asset: "user_tag_runtime.sql", bootstrapOnly: true, destructive: true},
	{version: "202606050017", name: "runtime_config", asset: "runtime_config.sql", bootstrapOnly: false, destructive: false},
	{version: "202606050018", name: "api_user_manage", asset: "api_user_manage.sql", bootstrapOnly: false, destructive: false},
	{version: "202606050019", name: "docs_permissions", asset: "docs_permissions.sql", bootstrapOnly: false, destructive: false},
}

// SchemaMigrationsSQL 返回剥离文件头说明后的迁移版本表 DDL。
func SchemaMigrationsSQL() string {
	data, err := migrationAssets.ReadFile(migrationAssetPath("schema_migrations.sql.tmpl"))
	if err != nil {
		return ""
	}
	return embedasset.StripLeadingLineComments(string(data), "--")
}

// SchemaMigrationsExistsSQL 返回版本表存在性检查 SQL。
func SchemaMigrationsExistsSQL() string {
	data, err := migrationAssets.ReadFile(migrationAssetPath("schema_migrations_exists.sql.tmpl"))
	if err != nil {
		return ""
	}
	return embedasset.StripLeadingLineComments(string(data), "--")
}

// DefaultMigrations 返回内置数据库迁移清单。
func DefaultMigrations() []Migration {
	items := make([]Migration, 0, len(defaultMigrationSpecs))
	for _, spec := range defaultMigrationSpecs {
		sqlText := readMigrationSQL(spec.asset)
		items = append(items, Migration{
			Version:       spec.version,
			Name:          spec.name,
			Asset:         spec.asset,
			SQL:           sqlText,
			Checksum:      sha256Hex(sqlText),
			BootstrapOnly: spec.bootstrapOnly,
			Destructive:   spec.destructive,
		})
	}
	return items
}

// PendingMigrations 返回尚未在版本表中登记的迁移。
func PendingMigrations(applied map[string]struct{}) []Migration {
	migrations := DefaultMigrations()
	pending := make([]Migration, 0, len(migrations))
	for _, item := range migrations {
		if _, ok := applied[item.Version]; ok {
			continue
		}
		pending = append(pending, item)
	}
	return pending
}

// ValidateDefaultMigrations 校验默认迁移清单完整性。
func ValidateDefaultMigrations() error {
	return validateMigrationList(DefaultMigrations())
}

// MigrationAssetNames 返回仓库内 database/assets 目录的一层 SQL 资产名。
func MigrationAssetNames() ([]string, error) {
	matches, err := fs.Glob(migrationAssets, migrationAssetPath("*.sql"))
	if err != nil {
		return nil, errors.Tag(err)
	}
	for index, item := range matches {
		matches[index] = strings.TrimPrefix(item, migrationAssetDir+"/")
	}
	sort.Strings(matches)
	return matches, nil
}

// readMigrationSQL 读取指定迁移 SQL 资产。
func readMigrationSQL(asset string) string {
	data, err := migrationAssets.ReadFile(migrationAssetPath(asset))
	if err != nil {
		return ""
	}
	return stripMigrationSQLHeader(string(data))
}

// migrationAssetPath 返回 go:embed 文件系统内的资产路径，迁移记录仍保留短文件名。
func migrationAssetPath(asset string) string {
	if strings.HasPrefix(asset, migrationAssetDir+"/") {
		return asset
	}
	return migrationAssetDir + "/" + asset
}

// sha256Hex 返回文本 SHA256 十六进制摘要。
func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// stripMigrationSQLHeader 剥离 SQL 资产文件头，兼容现有 Navicat 块注释和新模板行注释。
func stripMigrationSQLHeader(sqlText string) string {
	sqlText = strings.TrimSpace(sqlText)
	sqlText = strings.TrimSpace(embedasset.StripLeadingLineComments(sqlText, "--"))
	if strings.HasPrefix(sqlText, "/*") {
		if end := strings.Index(sqlText, "*/"); end >= 0 {
			sqlText = strings.TrimSpace(sqlText[end+len("*/"):])
		}
	}
	return strings.TrimSpace(embedasset.StripLeadingLineComments(sqlText, "--"))
}
