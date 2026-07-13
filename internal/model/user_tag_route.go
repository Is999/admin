package model

import (
	"admin/internal/sharding"

	"github.com/Is999/go-utils/errors"
)

const (
	// TableNameUserTag 是当前分表方案暴露的用户标签起始表名。
	TableNameUserTag = "user_tag_0"
)

// UserTagPhysicalTableName 返回固定逻辑桶对应的用户标签表名。
func UserTagPhysicalTableName(shardNo int, routeShardCount int) (string, error) {
	plan, err := sharding.NewPlan(TableNameUserTag, routeShardCount)
	if err != nil {
		return "", errors.Tag(err)
	}
	table, err := plan.TableForBucket(shardNo)
	if err != nil {
		return "", errors.Tag(err)
	}
	return table.Name, nil
}
