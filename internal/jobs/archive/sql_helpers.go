package archive

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/common/embedasset"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var (
	// identifierPattern 约束动态 SQL 标识符只允许字母、数字和下划线，避免拼接表名/列名时被注入。
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// archiveConditionBlockedPattern 限制自定义条件只表达筛选谓词。
	archiveConditionBlockedPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(select|union|insert|update|delete|drop|truncate|alter|create|replace|grant|revoke|call|execute|load|into|outfile|infile|sleep|benchmark)([^a-z0-9_]|$)`)
)

// archiveIndexHasTimePrefix 判断索引是否能支撑归档按时间窗口递增扫描。
// InnoDB 二级索引会携带主键，生产已有 KEY(time_column) 时也能支撑当前 `ORDER BY time_column, primary_key` 的小批量访问。
func archiveIndexHasTimePrefix(columns []string, timeColumn string) bool {
	if len(columns) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(columns[0]), strings.TrimSpace(timeColumn))
}

// isArchivedSegmentStatus 判断区间是否已经完成历史表归档。
// deleted/deleting 也属于已归档状态，允许归档 watermark 连续推进。
func isArchivedSegmentStatus(status string) bool {
	switch status {
	case statusDone, statusDeleting, statusDeleted:
		return true
	default:
		return false
	}
}

// normalizeArchiveSQLCondition 归一化运行期配置里的自定义 WHERE 条件。
// 条件会进入归档和删除 SQL，只允许安全谓词片段。
func normalizeArchiveSQLCondition(value string) (string, error) {
	condition := strings.TrimSpace(value)
	if condition == "" {
		return "", nil
	}
	if strings.Contains(condition, "\x00") {
		return "", errors.New("条件包含非法空字节")
	}
	if strings.Contains(condition, "?") {
		return "", errors.New("条件不支持占位符")
	}
	if strings.Contains(condition, ";") ||
		strings.Contains(condition, "--") ||
		strings.Contains(condition, "/*") ||
		strings.Contains(condition, "*/") {
		return "", errors.New("条件不允许包含注释或多语句符号")
	}
	scanText, quoteClosed := maskArchiveConditionQuotedText(condition)
	if !quoteClosed {
		return "", errors.New("条件引号不匹配")
	}
	if strings.Count(scanText, "(") != strings.Count(scanText, ")") {
		return "", errors.New("条件括号不匹配")
	}
	if archiveConditionBlockedPattern.MatchString(scanText) {
		return "", errors.New("条件只能是过滤谓词，不能包含查询、写入或高风险函数")
	}
	return condition, nil
}

// maskArchiveConditionQuotedText 屏蔽条件中的字符串字面量，避免把业务枚举值误判为 SQL 动作关键字。
// 返回的 bool 表示引号是否闭合，未闭合条件会在启动期被拒绝，防止最终 SQL 语义漂移。
func maskArchiveConditionQuotedText(value string) (string, bool) {
	var builder strings.Builder
	builder.Grow(len(value))
	var quote byte
	inQuote := false
	for idx := 0; idx < len(value); idx++ {
		ch := value[idx]
		if inQuote {
			builder.WriteByte(' ')
			if ch == '\\' && idx+1 < len(value) {
				idx++
				builder.WriteByte(' ')
				continue
			}
			if ch == quote {
				if idx+1 < len(value) && value[idx+1] == quote {
					idx++
					builder.WriteByte(' ')
					continue
				}
				inQuote = false
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = true
			quote = ch
			builder.WriteByte(' ')
			continue
		}
		builder.WriteByte(ch)
	}
	return builder.String(), !inQuote
}

// archiveConditionSQL 返回归档阶段使用的自定义条件。
// 空字符串表示只按时间窗口和主键游标筛选，不附加业务谓词。
func archiveConditionSQL(job jobConfig) string {
	return strings.TrimSpace(job.ArchiveCondition)
}

// deleteConditionSQL 返回清理热表时使用的完整条件。
// 删除条件会叠加归档条件，保证配置了部分归档时不会误删未进入历史表的数据。
func deleteConditionSQL(job jobConfig) string {
	conditions := make([]string, 0, 2)
	if condition := archiveConditionSQL(job); condition != "" {
		conditions = append(conditions, condition)
	}
	if condition := strings.TrimSpace(job.DeleteCondition); condition != "" {
		conditions = append(conditions, condition)
	}
	if len(conditions) == 0 {
		return ""
	}
	return combineArchiveSQLConditions(conditions...)
}

// combineArchiveSQLConditions 合并多个已校验条件，确保 OR 条件在叠加时不会被 AND 改变优先级。
func combineArchiveSQLConditions(conditions ...string) string {
	parts := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		if condition = strings.TrimSpace(condition); condition != "" {
			parts = append(parts, condition)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ") AND (") + ")"
}

// batchSourcePredicateSQL 返回归档/删除批次的基础谓词。
// 主键集合和时间半开区间一起落入事务，避免跨窗口归档或删除。
func batchSourcePredicateSQL(job jobConfig) string {
	return fmt.Sprintf("%s IN ? AND %s >= ? AND %s < ?",
		quoteIdent(job.PrimaryKey),
		quoteIdent(job.TimeColumn),
		quoteIdent(job.TimeColumn),
	)
}

// rangePredicateArgs 返回时间半开区间的查询参数，按时间列类型匹配数据库字段。
func rangePredicateArgs(job jobConfig, start time.Time, end time.Time) []any {
	return []any{archiveTimeArg(job, start), archiveTimeArg(job, end)}
}

// archiveBatchInsertSQL 渲染归档批次复制 SQL。
// INSERT IGNORE SELECT 使用模板化原生 SQL，只替换受控标识符和条件。
func archiveBatchInsertSQL(job jobConfig, segment *Segment, columnList string) string {
	archiveCondition := ""
	if condition := archiveConditionSQL(job); condition != "" {
		archiveCondition = " AND (" + condition + ")"
	}
	columnList = strings.TrimSpace(columnList)
	return renderArchiveSQLTemplate(
		archiveBatchInsertTemplate,
		"{{history_table}}", quoteIdent(segment.HistoryTableName),
		"{{columns}}", columnList,
		"{{source_table}}", quoteIdent(job.TableName),
		"{{primary_key}}", quoteIdent(job.PrimaryKey),
		"{{time_column}}", quoteIdent(job.TimeColumn),
		"{{archive_condition}}", archiveCondition,
	)
}

// archiveTableColumnList 返回归档复制使用的显式列清单，禁止模板使用 SELECT *。
func archiveTableColumnList(ctx context.Context, db *gorm.DB, table string) (string, error) {
	if db == nil {
		return "", errors.Errorf("归档表字段查询失败: table=%s db=nil", table)
	}
	columnTypes, err := db.WithContext(ctx).Migrator().ColumnTypes(table)
	if err != nil {
		return "", errors.Wrapf(err, "查询归档表字段失败 table=%s", table)
	}
	columns := make([]string, 0, len(columnTypes))
	for _, columnType := range columnTypes {
		name := strings.TrimSpace(columnType.Name())
		if !identifierPattern.MatchString(name) {
			return "", errors.Errorf("归档表字段名非法: table=%s column=%s", table, name)
		}
		columns = append(columns, quoteIdent(name))
	}
	if len(columns) == 0 {
		return "", errors.Errorf("归档表字段为空: table=%s", table)
	}
	return strings.Join(columns, ", "), nil
}

// archiveDropHistoryTableSQL 渲染历史表淘汰 DDL。
// 历史表名来自已完成区间元数据，执行前仍会通过 tableExists 精确确认，避免误删非归档表。
func archiveDropHistoryTableSQL(historyTable string) string {
	return renderArchiveSQLTemplate(
		archiveDropHistoryTableTemplate,
		"{{table_name}}", quoteIdent(historyTable),
	)
}

// archiveCreateHistoryTableSQL 渲染历史表结构自愈 DDL。
// 历史表复用热表结构和索引，避免归档任务与线上表结构变更产生 DDL 漂移。
func archiveCreateHistoryTableSQL(historyTable string, sourceTable string) string {
	return renderArchiveSQLTemplate(
		archiveCreateHistoryTableTemplate,
		"{{history_table}}", quoteIdent(historyTable),
		"{{source_table}}", quoteIdent(sourceTable),
	)
}

// renderArchiveSQLTemplate 渲染归档模块受控 SQL 模板，并裁剪模板文件末尾换行。
// replacements 必须按占位符和值成对传入；调用点只传入已转义标识符、内部状态或经过配置校验的谓词片段。
func renderArchiveSQLTemplate(template string, replacements ...string) string {
	// executableTemplate 是剥离文件头说明后的 SQL 模板，避免注释进入数据库执行链路。
	executableTemplate := embedasset.StripLeadingLineComments(template, "--")
	if len(replacements) == 0 {
		return strings.TrimSpace(executableTemplate)
	}
	return strings.TrimSpace(strings.NewReplacer(replacements...).Replace(executableTemplate))
}

// quoteIdent 为动态 SQL 标识符补反引号。
// 标识符已由配置入口校验，这里仍做反引号转义。
func quoteIdent(name string) string {
	name = strings.TrimSpace(name)
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// tableExists 检查当前数据库内指定表是否存在。
func tableExists(ctx context.Context, db *gorm.DB, table string) bool {
	if db == nil || strings.TrimSpace(table) == "" {
		return false
	}
	var count int64
	err := db.WithContext(ctx).
		Table("information_schema.TABLES").
		Where("TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table).
		Count(&count).Error
	return err == nil && count > 0
}

// withWriteResolver 为 GORM 连接显式附加主库路由。
func withWriteResolver(db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.Clauses(dbresolver.Write)
}

// positiveOr 在配置值不合法时回退到默认值，减少分散的判断分支。
func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// waitArchiveBatch 在归档批次之间短暂让出 MySQL 资源。
// 等待必须响应 context 取消和任务超时，避免限速逻辑反过来拖慢 worker 退出。
func waitArchiveBatch(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return errors.Tag(ctx.Err())
	}
}
