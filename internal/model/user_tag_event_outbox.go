package model

import "time"

// TableNameUserTagEventOutbox 表示用户标签得失事件 outbox 表名。
const TableNameUserTagEventOutbox = "user_tag_event_outbox"

// UserTagEventOutboxState 表示用户标签事件 outbox 处理状态。
type UserTagEventOutboxState int8

const (
	UserTagEventOutboxStatePending UserTagEventOutboxState = 0 // 待派发
	UserTagEventOutboxStateRunning UserTagEventOutboxState = 1 // 派发中
	UserTagEventOutboxStateDone    UserTagEventOutboxState = 2 // 已派发
	UserTagEventOutboxStateRetry   UserTagEventOutboxState = 3 // 待重试
	UserTagEventOutboxStateDead    UserTagEventOutboxState = 4 // 死信
)

// UserTagEventOutbox 表示用户标签得到和失去事件的可靠派发记录。
type UserTagEventOutbox struct {
	ID           int64                   `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement;index:idx_state_next_id,priority:3;index:idx_workflow_state_shard_id,priority:4;index:idx_updated_id,priority:2;comment:主键" json:"id"` // 主键 ID
	WorkflowID   string                  `gorm:"column:workflow_id;type:varchar(80);not null;index:idx_workflow_state_shard_id,priority:1;comment:工作流ID" json:"workflow_id"`                                                                   // 工作流 ID
	EventID      string                  `gorm:"column:event_id;type:varchar(120);not null;uniqueIndex:uk_event_id;comment:事件幂等ID" json:"event_id"`                                                                                            // 事件幂等 ID
	UID          int64                   `gorm:"column:uid;type:bigint;not null;index:idx_uid_tag_action,priority:1;comment:用户 ID" json:"uid"`                                                                                                 // 用户 ID
	TagType      int                     `gorm:"column:tag_type;type:int;not null;index:idx_uid_tag_action,priority:2;comment:标签类型" json:"tag_type"`                                                                                           // 标签类型
	TagSource    int8                    `gorm:"column:tag_source;type:tinyint;not null;default:0;comment:标签来源" json:"tag_source"`                                                                                                             // 标签来源
	Action       string                  `gorm:"column:action;type:varchar(20);not null;index:idx_uid_tag_action,priority:3;comment:动作：gain/lost" json:"action"`                                                                               // 标签得失动作
	SourceNode   string                  `gorm:"column:source_node;type:varchar(40);not null;default:'';comment:来源节点" json:"source_node"`                                                                                                      // 来源节点
	ShardNo      int                     `gorm:"column:shard_no;type:int;not null;default:0;index:idx_workflow_state_shard_id,priority:3;comment:uid取模分片" json:"shard_no"`                                                                     // UID 分片号
	Payload      string                  `gorm:"column:payload;type:varchar(1000);not null;default:'';comment:扩展载荷JSON" json:"payload"`                                                                                                        // 扩展载荷 JSON
	State        UserTagEventOutboxState `gorm:"column:state;type:tinyint;not null;default:0;index:idx_workflow_state_shard_id,priority:2;index:idx_state_next_id,priority:1;comment:处理状态" json:"state"`                                       // 处理状态
	Attempt      int                     `gorm:"column:attempt;type:int;not null;default:0;comment:派发尝试次数" json:"attempt"`                                                                                                                     // 派发尝试次数
	NextRetryAt  *time.Time              `gorm:"column:next_retry_at;type:datetime;index:idx_state_next_id,priority:2;comment:下次重试时间" json:"next_retry_at"`                                                                                    // 下次重试时间
	LockedAt     *time.Time              `gorm:"column:locked_at;type:datetime;comment:最近领取时间" json:"locked_at"`                                                                                                                               // 最近领取时间
	LockedBy     string                  `gorm:"column:locked_by;type:varchar(80);not null;default:'';comment:最近领取者" json:"locked_by"`                                                                                                         // 最近领取者
	LastError    string                  `gorm:"column:last_error;type:varchar(1000);not null;default:'';comment:最近失败原因" json:"last_error"`                                                                                                    // 最近失败原因
	DispatchedAt *time.Time              `gorm:"column:dispatched_at;type:datetime;comment:派发成功时间" json:"dispatched_at"`                                                                                                                       // 派发成功时间
	CreatedAt    time.Time               `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                                                            // 创建时间
	UpdatedAt    time.Time               `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;index:idx_updated_id,priority:1;comment:更新时间" json:"updated_at"`                                // 更新时间
}

// TableName 返回 GORM 使用的用户标签事件 outbox 表名。
func (*UserTagEventOutbox) TableName() string {
	return TableNameUserTagEventOutbox
}
