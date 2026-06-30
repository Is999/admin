package taskreport

const (
	// WorkflowNameDailySummary 表示周期任务和工作流运行日报工作流名称。
	WorkflowNameDailySummary = "task_report.daily_summary"
	// TaskTypeDailySummary 表示发送任务运行日报的任务类型。
	TaskTypeDailySummary = "task_report:daily_summary"
	// NodeDailySummary 表示日报工作流中的发送节点。
	NodeDailySummary = "send_summary"
	// TraceNameDailySummary 表示日报任务处理量明细前缀。
	TraceNameDailySummary = WorkflowNameDailySummary
)

// TaskPayload 描述任务运行日报节点负载。
type TaskPayload struct {
	WorkflowID   string `json:"workflowId,omitempty"`   // 工作流实例 ID
	WorkflowName string `json:"workflowName,omitempty"` // 工作流名称
	WorkflowNode string `json:"workflowNode,omitempty"` // 当前工作流节点
	ShardIndex   int    `json:"shardIndex,omitempty"`   // 当前分片下标
	ShardTotal   int    `json:"shardTotal,omitempty"`   // 当前分片总数
	WindowStart  string `json:"windowStart,omitempty"`  // 手动覆盖统计窗口开始时间，RFC3339
	WindowEnd    string `json:"windowEnd,omitempty"`    // 手动覆盖统计窗口结束时间，RFC3339
}
