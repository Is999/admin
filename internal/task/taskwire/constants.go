package taskwire

const (
	// QueueCritical 是高优先级业务队列名。
	QueueCritical = "critical"
	// QueueDefault 是默认业务队列名。
	QueueDefault = "default"
	// QueueMaintenance 是运维/缓存刷新等后台队列名。
	QueueMaintenance = "maintenance"

	// TypeWorkflowTrigger 是触发工作流实例的入口任务类型。
	TypeWorkflowTrigger = "workflow:trigger"
	// TypeWorkflowNoop 是 DAG 中用于收尾/汇聚的空任务类型。
	TypeWorkflowNoop = "workflow:noop"
)

const (
	// WorkflowSourceAPI 表示由管理接口手动触发。
	WorkflowSourceAPI = "api"
	// WorkflowSourcePeriodic 表示由周期调度触发。
	WorkflowSourcePeriodic = "periodic"
	// WorkflowSourceInternal 表示由应用内部逻辑触发。
	WorkflowSourceInternal = "internal"
)

const (
	// HeaderTaskSource 是任务触发来源头。
	HeaderTaskSource = "x-app-task-source"
	// HeaderPeriodicName 是周期任务原始名称头。
	HeaderPeriodicName = "x-app-periodic-name"
	// HeaderWorkflowID 是工作流实例 ID 头。
	HeaderWorkflowID = "x-app-workflow-id"
	// HeaderWorkflowName 是工作流名称头。
	HeaderWorkflowName = "x-app-workflow-name"
	// HeaderWorkflowNode 是工作流节点名称头。
	HeaderWorkflowNode = "x-app-workflow-node"
)
