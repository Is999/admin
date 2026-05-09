package repository

import (
	_ "embed"
	"regexp"
	"strings"

	"admin/common/embedasset"

	"github.com/Is999/go-utils/errors"
)

const (
	// sqlTemplatePreviewRuneLimit 限制模板错误中携带的 SQL 片段长度，避免日志打出整段高基数 UID 列表。
	sqlTemplatePreviewRuneLimit = 512
)

// sqlTemplatePlaceholderRegexp 匹配受控 SQL 模板占位符，并兼容 SQL formatter 插入的空格或换行。
var sqlTemplatePlaceholderRegexp = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// userTagTruncateTableTemplate 保存用户标签结果/快照表清理 DDL 模板。
// 表名来自固定分片命名并统一反引号保护，避免在业务方法中拼接 TRUNCATE SQL。
//
//go:embed assets/user_tag_truncate_table.sql.tmpl
var userTagTruncateTableTemplate string

// userTagCreateLikeTableTemplate 保存用户标签分片表按线上表结构自愈创建的 DDL 模板。
// CREATE TABLE LIKE 用于 full 临时结果表补齐，避免 tmp 表结构与线上结果表漂移。
//
//go:embed assets/user_tag_create_like_table.sql.tmpl
var userTagCreateLikeTableTemplate string

// userTagRenameTableTemplate 保存用户标签 full 结果分片原子切表 DDL 模板。
// RENAME 列表由固定分片表名渲染而来，调用方负责先 quoteIdent，避免动态 DDL 散落在业务方法中。
//
//go:embed assets/user_tag_rename_table.sql.tmpl
var userTagRenameTableTemplate string

// renderSQLTemplate 渲染本包受控 SQL 模板，并裁剪模板文件末尾换行。
// replacements 必须成对传入，调用点只传入已校验的值。
func renderSQLTemplate(template string, replacements ...string) string {
	// executableTemplate 表示已剥离文件头说明后的 SQL 模板，避免注释进入 MySQL/ClickHouse 执行链路。
	executableTemplate := embedasset.StripLeadingLineComments(template, "--")
	// normalizedTemplate 表示占位符格式已规整的模板文本，兼容 SQL formatter 插入空白的情况。
	normalizedTemplate := normalizeSQLTemplatePlaceholders(executableTemplate)
	if len(replacements) == 0 {
		return strings.TrimSpace(normalizedTemplate)
	}
	return strings.TrimSpace(strings.NewReplacer(replacements...).Replace(normalizedTemplate))
}

// normalizeSQLTemplatePlaceholders 统一模板占位符格式。
// 替换前先规整模板占位符，避免 ClickHouse 收到未渲染语法。
func normalizeSQLTemplatePlaceholders(template string) string {
	return sqlTemplatePlaceholderRegexp.ReplaceAllString(template, "{{$1}}")
}

// ensureSQLTemplateRendered 校验渲染后的 SQL 不再残留模板占位符。
// Go 侧提前拦截未渲染模板，避免任务运行后才暴露语法错误。
func ensureSQLTemplateRendered(sqlText string) error {
	if !strings.Contains(sqlText, "{{") && !strings.Contains(sqlText, "}}") {
		return nil
	}
	return errors.Errorf("SQL 模板占位符未完全渲染 preview=%s", previewSQLTemplate(sqlText))
}

// previewSQLTemplate 返回用于错误日志的 SQL 片段。
// SQL 中可能包含大批 UID 白名单，日志只保留前缀，既能定位未替换占位符，又避免刷屏。
func previewSQLTemplate(sqlText string) string {
	runes := []rune(sqlText)
	if len(runes) <= sqlTemplatePreviewRuneLimit {
		return sqlText
	}
	return string(runes[:sqlTemplatePreviewRuneLimit]) + "..."
}

// userTagTruncateTableSQL 渲染用户标签分片表清理 DDL。
func userTagTruncateTableSQL(tableName string) string {
	return renderSQLTemplate(
		userTagTruncateTableTemplate,
		"{{table_name}}", quoteIdent(tableName),
	)
}

// userTagCreateLikeTableSQL 渲染用户标签分片表结构自愈 DDL。
func userTagCreateLikeTableSQL(targetTable string, sourceTable string) string {
	return renderSQLTemplate(
		userTagCreateLikeTableTemplate,
		"{{target_table}}", quoteIdent(targetTable),
		"{{source_table}}", quoteIdent(sourceTable),
	)
}

// userTagRenameTableSQL 渲染用户标签 full 原子切表 DDL。
// renameItems 必须由固定分片表名和 quoteIdent 生成，模板只承载 RENAME TABLE 语法外壳。
func userTagRenameTableSQL(renameItems []string) string {
	return renderSQLTemplate(
		userTagRenameTableTemplate,
		"{{rename_items}}", strings.Join(renameItems, ", "),
	)
}
