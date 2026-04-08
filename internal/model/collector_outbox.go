package model

import "time"

// TableNameCollectorOutbox 表示通用收集器 outbox 表名。
const TableNameCollectorOutbox = "collector_outbox"

// CollectorOutboxState 表示通用收集器事件处理状态。
type CollectorOutboxState int

const (
	CollectorOutboxStatePending CollectorOutboxState = 0 // 待处理
	CollectorOutboxStateRunning CollectorOutboxState = 1 // 处理中
	CollectorOutboxStateDone    CollectorOutboxState = 2 // 已完成
	CollectorOutboxStateRetry   CollectorOutboxState = 3 // 待重试
	CollectorOutboxStateDead    CollectorOutboxState = 4 // 死信
)

// CollectorOutbox 表示通用收集器可靠投递 outbox 模型。
// 事件只保存结构化 Payload，不保存业务 SQL；最终如何聚合、upsert、落表由注册的 Processor 决定。
type CollectorOutbox struct {
	ID int64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true" json:"id"` // 主键 ID

	EventID      string               `gorm:"column:event_id;type:varchar(64);not null;uniqueIndex:uk_event_id;comment:事件ID(幂等键)" json:"eventId"`                                                                                          // 事件唯一 ID，也是 outbox 幂等键
	BizType      string               `gorm:"column:biz_type;type:varchar(100);not null;index:idx_biz_state_next,priority:1;comment:业务类型" json:"bizType"`                                                                                  // 业务类型，用于路由到对应 Processor
	PartitionKey string               `gorm:"column:partition_key;type:varchar(128);not null;index:idx_partition_state_next,priority:1;comment:分区Key(冲突域)" json:"partitionKey"`                                                            // 分区键/聚合键，用于业务方控制冲突域
	Payload      string               `gorm:"column:payload;type:text;not null;comment:事件负载(JSON)" json:"payload"`                                                                                                                         // 结构化事件负载 JSON
	Transport    string               `gorm:"column:transport;type:varchar(16);not null;default:db;comment:来源/载体:kafka|redis|db" json:"transport"`                                                                                         // 事件来源载体
	State        CollectorOutboxState `gorm:"column:state;type:tinyint unsigned;not null;default:0;index:idx_biz_state_next,priority:2;index:idx_state_next,priority:1;index:idx_partition_state_next,priority:2;comment:状态" json:"state"` // 当前处理状态
	Attempt      int                  `gorm:"column:attempt;type:tinyint unsigned;not null;default:0;comment:失败重试次数" json:"attempt"`                                                                                                       // 已失败重试次数
	NextRunAt    time.Time            `gorm:"column:next_run_at;type:timestamp;not null;index:idx_biz_state_next,priority:3;index:idx_state_next,priority:2;index:idx_partition_state_next,priority:3;comment:下次可处理时间" json:"nextRunAt"`   // 下次允许处理时间
	StartedAt    *time.Time           `gorm:"column:started_at;type:timestamp;null;index:idx_state_started,priority:2;comment:开始处理时间" json:"startedAt,omitempty"`                                                                          // 最近一次开始处理时间
	FinishedAt   *time.Time           `gorm:"column:finished_at;type:timestamp;null;comment:结束时间(完成/死信)" json:"finishedAt,omitempty"`                                                                                                      // 完成或死信时间
	LastError    string               `gorm:"column:last_error;type:varchar(1000);not null;default:'';comment:最近一次失败原因" json:"lastError"`                                                                                                  // 最近一次失败原因摘要
	CreatedAt    time.Time            `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                                                                           // 创建时间
	UpdatedAt    time.Time            `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"`                                                                                           // 更新时间
}

// TableName 返回 GORM 使用的表名。
func (*CollectorOutbox) TableName() string {
	return TableNameCollectorOutbox
}
