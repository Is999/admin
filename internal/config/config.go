package config

import "github.com/zeromicro/go-zero/rest"

// MySQLConfig 定义关系数据库连接与连接池参数。
type MySQLConfig struct {
	WriteDataSource string   `json:"write_data_source"`          // 写库 DSN（必填）
	ReadDataSources []string `json:"read_data_sources,optional"` // 读库 DSN 列表（启用读写分离）
	MaxOpenConns    int      `json:"max_open_conns"`             // 最大打开连接数
	MaxIdleConns    int      `json:"max_idle_conns"`             // 最大空闲连接数
	ConnMaxLifetime int      `json:"conn_max_lifetime"`          // 连接最大生命周期，单位：秒
	Debug           bool     `json:"debug"`                      // 是否开启 GORM 调试模式
}

// SiteMySQLConfig 定义可选命名扩展库配置。
// 默认只使用顶层 mysql；需要拆库时按 site_mysql.<name> 增加连接。
type SiteMySQLConfig map[string]MySQLConfig

// RedisConfig 定义 Redis 连接与连接池参数。
type RedisConfig struct {
	Type                  string            `json:"type,optional"`                     // Redis 模式：single 或 cluster
	Addrs                 []string          `json:"addrs"`                             // Redis 地址列表（1个=单机，多个=集群）
	AddrMap               map[string]string `json:"addr_map,optional"`                 // 集群地址改写表
	Password              string            `json:"password"`                          // 密码
	DB                    int               `json:"db"`                                // 数据库编号（仅单机/主从有效，集群模式会忽略）
	PoolSize              int               `json:"pool_size"`                         // 连接池大小
	TLS                   bool              `json:"tls,optional"`                      // 是否启用 TLS 连接云 Redis
	TLSInsecureSkipVerify bool              `json:"tls_insecure_skip_verify,optional"` // 是否跳过 TLS 证书校验（仅测试/代理场景使用）
}

// SnowflakeConfig 定义基于 bwmarrin/snowflake 的分布式雪花 ID 配置。
type SnowflakeConfig struct {
	WorkerID *int64 `json:"worker_id,optional"` // 当前实例全局唯一 worker_id/node_id，范围 0-1023
}

// UserConfig 定义业务用户写入和后续拆表路由配置。
type UserConfig struct {
	RouteShardCount int `json:"route_shard_count,optional,default=1"` // 新增业务用户默认物理表数量：1/10/100/1000
}

// SecuritySecretKeyVersionConfig 定义配置文件中的单个秘钥版本材料。
// 当前 app_id 命中时优先使用这里的版本材料。
type SecuritySecretKeyVersionConfig struct {
	KeyVersion             string `json:"key_version"`                         // 秘钥版本号
	AESKey                 string `json:"aes_key,optional"`                    // AES KEY 明文；为空时读取 aes_key_ref 指向的文件
	AESKeyRef              string `json:"aes_key_ref,optional"`                // AES KEY 文件路径
	AESIV                  string `json:"aes_iv,optional"`                     // AES IV 明文；为空时读取 aes_iv_ref 指向的文件
	AESIVRef               string `json:"aes_iv_ref,optional"`                 // AES IV 文件路径
	RSAPublicKeyUser       string `json:"rsa_public_key_user,optional"`        // 用户 RSA 公钥 PEM 文本
	RSAPublicKeyUserRef    string `json:"rsa_public_key_user_ref,optional"`    // 用户 RSA 公钥 PEM 文件绝对路径
	RSAPublicKeyServer     string `json:"rsa_public_key_server,optional"`      // 服务端 RSA 公钥 PEM 文本；为空时由私钥派生
	RSAPublicKeyServerRef  string `json:"rsa_public_key_server_ref,optional"`  // 服务端 RSA 公钥文件路径；为空时由私钥派生
	RSAPrivateKeyServer    string `json:"rsa_private_key_server,optional"`     // 服务端 RSA 私钥 PEM 文本
	RSAPrivateKeyServerRef string `json:"rsa_private_key_server_ref,optional"` // 服务端 RSA 私钥 PEM 文件绝对路径
	Remark                 string `json:"remark,optional"`                     // 版本备注，便于运维识别当前配置来源
}

