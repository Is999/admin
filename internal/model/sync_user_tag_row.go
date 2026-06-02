package model

// SyncUserTagRow 表示标签表与同步快照表差异比对使用的轻量行。
type SyncUserTagRow struct {
	ID      int64 `gorm:"column:id;type:bigint;comment:主键" json:"id"`            // 主键
	UID     int64 `gorm:"column:uid;type:bigint;comment:用户UID" json:"uid"`       // 用户 UID
	ShardNo int   `gorm:"column:shard_no;type:int;comment:取模分片" json:"shard_no"` // 取模分片
	TagType int   `gorm:"column:tag_type;type:int;comment:标签类型" json:"tag_type"` // 标签类型
}
