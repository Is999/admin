package repository

import (
	"sort"

	"admin/internal/svc"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// writeDB 返回指定数据库的写连接。
func writeDB(svcCtx *svc.ServiceContext, database svc.DBName) (*gorm.DB, error) {
	if svcCtx == nil {
		return nil, errors.Errorf("ServiceContext 不能为空")
	}
	db := svcCtx.WriteDB(database)
	if db == nil {
		return nil, errors.Errorf("数据库写连接为空 database=%s", database)
	}
	return db, nil
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