// SecuritySecretKeyConfig 定义当前站点 app_id 的签名验签和加解密秘钥配置。
// 该配置只服务顶层 app_id 对应的应用；其它 AppID 仍沿用数据库与缓存中的 secret_key 配置。
type SecuritySecretKeyConfig struct {
	KeyVersion             string                           `json:"key_version,optional"`                // 单版本秘钥版本号
	AESKey                 string                           `json:"aes_key,optional"`                    // 单版本 AES KEY 明文；为空时读取 aes_key_ref 指向的文件
	AESKeyRef              string                           `json:"aes_key_ref,optional"`                // 单版本 AES KEY 文件绝对路径
	AESIV                  string                           `json:"aes_iv,optional"`                     // 单版本 AES IV 明文；为空时读取 aes_iv_ref 指向的文件
	AESIVRef               string                           `json:"aes_iv_ref,optional"`                 // 单版本 AES IV 文件绝对路径
	RSAPublicKeyUser       string                           `json:"rsa_public_key_user,optional"`        // 单版本用户 RSA 公钥 PEM 文本
	RSAPublicKeyUserRef    string                           `json:"rsa_public_key_user_ref,optional"`    // 单版本用户 RSA 公钥 PEM 文件绝对路径
	RSAPublicKeyServer     string                           `json:"rsa_public_key_server,optional"`      // 单版本服务端 RSA 公钥 PEM 文本；为空时由私钥派生
	RSAPublicKeyServerRef  string                           `json:"rsa_public_key_server_ref,optional"`  // 单版本服务端 RSA 公钥文件路径；为空时由私钥派生
	RSAPrivateKeyServer    string                           `json:"rsa_private_key_server,optional"`     // 单版本服务端 RSA 私钥 PEM 文本
	RSAPrivateKeyServerRef string                           `json:"rsa_private_key_server_ref,optional"` // 单版本服务端 RSA 私钥 PEM 文件绝对路径
	SignStatus             int                              `json:"sign_status,optional,default=1"`      // 签名验签状态：1启用，0停用
	CryptoStatus           int                              `json:"crypto_status,optional,default=1"`    // 加密解密状态：1启用，0停用
	StableVersion          string                           `json:"stable_version,optional"`             // 多版本扩展配置的稳定版本；为空时回退 key_version
	GrayVersion            string                           `json:"gray_version,optional"`               // 多版本扩展配置的灰度版本；为空表示不启用灰度
	GrayPercent            int                              `json:"gray_percent,optional"`               // 多版本扩展配置的灰度流量百分比，取值 0-100
	GraySalt               string                           `json:"gray_salt,optional"`                  // 多版本扩展配置的灰度哈希盐值
	Versions               []SecuritySecretKeyVersionConfig `json:"versions,optional"`                   // 多版本材料列表
}

// SecurityConfig 聚合后台接口安全链路相关配置。
type SecurityConfig struct {
	SecretKey SecuritySecretKeyConfig `json:"secret_key,optional"` // 当前 app_id 的秘钥路由和版本材料配置
}

// KafkaTopicsConfig 定义按业务划分的 Kafka Topic。
type KafkaTopicsConfig struct {
	UserTag string `json:"user_tag,optional"` // 用户标签变更主题
}

// KafkaConfig 定义 Kafka 公共连接参数和 Topic 路由。
type KafkaConfig struct {
	Enabled      bool              `json:"enabled,optional"`       // 是否启用 Kafka 生产者
	Brokers      []string          `json:"brokers,optional"`       // Kafka broker 地址列表，Collector 未配置时继承
	BatchSize    int               `json:"batch_size,optional"`    // 单次写入最大消息数，Collector 未配置时继承
	WriteTimeout int               `json:"write_timeout,optional"` // 写入超时时间，单位秒，Collector 未配置时继承
	Topics       KafkaTopicsConfig `json:"topics,optional"`        // 按业务划分的 Topic 配置
}

