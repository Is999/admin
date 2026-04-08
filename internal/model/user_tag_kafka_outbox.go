package model

import "time"

// TableNameUserTagKafkaOutbox 表示用户标签 Kafka 差异 outbox 表名。
const TableNameUserTagKafkaOutbox = "user_tag_kafka_outbox"

// UserTagKafkaOutboxState 表示用户标签 Kafka outbox 处理状态。
type UserTagKafkaOutboxState int8

const (
	UserTagKafkaOutboxStatePending UserTagKafkaOutboxState = 0 // 待推送
	UserTagKafkaOutboxStateRunning UserTagKafkaOutboxState = 1 // 推送中
	UserTagKafkaOutboxStateDone    UserTagKafkaOutboxState = 2 // 已推送
	UserTagKafkaOutboxStateRetry   UserTagKafkaOutboxState = 3 // 待重试
	UserTagKafkaOutboxStateDead    UserTagKafkaOutboxState = 4 // 死信
)

// UserTagKafkaOutbox 表示用户标签得失事件的可靠投递记录。
type UserTagKafkaOutbox struct {
	ID         int64  `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement;index:idx_state_updated_id,priority:3;comment:主键" json:"id"`                                                            // 主键 ID
	WorkflowID string `gorm:"column:workflow_id;type:varchar(80);not null;uniqueIndex:uk_workflow_uid_tag_action,priority:1;index:idx_workflow_state_shard_uid,priority:1;comment:工作流ID" json:"workflow_id"` // 工作流 ID
	ShardNo    int8   `gorm:"column:shard_no;type:tinyint;not null;default:0;index:idx_workflow_state_shard_uid,priority:3;comment:uid取模分片" json:"shard_no"`                                                 // UID 分片号
	UID        int64  `gorm:"column:uid;type:bigint;not null;uniqueIndex:uk_workflow_uid_tag_action,priority:2;index:idx_workflow_state_shard_uid,priority:4;comment:用户ID" json:"uid"`                       // 用户 ID
	TagType    int    `gorm:"column:tag_type;type:int;not null;uniqueIndex:uk_workflow_uid_tag_action,priority:3;comment:标签类型" json:"tag_type"`                                                              // 标签类型
	ActionType string `gorm:"column:action_type;type:varchar(20);not null;uniqueIndex:uk_workflow_uid_tag_action,priority:4;comment:动作:getTag/lostTag" json:"action_type"`                                   // 标签得失动作
	Payload    string `gorm:"column:payload;type:varchar(500);not null;default:'';comment:Kafka消息JSON" json:"payload"`                                                                                       // Kafka 消息 JSON

	State     UserTagKafkaOutboxState `gorm:"column:state;type:tinyint;not null;default:0;index:idx_workflow_state_shard_uid,priority:2;index:idx_state_updated_id,priority:1;comment:状态" json:"state"`            // 处理状态
	Attempt   int                     `gorm:"column:attempt;type:int;not null;default:0;comment:推送尝试次数" json:"attempt"`                                                                                            // 推送尝试次数
	LastError string                  `gorm:"column:last_error;type:varchar(1000);not null;default:'';comment:最近失败原因" json:"last_error"`                                                                           // 最近失败原因
	SentAt    *time.Time              `gorm:"column:sent_at;type:datetime;comment:推送成功时间" json:"sent_at"`                                                                                                          // 推送成功时间
	CreatedAt time.Time               `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                                   // 创建时间
	UpdatedAt time.Time               `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;index:idx_state_updated_id,priority:2;comment:更新时间" json:"updated_at"` // 更新时间
}

// TableName 返回 GORM 使用的用户标签 Kafka outbox 表名。
func (*UserTagKafkaOutbox) TableName() string {
	return TableNameUserTagKafkaOutbox
}
