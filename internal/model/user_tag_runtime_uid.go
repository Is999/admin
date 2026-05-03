package model

import "time"

const (
	// TableNameUserTagRuntimeUID 用户标签工作流运行期 UID 表名。
	TableNameUserTagRuntimeUID = "user_tag_runtime_uid"
	// TableNameUserTagRuntimeCheckpoint 用户标签工作流节点进度表名。
	TableNameUserTagRuntimeCheckpoint = "user_tag_runtime_checkpoint"
)

// UserTagRuntimeUID 表示单次用户标签工作流内需要继续处理的 UID。
// 骨架只定义候选集合承载方式，具体范围来源由业务插件接入。
type UserTagRuntimeUID struct {
	WorkflowID string    `gorm:"column:workflow_id;type:varchar(80);primaryKey;index:idx_workflow_shard_uid,priority:1;comment:工作流ID" json:"workflow_id"`            // 工作流 ID
	UID        int64     `gorm:"column:uid;type:bigint;primaryKey;index:idx_workflow_shard_uid,priority:3;index:idx_created_uid,priority:2;comment:用户ID" json:"uid"` // 用户 ID
	ShardNo    int8      `gorm:"column:shard_no;type:tinyint;not null;default:0;index:idx_workflow_shard_uid,priority:2;comment:uid取模分片" json:"shard_no"`            // UID 取模分片
	Scope      string    `gorm:"column:scope;type:varchar(40);not null;default:'';comment:候选范围标识" json:"scope"`                                                      // 候选范围标识
	Reason     string    `gorm:"column:reason;type:varchar(80);not null;default:'';comment:进入候选集合原因" json:"reason"`                                                  // 候选原因
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;index:idx_created_uid,priority:1;comment:创建时间" json:"created_at"` // 创建时间
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`      // 更新时间
}

// TableName 返回用户标签工作流运行期 UID 表名。
func (*UserTagRuntimeUID) TableName() string {
	return TableNameUserTagRuntimeUID
}

// UserTagRuntimeCheckpoint 表示用户标签工作流节点的游标和状态。
type UserTagRuntimeCheckpoint struct {
	WorkflowID string    `gorm:"column:workflow_id;type:varchar(80);primaryKey;index:idx_updated_at,priority:2;comment:工作流ID" json:"workflow_id"`                                               // 工作流 ID
	Node       string    `gorm:"column:node;type:varchar(40);primaryKey;comment:工作流节点" json:"node"`                                                                                             // 工作流节点
	ShardNo    int       `gorm:"column:shard_no;type:int;primaryKey;comment:节点内部分片号" json:"shard_no"`                                                                                           // 节点分片号
	CursorUID  int64     `gorm:"column:cursor_uid;type:bigint;not null;default:0;comment:UID游标" json:"cursor_uid"`                                                                              // UID 游标
	Status     int8      `gorm:"column:status;type:tinyint;not null;default:0;comment:状态：0待处理 1处理中 2完成 3失败" json:"status"`                                                                      // 处理状态
	ErrorText  string    `gorm:"column:error_text;type:varchar(1000);not null;default:'';comment:最近失败摘要" json:"error_text"`                                                                     // 最近失败摘要
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                             // 创建时间
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;index:idx_updated_at,priority:1;comment:更新时间" json:"updated_at"` // 更新时间
}

// TableName 返回用户标签工作流节点进度表名。
func (*UserTagRuntimeCheckpoint) TableName() string {
	return TableNameUserTagRuntimeCheckpoint
}
