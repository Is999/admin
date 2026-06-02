package types

// WorkflowPayload 是 usertag 工作流节点之间传递的运行负载。
// 该结构刻意不包含 filterHash，多维筛选结果不能作为标签计算来源。
type WorkflowPayload struct {
	WorkflowID  string   `json:"workflow_id"`            // 工作流 ID，用于日志链路、运行期表和事件幂等
	Mode        string   `json:"mode"`                   // 运行模式：full/delta/targeted/recalculate
	Node        string   `json:"node,omitempty"`         // 当前节点名称，入口层未传时由 workflow 注入
	Targets     []string `json:"targets,omitempty"`      // 原始目标参数，兼容周期任务配置
	TagTypes    []int    `json:"tag_types,omitempty"`    // 指定标签类型集合
	UIDs        []int64  `json:"uids,omitempty"`         // 显式指定 UID 集合，仅 targeted 使用
	ShardIndex  int      `json:"shard_index,omitempty"`  // 当前分片下标
	ShardTotal  int      `json:"shard_total,omitempty"`  // 当前分片总数
	BatchSize   int      `json:"batch_size,omitempty"`   // 游标扫描批次大小
	WorkerCount int      `json:"worker_count,omitempty"` // 节点内部 worker 数
	DryRun      bool     `json:"dry_run,omitempty"`      // 是否只计算不写正式结果
}

// RuntimeOptions 是 usertag 完成参数解析后的运行选项。
type RuntimeOptions struct {
	WorkflowID       string  // 工作流 ID
	Mode             string  // 运行模式
	TagTypes         []int   // 需要计算的标签类型集合
	UIDs             []int64 // 显式 UID 集合，不能由 filterHash 推导
	ShardIndex       int     // 当前分片下标
	ShardTotal       int     // 当前分片总数
	BatchSize        int     // 游标扫描批次大小
	WorkerCount      int     // 节点内部 worker 数
	DryRun           bool    // 是否只计算不落库
	EventHookEnabled bool    // 是否允许派发标签得失 hook
}

// TagRecord 表示最终用户标签行的轻量模型。
type TagRecord struct {
	UID     int64 // 用户 UID
	TagType int   // 标签类型
	Source  int   // 标签来源：系统或人工
}

// TagChange 表示标签得失差异事件。
// 事件只允许在最终标签态完成后进入 outbox，不能在评估阶段提前派发。
type TagChange struct {
	EventID    string // 事件幂等 ID
	WorkflowID string // 工作流 ID
	UID        int64  // 用户 UID
	TagType    int    // 标签类型
	Action     string // 变更动作：gain/lost
	Source     int    // 标签来源：系统或人工
	Payload    string // 扩展载荷 JSON
}
