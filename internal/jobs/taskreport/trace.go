package taskreport

import (
	"context"

	taskstats "admin/internal/task/stats"
)

// RecordReportTrace 写入日报自身的执行统计，便于后续日报继续汇总自身运行情况。
func RecordReportTrace(ctx context.Context, report Report) {
	taskstats.RecordRead(ctx, taskstats.JoinDetailName(TraceNameDailySummary, "task_executions"), int64(report.TotalTaskExecutions))
	taskstats.RecordRead(ctx, taskstats.JoinDetailName(TraceNameDailySummary, "workflow_instances"), int64(report.WorkflowTotal))
	taskstats.RecordError(ctx, taskstats.JoinDetailName(TraceNameDailySummary, "failed_tasks"), int64(report.FailedTaskExecutions))
}

// RecordBuildError 记录日报聚合阶段失败次数。
func RecordBuildError(ctx context.Context) {
	taskstats.RecordError(ctx, taskstats.JoinDetailName(TraceNameDailySummary, taskstats.DetailPartRows), 1)
}

// RecordLarkError 记录日报 Lark 发送阶段失败次数。
func RecordLarkError(ctx context.Context) {
	taskstats.RecordError(ctx, taskstats.JoinDetailName(TraceNameDailySummary, "lark"), 1)
}
