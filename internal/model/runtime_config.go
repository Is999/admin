package model

import "time"

const (
	// TableNameRuntimeConfigState 保存当前环境的 active 发布版本。
	TableNameRuntimeConfigState = "runtime_config_state"
	// TableNameRuntimeConfigRelease 保存不可变运行配置发布快照。
	TableNameRuntimeConfigRelease = "runtime_config_release"
	// TableNameRuntimeTaskPeriodic 保存周期任务运行配置草稿。
	TableNameRuntimeTaskPeriodic = "runtime_task_periodic"
	// TableNameRuntimeArchiveJob 保存归档任务运行配置草稿。
	TableNameRuntimeArchiveJob = "runtime_archive_job"
)

// RuntimeConfigState 记录指定 app/env 当前正在生效的发布版本。
type RuntimeConfigState struct {
	ID              uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键ID" json:"id"`                                           // 主键 ID
	AppID           string    `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_env,priority:1;comment:应用ID" json:"appId"`                           // 应用 ID
	Env             string    `gorm:"column:env;type:varchar(64);not null;uniqueIndex:uk_app_env,priority:2;comment:运行环境" json:"env"`                                // 运行环境
	ActiveReleaseID uint64    `gorm:"column:active_release_id;type:bigint unsigned;not null;default:0;comment:当前发布ID" json:"activeReleaseId"`                        // 当前发布 ID
	ActiveVersion   uint64    `gorm:"column:active_version;type:bigint unsigned;not null;default:0;comment:当前发布版本号" json:"activeVersion"`                            // 当前发布版本号
	ActiveChecksum  string    `gorm:"column:active_checksum;type:char(64);not null;default:'';comment:当前快照SHA256" json:"activeChecksum"`                             // 当前快照 SHA256
	PublishedAt     time.Time `gorm:"column:published_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:最近发布时间" json:"publishedAt"`                       // 最近发布时间
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                             // 创建时间
	UpdatedAt       time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"` // 更新时间
}

// TableName 返回运行配置状态表名。
func (*RuntimeConfigState) TableName() string {
	return TableNameRuntimeConfigState
}

// RuntimeConfigRelease 记录一次不可变发布快照。
type RuntimeConfigRelease struct {
	ID                 uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键ID" json:"id"`                                                                // 主键 ID
	AppID              string    `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_env_version,priority:1;index:idx_app_env_published,priority:1;comment:应用ID" json:"appId"` // 应用 ID
	Env                string    `gorm:"column:env;type:varchar(64);not null;uniqueIndex:uk_app_env_version,priority:2;index:idx_app_env_published,priority:2;comment:运行环境" json:"env"`      // 运行环境
	VersionNo          uint64    `gorm:"column:version_no;type:bigint unsigned;not null;uniqueIndex:uk_app_env_version,priority:3;comment:发布版本号" json:"versionNo"`                           // 发布版本号
	SnapshotJSON       string    `gorm:"column:snapshot_json;type:json;not null;comment:发布快照JSON" json:"snapshotJson"`                                                                       // 发布快照 JSON
	SnapshotYAML       string    `gorm:"column:snapshot_yaml;type:mediumtext;not null;comment:发布快照YAML" json:"snapshotYaml"`                                                                 // 发布快照 YAML
	Checksum           string    `gorm:"column:checksum;type:char(64);not null;comment:快照SHA256" json:"checksum"`                                                                            // 快照 SHA256
	BaseReleaseID      uint64    `gorm:"column:base_release_id;type:bigint unsigned;not null;default:0;comment:来源发布ID" json:"baseReleaseId"`                                                 // 来源发布 ID
	RestartRequired    bool      `gorm:"column:restart_required;type:tinyint(1);not null;default:0;comment:是否需要重启" json:"restartRequired"`                                                   // 是否需要重启
	RestartReason      string    `gorm:"column:restart_reason;type:varchar(500);not null;default:'';comment:重启原因" json:"restartReason"`                                                      // 重启原因
	Remark             string    `gorm:"column:remark;type:varchar(500);not null;default:'';comment:发布备注" json:"remark"`                                                                     // 发布备注
	PublishedByAdminID int       `gorm:"column:published_by_admin_id;type:int unsigned;not null;default:0;comment:发布管理员ID" json:"publishedByAdminId"`                                        // 发布管理员 ID
	PublishedByName    string    `gorm:"column:published_by_name;type:varchar(64);not null;default:'';comment:发布管理员账号" json:"publishedByName"`                                               // 发布管理员账号
	PublishedAt        time.Time `gorm:"column:published_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;index:idx_app_env_published,priority:3;comment:发布时间" json:"publishedAt"`       // 发布时间
	CreatedAt          time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                                  // 创建时间
}

