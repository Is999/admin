package cdcx

import (
	"encoding/json"
	stderrors "errors"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// ConsumerStatus 描述 CDC 消费器运行快照。
type ConsumerStatus struct {
	Enabled          bool                  `json:"enabled"`           // 是否启用 CDC
	Running          bool                  `json:"running"`           // 当前是否运行中
	GroupID          string                `json:"group_id"`          // Kafka 消费组 ID
	ConsumerName     string                `json:"consumer_name"`     // 消费者名称
	StartedAt        time.Time             `json:"started_at"`        // 最近启动时间
	StoppedAt        time.Time             `json:"stopped_at"`        // 最近停止时间
	RegisteredTables []string              `json:"registered_tables"` // 已注册表处理器
	Topics           map[string]TopicStats `json:"topics"`            // Topic 维度状态
}

// TopicStats 描述单个 Topic 的消费状态。
type TopicStats struct {
	Topic              string    `json:"topic"`                 // Kafka Topic
	Fetched            int64     `json:"fetched"`               // 已拉取消息数
	Processed          int64     `json:"processed"`             // 已成功处理消息数
	Skipped            int64     `json:"skipped"`               // 已主动跳过消息数
	Failed             int64     `json:"failed"`                // 处理失败次数
	DeadLettered       int64     `json:"dead_lettered"`         // 已写入死信消息数
	LastPartition      int       `json:"last_partition"`        // 最近处理分区
	LastOffset         int64     `json:"last_offset"`           // 最近处理 offset
	LastError          string    `json:"last_error"`            // 最近错误摘要
	LastFetchedAt      time.Time `json:"last_fetched_at"`       // 最近拉取时间
	LastProcessedAt    time.Time `json:"last_processed_at"`     // 最近成功时间
	LastSkippedAt      time.Time `json:"last_skipped_at"`       // 最近跳过时间
	LastFailedAt       time.Time `json:"last_failed_at"`        // 最近失败时间
	LastDeadLetteredAt time.Time `json:"last_dead_lettered_at"` // 最近死信时间
}

// DeadLetterMessage 是写入死信 Topic 的稳定 JSON 结构。
type DeadLetterMessage struct {
	SourceTopic     string    `json:"source_topic"`     // 原 Topic
	SourcePartition int       `json:"source_partition"` // 原分区
	SourceOffset    int64     `json:"source_offset"`    // 原 offset
	Table           string    `json:"table"`            // 表路由键
	Attempt         int       `json:"attempt"`          // 失败次数
	Error           string    `json:"error"`            // 错误摘要
	Key             []byte    `json:"key"`              // 原 Kafka key
	Value           []byte    `json:"value"`            // 原 Kafka value
	CreatedAt       time.Time `json:"created_at"`       // 死信生成时间
}

// DecodeDeadLetterMessage 解析死信 Topic 消息，供后续重放任务复用。
func DecodeDeadLetterMessage(value []byte) (DeadLetterMessage, error) {
	if len(value) == 0 {
		return DeadLetterMessage{}, errors.Errorf("CDC 死信消息为空")
	}
	var msg DeadLetterMessage
	if err := json.Unmarshal(value, &msg); err != nil {
		return DeadLetterMessage{}, errors.Wrap(err, "解析 CDC 死信消息失败")
	}
	msg.SourceTopic = strings.TrimSpace(msg.SourceTopic)
	msg.Table = normalizeTable(msg.Table)
	msg.Error = strings.TrimSpace(msg.Error)
	if msg.SourceTopic == "" {
		return DeadLetterMessage{}, errors.Errorf("CDC 死信消息缺少 source_topic")
	}
	return msg, nil
}

// Skip 返回主动跳过事件的错误，Consumer 会提交 offset 但不写死信。
func Skip(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "skip"
	}
	return skipError{reason: reason}
}

// IsSkip 判断错误是否为主动跳过。
func IsSkip(err error) bool {
	if err == nil {
		return false
	}
	var target skipError
	return stderrors.As(err, &target)
}

// skipError 表示 Processor 主动跳过当前事件。
type skipError struct {
	reason string // 跳过原因
}

// Error 实现 error 接口。
func (e skipError) Error() string {
	return e.reason
}
