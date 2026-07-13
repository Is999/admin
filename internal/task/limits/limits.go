// Package limits 统一任务投递和工作流覆盖参数的生产硬上限。
package limits

const (
	// MaxRetry 与 Asynq 默认最大重试边界一致，避免人工覆盖制造重试风暴。
	MaxRetry = 25
	// MaxTimeoutSeconds 允许现有两小时任务并把单次执行硬限制在一天内。
	MaxTimeoutSeconds = 24 * 60 * 60
)