// TableName 返回运行配置发布表名。
func (*RuntimeConfigRelease) TableName() string {
	return TableNameRuntimeConfigRelease
}

// RuntimeTaskPeriodic 保存周期任务草稿配置；发布后写入不可变快照。
type RuntimeTaskPeriodic struct {
	ID               uint64      `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键ID" json:"id"`                                                                 // 主键 ID
	AppID            string      `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_env_name,priority:1;index:idx_workflow_enabled_sort,priority:1;comment:应用ID" json:"appId"` // 应用 ID
	Env              string      `gorm:"column:env;type:varchar(64);not null;uniqueIndex:uk_app_env_name,priority:2;index:idx_workflow_enabled_sort,priority:2;comment:运行环境" json:"env"`      // 运行环境
	Name             string      `gorm:"column:name;type:varchar(128);not null;uniqueIndex:uk_app_env_name,priority:3;comment:周期任务名称" json:"name"`                                            // 周期任务名称
	Enabled          bool        `gorm:"column:enabled;type:tinyint(1);not null;default:1;index:idx_workflow_enabled_sort,priority:4;comment:是否启用" json:"enabled"`                            // 是否启用
	Cron             string      `gorm:"column:cron;type:varchar(128);not null;default:'';comment:cron表达式" json:"cron"`                                                                       // cron 表达式
	EverySeconds     int         `gorm:"column:every_seconds;type:int;not null;default:0;comment:固定间隔秒数" json:"everySeconds"`                                                                 // 固定间隔秒数
	Workflow         string      `gorm:"column:workflow;type:varchar(128);not null;default:'';index:idx_workflow_enabled_sort,priority:3;comment:工作流名称" json:"workflow"`                      // 工作流名称
	Queue            string      `gorm:"column:queue;type:varchar(64);not null;default:'';comment:投递队列" json:"queue"`                                                                         // 投递队列
	Targets          StringSlice `gorm:"column:targets_json;type:json;comment:执行目标列表JSON" json:"targets"`                                                                                     // 执行目标列表
	ShardTotal       int         `gorm:"column:shard_total;type:int;not null;default:0;comment:分片总数" json:"shardTotal"`                                                                       // 分片总数
	GrayPercent      int         `gorm:"column:gray_percent;type:int;not null;default:0;comment:灰度比例" json:"grayPercent"`                                                                     // 灰度比例
	Retry            int         `gorm:"column:retry;type:int;not null;default:0;comment:覆盖重试次数" json:"retry"`                                                                                // 覆盖重试次数
	TimeoutSeconds   int         `gorm:"column:timeout_seconds;type:int;not null;default:0;comment:任务超时秒数" json:"timeoutSeconds"`                                                             // 任务超时秒数
	Deadline         string      `gorm:"column:deadline;type:varchar(64);not null;default:'';comment:截止时间RFC3339" json:"deadline"`                                                            // 截止时间 RFC3339
	UniqueKey        string      `gorm:"column:unique_key;type:varchar(255);not null;default:'';comment:去重键" json:"uniqueKey"`                                                                // 去重键
	UniqueTTLSeconds int         `gorm:"column:unique_ttl_seconds;type:int;not null;default:0;comment:去重TTL秒数" json:"uniqueTtlSeconds"`                                                       // 去重 TTL 秒数
	SortOrder        int         `gorm:"column:sort_order;type:int;not null;default:0;index:idx_workflow_enabled_sort,priority:5;comment:排序值" json:"sortOrder"`                               // 排序值
	Remark           string      `gorm:"column:remark;type:varchar(500);not null;default:'';comment:备注" json:"remark"`                                                                        // 备注
	CreatedByAdminID int         `gorm:"column:created_by_admin_id;type:int unsigned;not null;default:0;comment:创建管理员ID" json:"createdByAdminId"`                                             // 创建管理员 ID
	UpdatedByAdminID int         `gorm:"column:updated_by_admin_id;type:int unsigned;not null;default:0;comment:更新管理员ID" json:"updatedByAdminId"`                                             // 更新管理员 ID
	CreatedAt        time.Time   `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                                   // 创建时间
	UpdatedAt        time.Time   `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"`                       // 更新时间
}