// ArchiveJobConfig 定义单张热表的归档策略。
// 当前只负责热表归档，冷热合并查询需单独接入。
type ArchiveJobConfig struct {
	Name                    string `json:"name"`                                 // 归档任务名，要求全局唯一
	Enabled                 bool   `json:"enabled,optional"`                     // 是否启用该归档任务
	Database                string `json:"database"`                             // 热表/历史表所属数据库，默认 main
	TableName               string `json:"table_name"`                           // 热表名
	TimeColumn              string `json:"time_column,optional"`                 // 归档时间列，默认 created_at
	TimeColumnType          string `json:"time_column_type,optional"`            // 归档时间列类型：time|string|unix
	TimeColumnFormat        string `json:"time_column_format,optional"`          // 字符串时间列格式，使用 Go layout，默认 time.DateTime
	TimeColumnUnixUnit      string `json:"time_column_unix_unit,optional"`       // Unix 整数时间单位：seconds|milliseconds，默认 seconds
	PrimaryKey              string `json:"primary_key,optional"`                 // 主键列，默认 id
	ArchiveCondition        string `json:"archive_condition,optional"`           // 自定义归档过滤条件
	DeleteCondition         string `json:"delete_condition,optional"`            // 自定义清理过滤条件
	SplitUnit               string `json:"split_unit,optional"`                  // 历史表拆分粒度
	CustomDays              int    `json:"custom_days,optional"`                 // 自定义分段天数，仅 split_unit=custom_days 时生效
	HotKeepDays             int    `json:"hot_keep_days,optional"`               // 热表保留天数；删除默认按该值控制
	ArchiveDelayDays        int    `json:"archive_delay_days,optional"`          // 归档延迟天数
	ArchiveWindowSeconds    int    `json:"archive_window_seconds,optional"`      // 单个归档窗口秒数
	ArchiveWindowMode       string `json:"archive_window_mode,optional"`         // 归档/删除窗口推进模式：auto|fixed，默认 auto
	ArchiveMaxWindowsPerRun int    `json:"archive_max_windows_per_run,optional"` // 单次运行最多规划归档窗口数；窗口模式默认 1
	ArchiveAutoMaxWindows   int    `json:"archive_auto_max_windows,optional"`    // auto 模式单次最多追赶窗口数
	ArchiveAutoLightRows    int    `json:"archive_auto_light_rows,optional"`     // auto 模式轻量窗口行数阈值；删除阶段再受 delete_batch_size 限制
	ArchiveAutoLightMs      int    `json:"archive_auto_light_ms,optional"`       // auto 模式轻量窗口耗时阈值，单位毫秒
	DeleteDisabled          bool   `json:"delete_disabled,optional"`             // 是否禁用热表删除
	DeleteDelayDays         int    `json:"delete_delay_days,optional"`           // 删除延迟天数；默认等于 hot_keep_days
	DeleteWindowSeconds     int    `json:"delete_window_seconds,optional"`       // 删除窗口对齐秒数
	DeleteMaxWindowsPerRun  int    `json:"delete_max_windows_per_run,optional"`  // 单次运行最多删除已归档窗口数；auto 模式为基础窗口数
	BatchSize               int    `json:"batch_size,optional"`                  // 单批搬迁条数
	DeleteBatchSize         int    `json:"delete_batch_size,optional"`           // 单批删除条数；<=0 时回退为 batch_size
	MaxHistoryTables        int    `json:"max_history_tables,optional"`          // 最大历史表数量，超出后按最老优先淘汰
	HistoryTablePrefix      string `json:"history_table_prefix,optional"`        // 历史表前缀；为空时自动拼接 `<table_name>_archive`
	HistoryTableNameRule    string `json:"history_table_name_rule,optional"`     // 历史表命名模板
	StartAt                 string `json:"start_at,optional"`                    // 首次归档起点，格式 YYYY-MM-DD 或 YYYY-MM-DD HH:MM:SS
	QueryWriteDB            bool   `json:"query_write_db,optional"`              // 查询是否强制走主库
}

// ArchiveConfig 定义通用归档基础模块配置。
type ArchiveConfig struct {
	Enabled                bool               `json:"enabled,optional"`                    // 是否启用通用归档模块
	SafeDelayMinutes       int                `json:"safe_delay_minutes,optional"`         // 安全查询/归档延迟，单位分钟
	LockTTLSeconds         int                `json:"lock_ttl_seconds,optional"`           // 规划/推进 watermark 互斥锁 TTL，单位秒
	LeaseTTLSeconds        int                `json:"lease_ttl_seconds,optional"`          // 区间 checkpoint 租约 TTL，单位秒
	MaxConcurrentJobs      int                `json:"max_concurrent_jobs,optional"`        // 单次归档工作流并发 job 数
	DefaultBatchSize       int                `json:"default_batch_size,optional"`         // 默认单批归档条数
	DefaultDeleteBatchSize int                `json:"default_delete_batch_size,optional"`  // 默认单批删除条数
	BatchDelayMilliseconds int                `json:"batch_delay_milliseconds,optional"`   // 归档批次间隔，单位毫秒
	DefaultMaxHistoryTable int                `json:"default_max_history_tables,optional"` // 默认最大历史表数量
	Jobs                   []ArchiveJobConfig `json:"jobs,optional"`                       // 归档任务列表
}

