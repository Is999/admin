package model

import "time"

const (
	// TableNameShardBackfillCheckpoint 是源库 shard_no 回填任务进度表名。
	TableNameShardBackfillCheckpoint = "shard_backfill_checkpoint"
)

// ShardBackfillCheckpoint 表示源库内可恢复的 shard_no 回填与校验进度。
type ShardBackfillCheckpoint struct {
	JobName       string    `gorm:"column:job_name;type:varchar(64);primaryKey;comment:回填任务唯一名称"`                                                         // 回填任务唯一名称
	Table         string    `gorm:"column:table_name;type:varchar(64);not null;index:idx_shard_backfill_table_status,priority:1;comment:业务表名"`            // 业务表名
	PrimaryKey    string    `gorm:"column:primary_key;type:varchar(64);not null;comment:单调唯一游标列"`                                                         // 单调唯一游标列
	UIDColumn     string    `gorm:"column:uid_column;type:varchar(64);not null;comment:业务用户ID列"`                                                          // 业务用户 ID 列
	InsertTrigger string    `gorm:"column:insert_trigger;type:varchar(64);not null;comment:源表插入保护触发器"`                                                    // 源表插入保护触发器
	UpdateTrigger string    `gorm:"column:update_trigger;type:varchar(64);not null;comment:源表更新保护触发器"`                                                    // 源表更新保护触发器
	RangeStart    uint64    `gorm:"column:range_start;type:bigint unsigned;not null;comment:回填起点，不包含"`                                                    // 回填起点，不包含
	RangeEnd      uint64    `gorm:"column:range_end;type:bigint unsigned;not null;comment:回填终点，包含"`                                                       // 回填终点，包含
	Cursor        uint64    `gorm:"column:cursor_value;type:bigint unsigned;not null;comment:已提交回填游标"`                                                    // 已提交回填游标
	VerifyCursor  uint64    `gorm:"column:verify_cursor;type:bigint unsigned;not null;comment:已提交校验游标"`                                                   // 已提交校验游标
	UpdatedRows   uint64    `gorm:"column:updated_rows;type:bigint unsigned;not null;default:0;comment:累计修正行数"`                                           // 累计修正行数
	VerifiedRows  uint64    `gorm:"column:verified_rows;type:bigint unsigned;not null;default:0;comment:累计校验行数"`                                          // 累计校验行数
	MismatchRows  uint64    `gorm:"column:mismatch_rows;type:bigint unsigned;not null;default:0;comment:校验不一致行数"`                                         // 校验不一致行数
	Status        string    `gorm:"column:status;type:varchar(16);not null;index:idx_shard_backfill_table_status,priority:2;comment:任务状态"`                // 任务状态：running/backfilled/verifying/verified/mismatch
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);comment:创建时间"`                                // 创建时间
	UpdatedAt     time.Time `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3);comment:更新时间"` // 更新时间
}

// TableName 返回源库 shard_no 回填任务进度表名。
func (*ShardBackfillCheckpoint) TableName() string {
	return TableNameShardBackfillCheckpoint
}
