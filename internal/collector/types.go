package collector

import (
	"context"
	"encoding/json"
)

const (
	collectorTransportAuto    = "auto"    // 自动选择载体：优先 Kafka，其次 Redis，最终 DB outbox 兜底
	collectorTransportKafka   = "kafka"   // Kafka 载体：用于高吞吐异步投递，失败后按配置继续降级
	collectorTransportRedis   = "redis"   // Redis Stream 载体：用于轻量异步投递，失败后写入 DB outbox
	collectorTransportDB      = "db"      // DB outbox 载体：最终可靠兜底，也是落库后的来源标识
	collectorTransportUnknown = "unknown" // 未知来源标签：用于空 transport 的指标和聚合兜底
	collectorTransportMixed   = "mixed"   // 混合来源标签：用于批量落库时同批次包含多种来源的指标聚合
)

// Event 表示业务投递到通用收集器的一条结构化数据。
// 注意：Payload 只保存业务数据，不允许塞入 SQL 语句；具体如何聚合、累加、落库由对应 bizType 的 Processor 决定。
type Event struct {
	EventID      string          `json:"eventId"`      // 事件唯一 ID，也是幂等键；为空时 Enqueue 会自动生成
	BizType      string          `json:"bizType"`      // 业务类型，用于路由到对应批量处理器
	PartitionKey string          `json:"partitionKey"` // 分区键/聚合键，用于业务方控制冲突域或聚合维度
	Payload      json.RawMessage `json:"payload"`      // 业务数据负载，必须是结构化 JSON 数据
}

// ProcessResult 表示批量处理器对单个事件的处理结果。
// 当 Processor.ProcessBatch 返回空结果且 error=nil 时，框架认为整批事件全部成功。
// 当需要表达部分成功/部分失败时，Processor 必须为每个事件返回一条对应的 ProcessResult。
type ProcessResult struct {
	EventID string // 事件唯一 ID，必须对应输入事件中的 EventID
	Success bool   // 是否处理成功；false 时框架会按重试策略回写
	Error   string // 失败原因摘要，Success=false 时用于 last_error
}

// Processor 定义业务批量消费接口。
// Processor 决定单批数据的落库、聚合、upsert 或转投递方式。
type Processor interface {
	ProcessBatch(context.Context, []Event) ([]ProcessResult, error)
}

// ProcessorFunc 允许业务方用普通函数快速注册批量处理器。
type ProcessorFunc func(context.Context, []Event) ([]ProcessResult, error)

// ProcessBatch 执行批量消费函数。
func (f ProcessorFunc) ProcessBatch(ctx context.Context, events []Event) ([]ProcessResult, error) {
	return f(ctx, events)
}