// CollectorKafkaConfig 定义通用收集器的 Kafka 载体配置。
type CollectorKafkaConfig struct {
	Enabled      bool     `json:"enabled,optional"`       // 是否启用 Kafka 载体
	Brokers      []string `json:"brokers,optional"`       // Kafka broker 地址列表；为空时继承顶层 kafka
	Topic        string   `json:"topic,optional"`         // Collector 事件 Topic
	GroupID      string   `json:"group_id,optional"`      // Collector 消费组 ID
	BatchSize    int      `json:"batch_size,optional"`    // Kafka 生产和消费批次大小；<=0 时继承顶层 kafka
	WriteTimeout int      `json:"write_timeout,optional"` // Kafka 写入超时时间，单位秒；<=0 时继承顶层 kafka
	ReadTimeout  int      `json:"read_timeout,optional"`  // Kafka 读取超时时间，单位秒
}

// CollectorRedisConfig 定义通用收集器的 Redis Stream 载体配置。
type CollectorRedisConfig struct {
	Enabled  bool   `json:"enabled,optional"`  // 是否启用 Redis Stream 载体
	Stream   string `json:"stream,optional"`   // Redis Stream 业务名称，运行时自动追加 app_id 前缀
	Group    string `json:"group,optional"`    // Redis Stream 消费组
	Consumer string `json:"consumer,optional"` // Redis Stream 消费者名前缀
	MaxLen   int64  `json:"max_len,optional"`  // Stream 最大长度近似值，<=0 不裁剪
	BlockMs  int64  `json:"block_ms,optional"` // XREADGROUP 阻塞等待毫秒数
	Count    int64  `json:"count,optional"`    // 单次读取/认领消息数量
}

// CollectorDBConfig 定义通用收集器 DB outbox 消费和重试配置。
type CollectorDBConfig struct {
	RunnerBatchSize       int `json:"runner_batch_size,optional"`       // DB worker 单轮领取事件上限
	RunnerIntervalSeconds int `json:"runner_interval_seconds,optional"` // DB worker 轮询间隔，单位秒
	RunningLeaseSeconds   int `json:"running_lease_seconds,optional"`   // running 任务租约秒数，超时后自动回收重试
	MaxRetryTimes         int `json:"max_retry_times,optional"`         // 最大失败重试次数，达到后进入死信
}

// CollectorConfig 定义通用收集器配置。
// 业务方只投递结构化事件，Collector 负责可靠收集和批量投送。
type CollectorConfig struct {
	Enabled   bool                 `json:"enabled,optional"`   // 是否启用通用收集器
	Transport string               `json:"transport,optional"` // 载体：auto/kafka/redis/db
	Kafka     CollectorKafkaConfig `json:"kafka,optional"`     // Kafka 载体配置
	Redis     CollectorRedisConfig `json:"redis,optional"`     // Redis Stream 载体配置
	DB        CollectorDBConfig    `json:"db,optional"`        // DB outbox 消费和重试配置
}

// CDCTopicConfig 定义单个 Debezium CDC Topic 的消费规则。
type CDCTopicConfig struct {
	Enabled bool   `json:"enabled,optional"` // 是否启用该 Topic
	Topic   string `json:"topic,optional"`   // Kafka Topic 名称
	Table   string `json:"table,optional"`   // 业务表名，格式 db.table
}

// CDCConfig 定义本地轻量 CDC 消费器配置。
type CDCConfig struct {
	Enabled             bool             `json:"enabled,optional"`               // 是否启用 CDC 消费器
	Brokers             []string         `json:"brokers,optional"`               // Kafka broker 地址；为空时继承顶层 kafka.brokers
	GroupID             string           `json:"group_id,optional"`              // Kafka 消费组 ID
	ConsumerName        string           `json:"consumer_name,optional"`         // 当前进程消费者名称前缀
	ReadTimeoutSeconds  int              `json:"read_timeout_seconds,optional"`  // 单次读取超时秒数
	RetryBackoffSeconds int              `json:"retry_backoff_seconds,optional"` // 处理失败后的重试间隔秒数
	MaxRetryTimes       int              `json:"max_retry_times,optional"`       // 单条消息最大处理失败次数
	DeadLetterTopic     string           `json:"dead_letter_topic,optional"`     // 死信 Topic；为空时失败消息不提交
	Topics              []CDCTopicConfig `json:"topics,optional"`                // Debezium Topic 消费规则
}

