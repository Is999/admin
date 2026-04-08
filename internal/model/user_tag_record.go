package model

import (
	"fmt"
	"time"
)

const (
	// TableNameUserTagShardPrefix 用户标签结果分片表名前缀。
	TableNameUserTagShardPrefix = "user_tag_"
	// TableNameUserTagSyncShardPrefix 用户标签同步快照分片表名前缀。
	TableNameUserTagSyncShardPrefix = "user_tag_sync_"
)

// UserTagRecord 表示最终用户标签记录。
type UserTagRecord struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement;comment:主键" json:"id"`                                                               // 主键
	UID         uint64    `gorm:"column:uid;type:bigint unsigned;not null;uniqueIndex:uk_uid_tag_type,priority:1;index:idx_tag_type_uid,priority:2;comment:用户UID" json:"uid"` // 用户 UID
	TagType     int       `gorm:"column:tag_type;type:int;not null;uniqueIndex:uk_uid_tag_type,priority:2;index:idx_tag_type_uid,priority:1;comment:标签类型" json:"tag_type"`    // 标签类型
	TagSource   int8      `gorm:"column:tag_source;type:tinyint;not null;default:0;comment:标签来源" json:"tag_source"`                                                           // 标签来源
	TagData     int       `gorm:"column:tag_data;type:int;not null;default:0;comment:标签附加数据" json:"tag_data"`                                                                 // 标签附加数据
	TagCategory string    `gorm:"column:tag_category;type:varchar(50);not null;default:'';comment:标签大类" json:"tag_category"`                                                  // 标签大类
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                          // 创建时间
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`                                          // 更新时间
}

// UserTagShardTableName 返回指定分片的用户标签结果表名。
func UserTagShardTableName(shard int) string {
	return fmt.Sprintf("%s%d", TableNameUserTagShardPrefix, shard)
}

// UserTagTmpShardTableName 返回指定分片的 full 临时结果表名。
func UserTagTmpShardTableName(shard int) string {
	return fmt.Sprintf("%s%d_tmp", TableNameUserTagShardPrefix, shard)
}

// UserTagSwapShardTableName 返回指定分片的 full 原子切换中间表名。
func UserTagSwapShardTableName(shard int) string {
	return fmt.Sprintf("%s%d_swap", TableNameUserTagShardPrefix, shard)
}

// UserTagSyncShardTableName 返回指定分片的用户标签同步快照表名。
func UserTagSyncShardTableName(shard int) string {
	return fmt.Sprintf("%s%d", TableNameUserTagSyncShardPrefix, shard)
}
