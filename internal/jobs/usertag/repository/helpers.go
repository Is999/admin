package repository

import (
	"fmt"
	"sort"
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

// startOfDay 返回本地时区自然日起点。
func startOfDay(now time.Time) time.Time {
	y, m, d := now.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