// TestScenariosConfig 定义本地验证场景开关；未配置时不启用任何测试输出。
type TestScenariosConfig struct {
	AdminLogAudit AdminLogAuditTestScenario `json:"admin_log_audit,optional"` // admin_log 审核日志验证
}

// AdminLogAuditTestScenario 定义 admin_log 审核日志验证输出。
type AdminLogAuditTestScenario struct {
	LarkEnabled      bool     `json:"lark_enabled,optional"`      // 是否把审核日志推送到 Lark
	CollectorEnabled bool     `json:"collector_enabled,optional"` // 是否写入 Collector outbox 并批量消费
	TraceIDPrefix    string   `json:"trace_id_prefix,optional"`   // 验证 trace_id 前缀；为空不过滤
	Actions          []string `json:"actions,optional"`           // 允许处理的审计动作；为空不过滤
	Routes           []string `json:"routes,optional"`            // 允许处理的路由别名；为空不过滤
	OutputFile       string   `json:"output_file,optional"`       // 批处理观察文件；为空只打印日志
}

// UserTagConfig 定义用户标签重构任务的运行参数。
type UserTagConfig struct {
	Enabled            bool `json:"enabled,optional"`              // 是否启用用户标签工作流插件
	EventHookEnabled   bool `json:"event_hook_enabled,optional"`   // 是否启用标签得失事件 hook 派发
	DefaultShardTotal  int  `json:"default_shard_total,optional"`  // 默认工作流任务分片数，小量数据建议从 1 开始
	DefaultBatchSize   int  `json:"default_batch_size,optional"`   // 默认游标扫描批次大小
	DefaultWorkerCount int  `json:"default_worker_count,optional"` // 节点内部 worker 默认值
	RuntimeShardTotal  int  `json:"runtime_shard_total,optional"`  // 运行期 UID 索引分片数，默认 1000
	ResultShardTotal   int  `json:"result_shard_total,optional"`   // 标签结果物理分表数量，默认 1
	DiffBatchSize      int  `json:"diff_batch_size,optional"`      // 标签差异解析批次大小
	EventBatchSize     int  `json:"event_batch_size,optional"`     // 事件 outbox 派发批次大小
}

// TaskQueueSchedulerConfig 定义周期任务调度器的 leader 选举与同步参数。
type TaskQueueSchedulerConfig struct {
	Enabled                  bool   `json:"enabled,optional"`                    // 是否启用周期调度器
	LeaseKey                 string `json:"lease_key,optional"`                  // 调度 leader 锁 key
	LeaseTTLSeconds          int    `json:"lease_ttl_seconds,optional"`          // leader 锁 TTL（秒）
	RenewIntervalSeconds     int    `json:"renew_interval_seconds,optional"`     // leader 续约间隔（秒）
	SyncIntervalSeconds      int    `json:"sync_interval_seconds,optional"`      // 周期任务配置同步间隔（秒）
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds,optional"` // 调度器心跳间隔（秒）
	MaxQueueBacklog          int    `json:"max_queue_backlog,optional"`          // 周期投递积压上限
}

// TaskPeriodicConfig 定义一个周期性工作流触发配置。
type TaskPeriodicConfig struct {
	Enabled          *bool    `json:"enabled,optional"`            // 是否启用该周期任务；未配置时默认启用
	Name             string   `json:"name,optional"`               // 周期任务名称；为空时使用 cron/workflow/queue 生成去重键
	Cron             string   `json:"cron,optional"`               // cron 表达式；支持 5 段或带秒字段的 6 段表达式
	EverySeconds     int      `json:"every_seconds,optional"`      // 秒级固定间隔；>0 时生成 @every Ns 调度，不能和 cron 同时配置
	Workflow         string   `json:"workflow,optional"`           // 工作流名称
	Queue            string   `json:"queue,optional"`              // 投递队列
	Targets          []string `json:"targets,optional"`            // 执行目标列表（如缓存键）
	ShardTotal       int      `json:"shard_total,optional"`        // 分片总数
	GrayPercent      int      `json:"gray_percent,optional"`       // 灰度比例
	Retry            int      `json:"retry,optional"`              // 覆盖重试次数（0 表示使用默认值）
	TimeoutSeconds   int      `json:"timeout_seconds,optional"`    // 任务超时（秒）
	Deadline         string   `json:"deadline,optional"`           // 截止时间（RFC3339）
	UniqueKey        string   `json:"unique_key,optional"`         // 去重键
	UniqueTTLSeconds int      `json:"unique_ttl_seconds,optional"` // 去重 TTL（秒）
}

