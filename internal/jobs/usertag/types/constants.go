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
	// NodeBusinessHook 表示扩展占位节点。
	NodeBusinessHook = "business_hook"
	// NodeFinalize 表示最终结果提交节点。
	NodeFinalize = "finalize"
	// NodeSyncKafka 表示 full 同步快照重建节点，名称保留兼容历史工作流。
	NodeSyncKafka = "sync_kafka"
	// NodeRuntimeCleanup 表示用户标签运行期辅助表清理节点。
	NodeRuntimeCleanup = "runtime_cleanup"
	// NodeKafkaOutboxRetryScan 表示用户标签 Kafka outbox 异常扫描重推节点。
	NodeKafkaOutboxRetryScan = "kafka_outbox_retry_scan"
)

const (
	// TaskTypeUserTagKafkaOutboxRetry 表示用户标签 outbox 独立重试任务。
	TaskTypeUserTagKafkaOutboxRetry = "user_tag:kafka_outbox_retry"
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
	// SourceKafkaOutbox 表示 Kafka 分片 outbox 数据源。
	SourceKafkaOutbox = "kafka_outbox"
)
