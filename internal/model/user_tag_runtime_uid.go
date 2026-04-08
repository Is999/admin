package model

import "time"

const (
	// TableNameUserTagRuntimeUID 用户标签工作流运行期 UID 表名。
	TableNameUserTagRuntimeUID = "user_tag_runtime_uid"
)

// UserTagRuntimeUID 表示单次用户标签工作流内需要继续处理的 UID。
type UserTagRuntimeUID struct {
	WorkflowID string    `gorm:"column:workflow_id;type:varchar(80);primaryKey;index:idx_workflow_shard_uid,priority:1;comment:工作流ID" json:"workflow_id"`            // 工作流 ID
	UID        int64     `gorm:"column:uid;type:bigint;primaryKey;index:idx_workflow_shard_uid,priority:3;index:idx_created_uid,priority:2;comment:用户ID" json:"uid"` // 用户 ID
	ShardNo    int8      `gorm:"column:shard_no;type:tinyint;not null;default:0;index:idx_workflow_shard_uid,priority:2;comment:uid取模分片" json:"shard_no"`            // UID 取模分片
	Source     string    `gorm:"column:source;type:varchar(40);not null;default:'';comment:命中来源节点" json:"source"`                                                    // 命中来源节点
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;index:idx_created_uid,priority:1;comment:创建时间" json:"created_at"` // 创建时间
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`      // 更新时间
}

// TableName 返回用户标签工作流运行期 UID 表名。
func (*UserTagRuntimeUID) TableName() string {
	return TableNameUserTagRuntimeUID
}