// EnabledOrDefault 返回周期任务启用状态；未配置 enabled 时按默认启用处理。
func (c TaskPeriodicConfig) EnabledOrDefault() bool {
	return c.Enabled == nil || *c.Enabled
}

// TaskQueueConfig 定义任务系统的 Worker、队列、聚合、独立 Redis 与工作流保留参数。
type TaskQueueConfig struct {
	Enabled                   bool                     `json:"enabled,optional"`                     // 是否启用任务系统
	AppID                     string                   `json:"-"`                                    // 任务系统站点命名空间
	DefaultQueue              string                   `json:"default_queue,optional"`               // 默认工作流队列
	Concurrency               int                      `json:"concurrency,optional"`                 // Worker 并发度
	StrictPriority            bool                     `json:"strict_priority,optional"`             // 是否启用严格优先级
	Queues                    map[string]int           `json:"queues,optional"`                      // 队列权重配置
	Redis                     RedisConfig              `json:"redis,optional"`                       // 任务系统独立 Redis 配置
	DefaultRetry              int                      `json:"default_retry,optional"`               // 默认重试次数
	DefaultTimeoutSeconds     int                      `json:"default_timeout_seconds,optional"`     // 默认任务超时（秒）
	DefaultUniqueTTLSeconds   int                      `json:"default_unique_ttl_seconds,optional"`  // 默认去重 TTL（秒）
	CompletedRetentionSeconds int                      `json:"completed_retention_seconds,optional"` // 已完成任务保留时长（秒）
	ArchivedRetentionSeconds  int                      `json:"archived_retention_seconds,optional"`  // 归档失败任务保留时长（秒）
	ShutdownTimeoutSeconds    int                      `json:"shutdown_timeout_seconds,optional"`    // Worker 关闭等待时间（秒）
	TaskCheckSeconds          int                      `json:"task_check_seconds,optional"`          // 空队列轮询间隔（秒）
	DelayedTaskCheckSeconds   int                      `json:"delayed_task_check_seconds,optional"`  // 定时/重试任务检查间隔（秒）
	GroupGracePeriodSeconds   int                      `json:"group_grace_period_seconds,optional"`  // 聚合等待窗口（秒）
	GroupMaxDelaySeconds      int                      `json:"group_max_delay_seconds,optional"`     // 聚合最大等待时间（秒）
	GroupMaxSize              int                      `json:"group_max_size,optional"`              // 单次聚合最大任务数
	WorkflowRetentionSeconds  int                      `json:"workflow_retention_seconds,optional"`  // 工作流状态保留时长（秒）
	Scheduler                 TaskQueueSchedulerConfig `json:"scheduler,optional"`                   // 周期调度器配置
	Periodic                  []TaskPeriodicConfig     `json:"periodic,optional"`                    // 周期任务列表
}

// WorkflowsConfig 聚合工作流类配置。
type WorkflowsConfig struct {
	UserTag UserTagConfig `json:"user_tag,optional"` // 用户标签计算工作流配置
}

// HotReloadConfig 定义 config.yaml 热加载监听参数。
// 该能力只刷新运行期配置，不重建基础设施连接。
type HotReloadConfig struct {
	Enabled              bool `json:"enabled,optional"`                // 是否启用配置热加载
	CheckIntervalSeconds int  `json:"check_interval_seconds,optional"` // 配置文件轮询间隔，单位秒
}

// ConfigFilesConfig 定义可选外部配置文件。
// 该配置用于拆分运行期大列表配置。
type ConfigFilesConfig struct {
	Runtime string `json:"runtime,optional"` // 运行期配置文件
}

