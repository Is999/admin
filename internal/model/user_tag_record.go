package model

import "time"

// UserTagRecord 表示最终用户标签记录。
type UserTagRecord struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement;comment:标签记录主键" json:"id"`                                                                                                // 标签记录主键
	UID         uint64    `gorm:"column:uid;type:bigint unsigned;not null;uniqueIndex:uk_uid_tag_type,priority:1;index:idx_shard_uid,priority:2;index:idx_tag_type_uid,priority:2;comment:用户UID" json:"uid"`       // 用户 UID
	ShardNo     int       `gorm:"column:shard_no;type:int;not null;default:0;index:idx_shard_uid,priority:1;index:idx_tag_type_shard_uid,priority:2;comment:CRC32(uid字符串)%1024固定逻辑桶" json:"shard_no"`              // 与 user.shard_no 一致的固定逻辑桶
	TagType     int       `gorm:"column:tag_type;type:int;not null;uniqueIndex:uk_uid_tag_type,priority:2;index:idx_tag_type_uid,priority:1;index:idx_tag_type_shard_uid,priority:1;comment:标签类型" json:"tag_type"` // 标签类型
	TagSource   int8      `gorm:"column:tag_source;type:tinyint;not null;default:0;comment:标签来源" json:"tag_source"`                                                                                                // 标签来源
	TagData     int       `gorm:"column:tag_data;type:int;not null;default:0;comment:标签附加数据" json:"tag_data"`                                                                                                      // 标签附加数据
	TagCategory string    `gorm:"column:tag_category;type:varchar(50);not null;default:'';comment:标签大类" json:"tag_category"`                                                                                       // 标签大类
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                                               // 创建时间
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`                                                                               // 更新时间
}

// TableName 返回当前分表方案的用户标签起始表名。
func (*UserTagRecord) TableName() string {
	return TableNameUserTag
}
