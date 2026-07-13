package model

import (
	"admin/common/idgen"

	"github.com/Is999/go-utils/errors"
)

const (
	// TableNameUserTag 是当前分表方案暴露的用户标签起始表名。
	TableNameUserTag = "user_tag"
)

// UserTagPhysicalTableName 返回固定逻辑桶对应的用户标签表名。
func UserTagPhysicalTableName(shardNo int, routeShardCount int) (string, error) {
	if shardNo < 0 || shardNo >= idgen.ShardMod {
		return "", errors.Errorf("用户标签 shard_no 必须在 0-%d 之间", idgen.ShardMod-1)
	}
	if routeShardCount <= 0 || routeShardCount > idgen.ShardMod || routeShardCount&(routeShardCount-1) != 0 {
		return "", errors.Errorf("用户标签物理分片数量仅支持 1/2/4/8/16/32/64/128/256/512/1024")
	}
	return TableNameUserTag, nil
}
