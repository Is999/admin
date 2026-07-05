package model

import "time"

// TableNameCollectorFailedEvent 表示通用收集器失败事件表名。
const TableNameCollectorFailedEvent = "collector_failed_event"

// CollectorFailedEventState 表示通用收集器失败事件重试状态。
type CollectorFailedEventState int

const (
	CollectorFailedEventStatePending CollectorFailedEventState = 0 // 待重试
	CollectorFailedEventStateRunning CollectorFailedEventState = 1 // 重试中
	CollectorFailedEventStateDone    CollectorFailedEventState = 2 // 重试成功
	CollectorFailedEventStateRetry   CollectorFailedEventState = 3 // 延迟重试
	CollectorFailedEventStateDead    CollectorFailedEventState = 4 // 死信
)

// CollectorFailedEvent 表示通用收集器失败事件模型。
// 正常 Kafka 消费成功不写入该表，只有处理失败、坏消息和死信进入这里。
type CollectorFailedEvent struct {
	// ID 主键 ID。
	ID int64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true" json:"id"`
	// EventID 事件唯一 ID，在同 bizType 内作为失败账本幂等键。
	EventID string `gorm:"column:event_id;type:varchar(64);not null;uniqueIndex:uk_biz_event_id,priority:2;comment:事件ID(同业务幂等键)" json:"eventId"`
	// BizType 业务类型，用于路由到对应 Processor。
	BizType string `gorm:"column:biz_type;type:varchar(100);not null;uniqueIndex:uk_biz_event_id,priority:1;index:idx_biz_state_next,priority:1;comment:业务类型" json:"bizType"`
	// PartitionKey 分区键/聚合键，用于业务方控制冲突域。
	PartitionKey string `gorm:"column:partition_key;type:varchar(128);not null;index:idx_partition_state_next,priority:1;comment:分区Key(冲突域)" json:"partitionKey"`
	// Payload 结构化事件负载 JSON。
	Payload string `gorm:"column:payload;type:text;not null;comment:事件负载(JSON)" json:"payload"`
	// State 当前重试状态。
	State CollectorFailedEventState `gorm:"column:state;type:tinyint unsigned;not null;default:0;index:idx_biz_state_next,priority:2;index:idx_state_next,priority:1;index:idx_partition_state_next,priority:2;index:idx_state_finished;index:idx_state_updated;comment:状态" json:"state"`
	// Attempt 已失败重试次数。
	Attempt int `gorm:"column:attempt;type:tinyint unsigned;not null;default:0;comment:失败重试次数" json:"attempt"`
	// NextRunAt 下次允许处理时间。
	NextRunAt time.Time `gorm:"column:next_run_at;type:datetime;not null;index:idx_biz_state_next,priority:3;index:idx_state_next,priority:2;index:idx_partition_state_next,priority:3;comment:下次可处理时间" json:"nextRunAt"`
	// StartedAt 最近一次开始处理时间。
	StartedAt *time.Time `gorm:"column:started_at;type:datetime;null;index:idx_state_started,priority:2;comment:开始处理时间" json:"startedAt,omitempty"`
	// FinishedAt 完成或死信时间。
	FinishedAt *time.Time `gorm:"column:finished_at;type:datetime;null;index:idx_state_finished;comment:结束时间(完成/死信)" json:"finishedAt,omitempty"`
	// LastError 最近一次失败原因摘要。
	LastError string `gorm:"column:last_error;type:varchar(1000);not null;default:'';comment:最近一次失败原因" json:"lastError"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;index:idx_state_updated;comment:更新时间" json:"updatedAt"`
}

// TableName 返回 GORM 使用的表名。
func (*CollectorFailedEvent) TableName() string {
	return TableNameCollectorFailedEvent
}