// RuntimeConfigSourceConfig 定义运行期大列表配置来源。
// 仅 task_periodic 和 archive_jobs 支持 DB 发布快照，基础设施配置仍来自启动 YAML。
type RuntimeConfigSourceConfig struct {
	Source              string `json:"source,optional"`                // 来源：file 或 database，默认 file
	Env                 string `json:"env,optional"`                   // 运行配置环境，默认取 mode，仍为空时使用 default
	PollIntervalSeconds int    `json:"poll_interval_seconds,optional"` // DB 模式版本轮询间隔秒数，默认 30
}

// APIServiceConfig 定义 admin 访问前台 API 内网运维接口的配置。
type APIServiceConfig struct {
	InternalBaseURL string `json:"internal_base_url,optional"` // API 内网地址，仅 admin 后端调用
	OpsToken        string `json:"ops_token,optional"`         // API 内网运维令牌，只在后端请求头使用
	TimeoutSeconds  int    `json:"timeout_seconds,optional"`   // API 内网调用超时秒数，默认 5
}

// FileStorageLocalConfig 定义本地文件存储根目录和访问域名。
type FileStorageLocalConfig struct {
	RootDir string `json:"root_dir,optional"` // 本地存储根目录；为空时回退到系统临时目录
	Domain  string `json:"domain,optional"`   // 本地文件公开访问域名
}

// FileStorageS3Config 定义 AWS S3 对象存储连接参数。
type FileStorageS3Config struct {
	Enabled              bool   `json:"enabled,optional"`                // 是否启用 S3 存储
	Bucket               string `json:"bucket,optional"`                 // S3 bucket 名称
	Region               string `json:"region,optional"`                 // S3 区域
	AccessKey            string `json:"access_key,optional"`             // S3 AccessKey
	SecretKey            string `json:"secret_key,optional"`             // S3 SecretKey
	PathPrefix           string `json:"path_prefix,optional"`            // S3 对象路径前缀
	Domain               string `json:"domain,optional"`                 // S3 自定义访问域名
	Endpoint             string `json:"endpoint,optional"`               // 自定义 S3 Endpoint
	UsePathStyle         bool   `json:"use_path_style,optional"`         // 是否启用 path style 地址
	PresignExpireSeconds int    `json:"presign_expire_seconds,optional"` // 直传预签名有效期，单位秒
}

// FileStorageVirusScannerConfig 定义文件上传后的病毒扫描器配置。
// Name 为空时使用 noop 空实现。
type FileStorageVirusScannerConfig struct {
	Name string `json:"name,optional"` // 病毒扫描器名称
}

// FileStorageUploadSessionConfig 定义断点续传会话的运行参数。
type FileStorageUploadSessionConfig struct {
	RootDir    string `json:"root_dir,optional"`    // 断点续传本地临时根目录；为空时回退到系统临时目录
	TTLSeconds int    `json:"ttl_seconds,optional"` // 上传会话保留时间，单位秒；<=0 时默认 24 小时
}

// FileStorageConfig 定义统一文件存储组件配置。
type FileStorageConfig struct {
	Type          string                         `json:"type,optional"`           // 存储类型：local | s3
	UploadMode    string                         `json:"upload_mode,optional"`    // 默认上传模式：server | direct
	Local         FileStorageLocalConfig         `json:"local,optional"`          // 本地存储配置
	S3            FileStorageS3Config            `json:"s3,optional"`             // S3 存储配置
	VirusScanner  FileStorageVirusScannerConfig  `json:"virus_scanner,optional"`  // 病毒扫描器配置，默认 noop
	UploadSession FileStorageUploadSessionConfig `json:"upload_session,optional"` // 断点续传会话配置
}

// ObservabilityConfig 聚合日志、链路追踪和审计相关配置，避免可观测性参数散落在多个配置段中。
type ObservabilityConfig struct {
	ServiceName     string  `json:"service_name,optional"`       // 服务名
	Environment     string  `json:"environment,optional"`        // 环境名
	TraceEnabled    bool    `json:"trace_enabled,optional"`      // 是否启用 trace 采样/上报
	OTLPProtocol    string  `json:"otlp_protocol,optional"`      // OTLP 协议：grpc/http；为空默认 grpc
	OTLPEndpoint    string  `json:"otlp_endpoint,optional"`      // OTLP endpoint
	OTLPInsecure    bool    `json:"otlp_insecure,optional"`      // OTLP 是否明文
	SampleRatio     float64 `json:"sample_ratio,optional"`       // trace 采样率 0~1
	SlowSQLMs       int64   `json:"slow_sql_ms,optional"`        // 慢 SQL 阈值，毫秒
	RedisSlowMs     int64   `json:"redis_slow_ms,optional"`      // 慢 Redis 阈值，毫秒
	LogBodyMaxBytes int     `json:"log_body_max_bytes,optional"` // 审计/日志负载最大长度
}

