package repository

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"admin/internal/svc"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

var (
	// tableExistsCache 缓存单进程内已探测表存在性，避免每个批次反复查 information_schema。
	tableExistsCache sync.Map
)

// dbCacheKey 使用底层连接池和表名生成缓存 key。
// 不能直接使用 *gorm.DB 指针，因为 WithContext 会派生新对象，导致缓存失效。
func dbCacheKey(db *gorm.DB, table string) string {
	if db == nil {
		return strings.TrimSpace(table)
	}
	if db.Statement != nil && db.Statement.ConnPool != nil {
		return fmt.Sprintf("%p:%s", db.Statement.ConnPool, strings.TrimSpace(table))
	}
	if db.Config != nil && db.Config.ConnPool != nil {
		return fmt.Sprintf("%p:%s", db.Config.ConnPool, strings.TrimSpace(table))
	}
	return fmt.Sprintf("%p:%s", db, strings.TrimSpace(table))
}

// readDB 返回指定数据库的只读连接。
func readDB(svcCtx *svc.ServiceContext, database svc.DbName) (*gorm.DB, error) {
	if svcCtx == nil {
		return nil, errors.Errorf("ServiceContext 不能为空")
	}
	db := svcCtx.ReadDB(database)
	if db == nil {
		return nil, errors.Errorf("数据库连接为空 database=%s", database)
	}
	return db, nil
}

// writeDB 返回指定数据库的写连接。
func writeDB(svcCtx *svc.ServiceContext, database svc.DbName) (*gorm.DB, error) {
	if svcCtx == nil {
		return nil, errors.Errorf("ServiceContext 不能为空")
	}
	db := svcCtx.WriteDB(database)
	if db == nil {
		return nil, errors.Errorf("数据库写连接为空 database=%s", database)
	}
	return db, nil
}

// tableExists 判断当前连接所在库中表是否存在。
func tableExists(db *gorm.DB, table string) bool {
	if db == nil || strings.TrimSpace(table) == "" {
		return false
	}
	cacheKey := dbCacheKey(db, table)
	if val, ok := tableExistsCache.Load(cacheKey); ok {
		return val.(bool)
	}
	var count int64
	err := db.Table("information_schema.TABLES").
		Where("TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table).
		Count(&count).Error
	exists := err == nil && count > 0
	tableExistsCache.Store(cacheKey, exists)
	return exists
}

// chunkInt64s 按批次切分 UID 集合。
func chunkInt64s(items []int64, batchSize int) [][]int64 {
	if batchSize <= 0 {
		batchSize = 1000
	}
	chunks := make([][]int64, 0, (len(items)+batchSize-1)/batchSize)
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

// uniqueInt64s 清洗 UID 集合并按升序去重。
// 统一过滤非正 UID 并稳定排序。
func uniqueInt64s(items []int64) []int64 {
	out := make([]int64, 0, len(items))
	for _, item := range items {
		if item <= 0 {
			continue
		}
		out = append(out, item)
	}
	out = utils.Unique(out)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// chunkStrings 按批次切分字符串集合。
// 按 batchSize 拆分字符串 IN 条件，非正数时使用保守默认值。
func chunkStrings(items []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = 1000
	}
	chunks := make([][]string, 0, (len(items)+batchSize-1)/batchSize)
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

// filterUIDsByShard 按 uid%shard_total 过滤当前 worker 负责的 UID。
func filterUIDsByShard(uids []int64, shardIndex, shardTotal int) []int64 {
	if shardTotal <= 0 {
		shardTotal = 1
	}
	out := make([]int64, 0, len(uids))
	for _, uid := range uids {
		if uid <= 0 {
			continue
		}
		shard := int(uid % int64(shardTotal))
		if shard < 0 {
			shard += shardTotal
		}
		if shard == shardIndex%shardTotal {
			out = append(out, uid)
		}
	}
	return out
}

// toInt64 把数据库 map 扫描值转换为 int64。
func toInt64(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case sql.NullInt64:
		if !v.Valid {
			return 0
		}
		return v.Int64
	case sql.NullString:
		if !v.Valid {
			return 0
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v.String), 10, 64)
		return n
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case []byte:
		n, _ := strconv.ParseInt(string(v), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

// toFloat64 把数据库 map 扫描值转换为 float64。
func toFloat64(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case float32:
		return float64(v)
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case []byte:
		n, _ := strconv.ParseFloat(string(v), 64)
		return n
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	default:
		return 0
	}
}

// toNullTime 把数据库 map 扫描值转换为 sql.NullTime。
func toNullTime(value any) sql.NullTime {
	switch v := value.(type) {
	case nil:
		return sql.NullTime{}
	case sql.NullTime:
		return v
	case sql.NullString:
		if !v.Valid {
			return sql.NullTime{}
		}
		return parseNullTime(v.String)
	case time.Time:
		return sql.NullTime{Time: v, Valid: true}
	case []byte:
		return parseNullTime(string(v))
	case string:
		return parseNullTime(v)
	default:
		return sql.NullTime{}
	}
}

// businessTimeFromValue 把业务时间字段转换为本地时间，兼容 datetime 字符串和 Unix 秒。
func businessTimeFromValue(value any) sql.NullTime {
	if parsed := toNullTime(value); parsed.Valid {
		return parsed
	}
	return unixSecondsValueToLocalNullTime(toInt64(value))
}

// unixSecondsValueToLocalNullTime 把 Unix 秒数值转换成本地时区时间。
func unixSecondsValueToLocalNullTime(value int64) sql.NullTime {
	return unixSecondsToLocalNullTime(sql.NullInt64{Int64: value, Valid: value > 0})
}

// unixSecondsToLocalNullTime 把 Unix 秒转换成本地时区时间。
func unixSecondsToLocalNullTime(value sql.NullInt64) sql.NullTime {
	if !value.Valid || value.Int64 <= 0 {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: time.Unix(value.Int64, 0).In(time.Local), Valid: true}
}

// parseNullTime 兼容日期和日期时间字符串。
func parseNullTime(value string) sql.NullTime {
	value = strings.TrimSpace(value)
	if value == "" || value == "0000-00-00" || strings.HasPrefix(value, "0000-00-00 ") {
		return sql.NullTime{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		time.DateTime,
		time.DateOnly,
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return sql.NullTime{Time: parsed, Valid: true}
		}
	}
	return sql.NullTime{}
}

// startOfDay 返回本地时区自然日起点。
func startOfDay(now time.Time) time.Time {
	y, m, d := now.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
