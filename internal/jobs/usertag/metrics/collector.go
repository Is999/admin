package metrics

import (
	"context"
	"time"
)

// QueryMetric 记录一次关键查询的观测数据。
type QueryMetric struct {
	WorkflowID string        // 工作流 ID
	Mode       string        // 运行模式
	Node       string        // 节点名称
	Shard      int           // 分片下标
	Batch      int           // 批次序号
	Source     string        // 数据源类型
	Name       string        // 查询名称
	Rows       int64         // 返回或影响行数
	Duration   time.Duration // 查询耗时
}

// Collector 定义 usertag 指标采集接口。
type Collector interface {
	ObserveQuery(ctx context.Context, metric QueryMetric) // 记录查询指标
}

// NoopCollector 是默认空实现，避免未接入指标系统时污染业务逻辑。
type NoopCollector struct{}

// ObserveQuery 忽略查询指标。
func (NoopCollector) ObserveQuery(context.Context, QueryMetric) {}
