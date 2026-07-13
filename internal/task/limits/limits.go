// Package limits 统一任务投递和工作流覆盖参数的生产硬上限。
package limits

const (
	// MinPeriodicEverySeconds 限制固定间隔周期任务的最小秒数，避免高频误配置打爆队列。
	MinPeriodicEverySeconds = 5
	// MaxRetry 与 Asynq 默认最大重试边界一致，避免人工覆盖制造重试风暴。
	MaxRetry = 25
	// MaxShardTotal 限制单个工作流可生成的分片任务数，避免误配置放大队列和状态查询负载。
	MaxShardTotal = 128
	// MaxTimeoutSeconds 允许现有两小时任务并把单次执行硬限制在一天内。
	MaxTimeoutSeconds = 24 * 60 * 60
)
