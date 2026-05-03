package types

const (
	// ModeFull 表示全量重建模式。
	ModeFull = "full"
	// ModeDelta 表示增量刷新模式。
	ModeDelta = "delta"
	// ModeTargeted 表示显式 UID 定向补算模式。
	ModeTargeted = "targeted"
	// ModeRecalculate 表示指定标签全量重算模式。
	ModeRecalculate = "recalculate"
)

const (
	// NodePrepare 表示工作流准备节点。
	NodePrepare = "prepare"
	// NodeCollectScope 表示通用候选范围收集节点。
	NodeCollectScope = "collect_scope"
	// NodeEvaluateTags 表示通用标签规则评估节点。
	NodeEvaluateTags = "evaluate_tags"
	// NodeResolveChanges 表示标签得失差异解析节点。
	NodeResolveChanges = "resolve_changes"
	// NodePersistResults 表示最终标签结果与事件 outbox 写入节点。
	NodePersistResults = "persist_results"
	// NodeFinalize 表示最终结果提交节点。
	NodeFinalize = "finalize"
	// NodeDispatchHooks 表示标签得失事件 hook 派发节点。
	NodeDispatchHooks = "dispatch_hooks"
	// NodeRuntimeCleanup 表示用户标签运行期辅助表清理节点。
	NodeRuntimeCleanup = "runtime_cleanup"
	// NodeEventOutboxRetryScan 表示用户标签事件 outbox 异常扫描重派节点。
	NodeEventOutboxRetryScan = "event_outbox_retry_scan"
)

const (
	// TaskTypeUserTagEventOutboxRetry 表示用户标签事件 outbox 独立重试任务。
	TaskTypeUserTagEventOutboxRetry = "user_tag:event_outbox_retry"
	// TaskTypeUserTagRuntimeCleanup 表示用户标签运行期辅助表独立清理任务。
	TaskTypeUserTagRuntimeCleanup = "user_tag:runtime_cleanup"
)

const (
	// SourceMySQL 表示 MySQL 数据源。
	SourceMySQL = "mysql"
	// SourceClickHouse 表示 ClickHouse 数据源。
	SourceClickHouse = "clickhouse"
	// SourceRedis 表示 Redis 数据源。
	SourceRedis = "redis"
	// SourceEventOutbox 表示用户标签事件 outbox 数据源。
	SourceEventOutbox = "event_outbox"
)

const (
	// ChangeActionGain 表示用户获得标签。
	ChangeActionGain = "gain"
	// ChangeActionLost 表示用户失去标签。
	ChangeActionLost = "lost"
)