// LarkAlertConfig 定义 Lark 群机器人告警配置。
type LarkAlertConfig struct {
	Enabled        bool   `json:"enabled,optional"`         // 是否启用 Lark 告警
	WebhookURL     string `json:"webhook_url,optional"`     // Lark 机器人 webhook URL；为空时读取 webhook_url_ref
	WebhookURLRef  string `json:"webhook_url_ref,optional"` // webhook URL 文件路径
	Secret         string `json:"secret,optional"`          // Lark 签名密钥；为空时读取 secret_ref
	SecretRef      string `json:"secret_ref,optional"`      // Lark 签名密钥文件路径
	TimeoutSeconds int    `json:"timeout_seconds,optional"` // HTTP 请求超时，单位秒，默认 5 秒
	AtAll          bool   `json:"at_all,optional"`          // 是否在告警中 @所有人
	MaxErrorBytes  int    `json:"max_error_bytes,optional"` // 错误摘要最大字节数，默认 800
}

// AlertConfig 聚合外部告警通道配置。
type AlertConfig struct {
	Lark LarkAlertConfig `json:"lark,optional"` // Lark 群机器人告警配置
}

// Config 是服务总配置，除 go-zero RestConf 外，补充数据库、Redis、JWT 与可观测性参数。
type Config struct {
	rest.RestConf                           // go-zero HTTP 基础配置
	RunMode       int                       `json:"run_mode,optional"`                     // 进程启动模式位掩码；未显式传 `-mode` 时作为兜底值
	AppID         string                    `json:"app_id,optional"`                       // 站点/应用 ID（如 1）
	AppKey        string                    `json:"app_key,optional"`                      // 全局应用密钥，用于 MFA 秘钥等敏感数据的库内加解密
	Snowflake     SnowflakeConfig           `json:"snowflake,optional"`                    // 分布式雪花 ID 配置
	User          UserConfig                `json:"user,optional"`                         // 业务用户写入路由配置
	JwtSecret     string                    `json:"jwt_secret"`                            // JWT 签名密钥
	JwtExpiresIn  int64                     `json:"jwt_expires_in,optional,default=86400"` // JWT 过期时间，单位秒，默认 24 小时
	HotReload     HotReloadConfig           `json:"hot_reload,optional"`                   // config.yaml 热加载配置
	ConfigFiles   ConfigFilesConfig         `json:"config_files,optional"`                 // 外部配置文件入口
	RuntimeConfig RuntimeConfigSourceConfig `json:"runtime_config,optional"`               // 运行期大列表配置来源
	APIService    APIServiceConfig          `json:"api_service,optional"`                  // 前台 API 内网运维接口配置
	FileStorage   FileStorageConfig         `json:"file_storage,optional"`                 // 统一文件存储配置，支持本地与 S3
	Security      SecurityConfig            `json:"security,optional"`                     // 后台接口签名验签和加解密配置
	Observability ObservabilityConfig       `json:"observability,optional"`                // 日志、审计、追踪等可观测性配置
	Alert         AlertConfig               `json:"alert,optional"`                        // 外部告警通道配置
	Collector     CollectorConfig           `json:"collector,optional"`                    // 通用收集器配置
	CDC           CDCConfig                 `json:"cdc,optional"`                          // Debezium CDC 消费器配置
	TestScenarios TestScenariosConfig       `json:"test_scenarios,optional"`               // 本地验证场景配置
	MySQL         MySQLConfig               `json:"mysql,optional"`                        // 默认主库 MySQL 配置
	SiteMySQL     SiteMySQLConfig           `json:"site_mysql,optional"`                   // 可选命名扩展库配置
	Redis         RedisConfig               `json:"redis"`                                 // Redis 连接与连接池配置
	Kafka         KafkaConfig               `json:"kafka,optional"`                        // Kafka 标签变更同步配置
	Task          TaskQueueConfig           `json:"task,optional"`                         // 异步任务系统配置
	Archive       ArchiveConfig             `json:"archive,optional"`                      // 通用归档配置
	Workflows     WorkflowsConfig           `json:"workflows,optional"`                    // 工作流类配置聚合入口
}
