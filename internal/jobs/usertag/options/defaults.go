package options

import "admin/internal/config"

const (
	// defaultShardTotal 是用户标签默认分片数，和 uid%10 分表口径保持一致。
	defaultShardTotal = 10
	// defaultBatchSize 是默认游标批次大小。
	defaultBatchSize = 2000
	// defaultWorkerCount 是默认节点内部 worker 数。
	defaultWorkerCount = 4
	// defaultDiffBatchSize 是默认标签差异处理批次大小。
	defaultDiffBatchSize = 10000
	// defaultEventBatchSize 是默认事件 outbox 派发批次大小。
	defaultEventBatchSize = 500
)

// Defaults 定义 usertag 运行默认值。
type Defaults struct {
	ShardTotal        int  // 工作流默认分片数
	RuntimeShardTotal int  // 运行期 UID 索引分片数
	BatchSize         int  // 游标扫描批次大小
	WorkerCount       int  // 节点内部 worker 数
	DiffBatchSize     int  // 标签差异处理批次大小
	EventBatchSize    int  // 事件 outbox 派发批次大小
	EventHookEnabled  bool // 是否启用用户标签事件 hook
}

// NewDefaults 根据全局用户标签配置构造 默认值。
func NewDefaults(cfg config.UserTagConfig) Defaults {
	return Defaults{
		ShardTotal:        boundedPositiveOr(cfg.DefaultShardTotal, defaultShardTotal, MaxShardTotal),
		RuntimeShardTotal: boundedPositiveOr(cfg.RuntimeShardTotal, defaultShardTotal, MaxShardTotal),
		BatchSize:         boundedPositiveOr(cfg.DefaultBatchSize, defaultBatchSize, MaxBatchSize),
		WorkerCount:       boundedPositiveOr(cfg.DefaultWorkerCount, defaultWorkerCount, MaxWorkerCount),
		DiffBatchSize:     boundedPositiveOr(cfg.DiffBatchSize, defaultDiffBatchSize, MaxBatchSize),
		EventBatchSize:    boundedPositiveOr(cfg.EventBatchSize, defaultEventBatchSize, MaxBatchSize),
		EventHookEnabled:  cfg.EventHookEnabled,
	}
}

// positiveOr 返回正数配置，非正数时使用兜底值。
func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// boundedPositiveOr 返回正数配置并限制最大值。
func boundedPositiveOr(value, fallback, max int) int {
	value = positiveOr(value, fallback)
	if value > max {
		return max
	}
	return value
}
