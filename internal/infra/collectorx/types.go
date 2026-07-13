package collectorx

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	collectorRuntimeChannelKafka = "kafka" // Kafka 通道：正常链路唯一投递和消费通道
)

const (
	// RuntimeAlertKindEnqueueFailed 表示 Collector Kafka 投递失败。
	RuntimeAlertKindEnqueueFailed = "collector_enqueue_failed"
	// RuntimeAlertKindWorkerFailed 表示 Collector 后台消费链路失败。
	RuntimeAlertKindWorkerFailed = "collector_worker_failed"
	// RuntimeAlertKindInvalidEvent 表示 Collector 收到不可恢复的无效事件。
	RuntimeAlertKindInvalidEvent = "collector_invalid_event"
	// RuntimeAlertKindDeadEvent 表示 Collector 事件超过重试次数进入死信。
	RuntimeAlertKindDeadEvent = "collector_dead_event"
)

// Event 表示业务投递到通用收集器的一条结构化数据。
// 注意：Payload 只保存业务数据，不允许塞入 SQL 语句；具体如何聚合、累加、落库由对应 bizType 的 Processor 决定。
type Event struct {
	EventID      string          `json:"eventId"`      // 事件唯一 ID，最多 64 字节
	BizType      string          `json:"bizType"`      // 业务类型，最多 100 字节
	PartitionKey string          `json:"partitionKey"` // 分区键/聚合键，最多 128 字节
	Payload      json.RawMessage `json:"payload"`      // 有效 UTF-8 JSON 负载，最多 60 KiB
}

// RuntimeAlert 描述 Collector 后台投递、消费、失败账本和死信链路中的运行异常。
type RuntimeAlert struct {
	Kind       string    // 异常类型，用于上层告警指纹和排障归类
	Title      string    // 告警标题
	Status     string    // 当前处理状态
	Component  string    // 发生异常的组件
	Operation  string    // 发生异常的运行操作
	BizType    string    // 关联业务类型
	Channel    string    // 关联通道，当前正常链路为 kafka
	UniqueKey  string    // 关联事件、Topic 或消费组等稳定键
	Reason     string    // 异常原因摘要
	Advice     string    // 处理建议
	Count      int       // 本次异常影响数量
	OccurredAt time.Time // 发现异常的时间
}

// AlertHook 接收 Collector 后台运行异常；上层负责限频和外部通知。
type AlertHook func(ctx context.Context, alert RuntimeAlert)

// ProcessResult 表示批量处理器对单个事件的处理结果。
// 当 Processor.ProcessBatch 返回空结果且 error=nil 时，框架认为整批事件全部成功。
// 当需要表达部分成功/部分失败时，Processor 必须为每个事件返回一条对应的 ProcessResult。
type ProcessResult struct {
	EventID string // 事件唯一 ID，必须对应输入事件中的 EventID
	Success bool   // 是否处理成功；false 时框架会按重试策略回写
	Error   string // 失败原因摘要，Success=false 时用于 last_error
}

// Processor 定义业务批量消费接口。
// Processor 决定单批数据的落库、聚合、upsert 或转投递方式；Collector 的 Redis token 只能减少重复执行。
// 生产持久化必须使用 EventID 对应的唯一业务键或幂等 upsert，不能把 Redis 状态视为 exactly-once 保证。
// 实现必须把 context 传给所有下游调用并及时响应取消；框架会同步等待返回，不会遗留孤儿执行。
type Processor interface {
	ProcessBatch(context.Context, []Event) ([]ProcessResult, error)
}

// ProcessorFunc 允许业务方用普通函数快速注册批量处理器。
type ProcessorFunc func(context.Context, []Event) ([]ProcessResult, error)

// ProcessBatch 执行批量消费函数。
func (f ProcessorFunc) ProcessBatch(ctx context.Context, events []Event) ([]ProcessResult, error) {
	return f(ctx, events)
}

// normalizeRuntimeAlert 清理告警字段，保证上层通知文本稳定。
func normalizeRuntimeAlert(alert RuntimeAlert) RuntimeAlert {
	alert.Kind = strings.TrimSpace(alert.Kind)
	alert.Title = strings.TrimSpace(alert.Title)
	alert.Status = strings.TrimSpace(alert.Status)
	alert.Component = strings.TrimSpace(alert.Component)
	alert.Operation = strings.TrimSpace(alert.Operation)
	alert.BizType = strings.TrimSpace(alert.BizType)
	alert.Channel = strings.TrimSpace(alert.Channel)
	alert.UniqueKey = strings.TrimSpace(alert.UniqueKey)
	alert.Reason = strings.TrimSpace(alert.Reason)
	alert.Advice = strings.TrimSpace(alert.Advice)
	if alert.OccurredAt.IsZero() {
		alert.OccurredAt = time.Now()
	}
	return alert
}