// TableName 返回周期任务运行配置表名。
func (*RuntimeTaskPeriodic) TableName() string {
	return TableNameRuntimeTaskPeriodic
}

// RuntimeArchiveJob 保存归档任务草稿配置；发布后写入不可变快照。
type RuntimeArchiveJob struct {
	ID                      uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键ID" json:"id"`                                                         // 主键 ID
	AppID                   string    `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_env_name,priority:1;index:idx_archive_table,priority:1;comment:应用ID" json:"appId"` // 应用 ID
	Env                     string    `gorm:"column:env;type:varchar(64);not null;uniqueIndex:uk_app_env_name,priority:2;index:idx_archive_table,priority:2;comment:运行环境" json:"env"`      // 运行环境
	Name                    string    `gorm:"column:name;type:varchar(128);not null;uniqueIndex:uk_app_env_name,priority:3;comment:归档任务名称" json:"name"`                                    // 归档任务名称
	Enabled                 bool      `gorm:"column:enabled;type:tinyint(1);not null;default:1;comment:是否启用" json:"enabled"`                                                               // 是否启用
	Database                string    `gorm:"column:database_name;type:varchar(64);not null;default:'main';index:idx_archive_table,priority:3;comment:热表数据库" json:"database"`              // 热表数据库
	HotTableName            string    `gorm:"column:table_name;type:varchar(128);not null;default:'';index:idx_archive_table,priority:4;comment:热表名" json:"tableName"`                     // 热表名
	TimeColumn              string    `gorm:"column:time_column;type:varchar(64);not null;default:'';comment:归档时间列" json:"timeColumn"`                                                     // 归档时间列
	TimeColumnType          string    `gorm:"column:time_column_type;type:varchar(32);not null;default:'';comment:时间列类型" json:"timeColumnType"`                                            // 时间列类型
	TimeColumnFormat        string    `gorm:"column:time_column_format;type:varchar(64);not null;default:'';comment:字符串时间格式" json:"timeColumnFormat"`                                      // 字符串时间格式
	TimeColumnUnixUnit      string    `gorm:"column:time_column_unix_unit;type:varchar(32);not null;default:'';comment:Unix时间单位" json:"timeColumnUnixUnit"`                                // Unix 时间单位
	PrimaryKey              string    `gorm:"column:primary_key;type:varchar(64);not null;default:'';comment:主键列" json:"primaryKey"`                                                       // 主键列
	ArchiveCondition        string    `gorm:"column:archive_condition;type:varchar(500);not null;default:'';comment:归档过滤条件" json:"archiveCondition"`                                       // 归档过滤条件
	DeleteCondition         string    `gorm:"column:delete_condition;type:varchar(500);not null;default:'';comment:清理过滤条件" json:"deleteCondition"`                                         // 清理过滤条件
	SplitUnit               string    `gorm:"column:split_unit;type:varchar(32);not null;default:'';comment:历史表拆分粒度" json:"splitUnit"`                                                     // 历史表拆分粒度
	CustomDays              int       `gorm:"column:custom_days;type:int;not null;default:0;comment:自定义分段天数" json:"customDays"`                                                            // 自定义分段天数
	HotKeepDays             int       `gorm:"column:hot_keep_days;type:int;not null;default:0;comment:热表保留天数" json:"hotKeepDays"`                                                          // 热表保留天数
	ArchiveDelayDays        int       `gorm:"column:archive_delay_days;type:int;not null;default:0;comment:归档延迟天数" json:"archiveDelayDays"`                                                // 归档延迟天数
	ArchiveWindowSeconds    int       `gorm:"column:archive_window_seconds;type:int;not null;default:0;comment:归档窗口秒数" json:"archiveWindowSeconds"`                                        // 归档窗口秒数
	ArchiveWindowMode       string    `gorm:"column:archive_window_mode;type:varchar(32);not null;default:'';comment:归档窗口模式" json:"archiveWindowMode"`                                     // 归档窗口模式
	ArchiveMaxWindowsPerRun int       `gorm:"column:archive_max_windows_per_run;type:int;not null;default:0;comment:单次最大归档窗口数" json:"archiveMaxWindowsPerRun"`                             // 单次最大归档窗口数
	ArchiveAutoMaxWindows   int       `gorm:"column:archive_auto_max_windows;type:int;not null;default:0;comment:auto最大追赶窗口数" json:"archiveAutoMaxWindows"`                                // auto 最大追赶窗口数
	ArchiveAutoLightRows    int       `gorm:"column:archive_auto_light_rows;type:int;not null;default:0;comment:auto轻量行数阈值" json:"archiveAutoLightRows"`                                   // auto 轻量行数阈值
	ArchiveAutoLightMs      int       `gorm:"column:archive_auto_light_ms;type:int;not null;default:0;comment:auto轻量耗时阈值毫秒" json:"archiveAutoLightMs"`                                     // auto 轻量耗时阈值毫秒
	DeleteDisabled          bool      `gorm:"column:delete_disabled;type:tinyint(1);not null;default:0;comment:是否禁用删除" json:"deleteDisabled"`                                              // 是否禁用删除
	DeleteDelayDays         int       `gorm:"column:delete_delay_days;type:int;not null;default:0;comment:删除延迟天数" json:"deleteDelayDays"`                                                  // 删除延迟天数
	DeleteWindowSeconds     int       `gorm:"column:delete_window_seconds;type:int;not null;default:0;comment:删除窗口秒数" json:"deleteWindowSeconds"`                                          // 删除窗口秒数
	DeleteMaxWindowsPerRun  int       `gorm:"column:delete_max_windows_per_run;type:int;not null;default:0;comment:单次最大删除窗口数" json:"deleteMaxWindowsPerRun"`                               // 单次最大删除窗口数
	BatchSize               int       `gorm:"column:batch_size;type:int;not null;default:0;comment:归档批次大小" json:"batchSize"`                                                               // 归档批次大小
	DeleteBatchSize         int       `gorm:"column:delete_batch_size;type:int;not null;default:0;comment:删除批次大小" json:"deleteBatchSize"`                                                  // 删除批次大小
	MaxHistoryTables        int       `gorm:"column:max_history_tables;type:int;not null;default:0;comment:最大历史表数量" json:"maxHistoryTables"`                                               // 最大历史表数量
	HistoryTablePrefix      string    `gorm:"column:history_table_prefix;type:varchar(128);not null;default:'';comment:历史表前缀" json:"historyTablePrefix"`                                   // 历史表前缀
	HistoryTableNameRule    string    `gorm:"column:history_table_name_rule;type:varchar(255);not null;default:'';comment:历史表命名规则" json:"historyTableNameRule"`                            // 历史表命名规则
	StartAt                 string    `gorm:"column:start_at;type:varchar(64);not null;default:'';comment:首次归档起点" json:"startAt"`                                                          // 首次归档起点
	QueryWriteDB            bool      `gorm:"column:query_write_db;type:tinyint(1);not null;default:0;comment:查询是否强制走主库" json:"queryWriteDb"`                                              // 查询是否强制走主库
	SortOrder               int       `gorm:"column:sort_order;type:int;not null;default:0;comment:排序值" json:"sortOrder"`                                                                  // 排序值
	Remark                  string    `gorm:"column:remark;type:varchar(500);not null;default:'';comment:备注" json:"remark"`                                                                // 备注
	CreatedByAdminID        int       `gorm:"column:created_by_admin_id;type:int unsigned;not null;default:0;comment:创建管理员ID" json:"createdByAdminId"`                                     // 创建管理员 ID
	UpdatedByAdminID        int       `gorm:"column:updated_by_admin_id;type:int unsigned;not null;default:0;comment:更新管理员ID" json:"updatedByAdminId"`                                     // 更新管理员 ID
	CreatedAt               time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                           // 创建时间
	UpdatedAt               time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"`               // 更新时间
}

// TableName 返回归档任务运行配置表名。
func (*RuntimeArchiveJob) TableName() string {
	return TableNameRuntimeArchiveJob
}
