package model

import (
	"admin/internal/sharding"

	"github.com/Is999/go-utils/errors"
)

// UserPhysicalTableName 返回固定逻辑桶对应的用户表名。
func UserPhysicalTableName(shardNo int, routeShardCount int) (string, error) {
	plan, err := sharding.NewPlan(TableNameUser, normalizeUserRouteShardCount(routeShardCount))
	if err != nil {
		return "", errors.Tag(err)
	}
	table, err := plan.TableForBucket(shardNo)
	if err != nil {
		return "", errors.Tag(err)
	}
	return table.Name, nil
}

// normalizeUserRouteShardCount 规范化用户物理分片数，空值使用单表。
func normalizeUserRouteShardCount(routeShardCount int) int {
	if routeShardCount == 0 {
		return UserRouteShardCountDefault
	}
	return routeShardCount
}
