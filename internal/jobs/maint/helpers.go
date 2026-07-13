package maint

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"admin/helper"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

var (
	// identifierPattern 约束动态 SQL 标识符只允许普通表名、列名或 named collection 名称，避免拼接 SQL 时扩大注入面。
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// indexPrefixCache 缓存单进程内已经成功校验过的索引访问路径，避免周期任务每轮重复读取 information_schema。
	indexPrefixCache sync.Map
)

const (
	// checkpointErrorMaxBytes 表示维护任务 checkpoint 错误摘要最大字节数，完整错误链只保留在任务日志中。
	checkpointErrorMaxBytes = 500
)

// IndexPrefixCheck 表示大表游标扫描前需要校验的索引访问路径。
type IndexPrefixCheck struct {
	Subject            string   // Subject 表示错误信息中的对象名称，例如 archive_source
	Table              string   // Table 表示需要读取索引元数据的 MySQL 表名
	LeadColumn         string   // LeadColumn 表示范围扫描必须命中的左前缀列
	RecommendedColumns []string // RecommendedColumns 表示推荐 DBA 在线补充的索引列顺序
}

// ValidateClickHouseCollectionName 校验 ClickHouse named collection 名称。
// named collection 只允许普通标识符，避免扩大 SQL 注入面。
func ValidateClickHouseCollectionName(name string, emptyHint string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		emptyHint = strings.TrimSpace(emptyHint)
		if emptyHint == "" {
			emptyHint = "app_id 或 clickhouse_mysql_collection"
		}
		return errors.Errorf("ClickHouse MySQL named collection 不能为空，请配置 %s", emptyHint)
	}
	if !identifierPattern.MatchString(name) {
		return errors.Errorf("ClickHouse MySQL named collection 名称非法 collection=%s", name)
	}
	return nil
}

// EnsureIndexPrefix 校验 MySQL 热表是否具备指定左前缀索引。
// 大表任务缺少范围索引时直接失败，避免反复全表扫描。
func EnsureIndexPrefix(ctx context.Context, db *gorm.DB, check IndexPrefixCheck) error {
	subject := helper.FirstNonEmptyString(check.Subject, check.Table, "维护任务")
	table := strings.TrimSpace(check.Table)
	leadColumn := strings.TrimSpace(check.LeadColumn)
	if db == nil {
		return errors.Errorf("%s 数据库连接为空", subject)
	}
	if table == "" || leadColumn == "" {
		return errors.Errorf("%s 索引校验配置缺失 table=%s lead_column=%s", subject, table, leadColumn)
	}
	cacheID := dbCacheKey(db, "index:"+table+":"+strings.ToLower(leadColumn))
	if val, ok := indexPrefixCache.Load(cacheID); ok && val.(bool) {
		return nil
	}
	indexes, err := db.WithContext(ctx).Migrator().GetIndexes(table)
	if err != nil {
		return errors.Tag(err)
	}
	for _, index := range indexes {
		if IndexHasPrefix(index.Columns(), leadColumn) {
			indexPrefixCache.Store(cacheID, true)
			return nil
		}
	}
	recommended := recommendedIndexColumns(check.RecommendedColumns, leadColumn)
	return errors.Errorf("%s 表缺少 %s 左前缀索引，请先由 DBA 补充生产索引: table=%s recommended_index=(%s)",
		subject, leadColumn, table, strings.Join(recommended, ","))
}

// IndexHasPrefix 判断索引字段列表是否以目标列开头。
// InnoDB 二级索引会附带主键，因此只要范围列是左前缀，就能支撑“范围列升序 + 主键升序”的游标扫描。
func IndexHasPrefix(columns []string, leadColumn string) bool {
	if len(columns) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(columns[0]), strings.TrimSpace(leadColumn))
}

// QuoteClickHouseIdent 返回安全的 ClickHouse 标识符。
func QuoteClickHouseIdent(name string) string {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// QuoteClickHouseCollection 返回安全的 ClickHouse named collection 标识符。
// ClickHouse named collection 在 mysql() 表函数中不加反引号，非法值由配置校验提前拦截。
func QuoteClickHouseCollection(name string) string {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return "``"
	}
	return name
}

// ClickHouseStringLiteral 返回 ClickHouse SQL 字符串字面量。
func ClickHouseStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// QuoteMySQLIdent 返回安全的 MySQL 标识符。
func QuoteMySQLIdent(name string) string {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// UInt64ListSQL 把无符号整数集合转成 ClickHouse IN 列表。
// 输入应来自受 batch_size 保护的游标扫描结果，不能用于无界用户输入集合。
func UInt64ListSQL(values []uint64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ",")
}

// EpochTime 返回维护任务控制表使用的最小有效时间占位。
// 该值用于 checkpoint 初始化、水位判空和清理边界判断，统一为业务本地时区 1970-01-01。
func EpochTime() time.Time {
	return time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local)
}

// PositiveOrDefault 返回正数配置值；非正数按默认值回退。
// 维护任务读取 YAML 时会先经过该方法归一化，避免执行阶段继续判断无效配置。
func PositiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// ClampPositive 对正数配置应用默认值、下限和上限保护。
// 该方法用于批次大小、窗口数量等生产保护参数，避免误配置扩大单轮扫描或写入压力。
func ClampPositive(value int, fallback int, minValue int, maxValue int) int {
	value = PositiveOrDefault(value, fallback)
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// ClampNonNegative 对非负配置应用默认值和上限保护。
// 该方法用于批次间 sleep 等允许为 0 的参数，负数视为无效配置并回退默认值。
func ClampNonNegative(value int, fallback int, maxValue int) int {
	if value < 0 {
		value = fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// SleepBatch 在批次之间做可取消短暂停顿，用于降低数据库持续压力。
func SleepBatch(ctx context.Context, sleepMs int) {
	if sleepMs <= 0 {
		return
	}
	timer := time.NewTimer(time.Duration(sleepMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// TruncateCheckpointError 截断维护任务 checkpoint 中保存的错误摘要。
// checkpoint 只用于页面和排障索引展示，不能写入过长错误链；完整错误上下文由最外层 worker 日志统一记录。
func TruncateCheckpointError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) <= checkpointErrorMaxBytes {
		return text
	}
	return text[:checkpointErrorMaxBytes]
}

// dbCacheKey 使用底层连接池和校验对象生成缓存 key。
// 不能直接使用 *gorm.DB 指针，因为 WithContext 会派生新对象，导致同一连接池重复校验。
func dbCacheKey(db *gorm.DB, name string) string {
	name = strings.TrimSpace(name)
	if db == nil {
		return name
	}
	if db.Statement != nil && db.Statement.ConnPool != nil {
		return fmt.Sprintf("%p:%s", db.Statement.ConnPool, name)
	}
	if db.Config != nil && db.Config.ConnPool != nil {
		return fmt.Sprintf("%p:%s", db.Config.ConnPool, name)
	}
	return fmt.Sprintf("%p:%s", db, name)
}

// recommendedIndexColumns 返回错误提示中的推荐索引列。
// 未显式传入推荐列时至少返回左前缀列，保证排障提示有可执行方向。
func recommendedIndexColumns(columns []string, leadColumn string) []string {
	normalized := normalizeNames(columns)
	if len(normalized) > 0 {
		return normalized
	}
	return []string{strings.TrimSpace(leadColumn)}
}

// normalizeNames 清洗字段或列名列表，生成稳定缓存 key 和错误提示。
func normalizeNames(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
