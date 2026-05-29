-- 用途：运行期大列表配置数据库化。
-- 范围：仅 task_periodic 与 archive_jobs；基础设施配置和 workflows.user_tag 仍由 YAML 管理。

CREATE TABLE IF NOT EXISTS `runtime_config_state` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `app_id` varchar(64) NOT NULL COMMENT '应用ID',
  `env` varchar(64) NOT NULL COMMENT '运行环境',
  `active_release_id` bigint unsigned NOT NULL DEFAULT '0' COMMENT '当前发布ID',
  `active_version` bigint unsigned NOT NULL DEFAULT '0' COMMENT '当前发布版本号',
  `active_checksum` char(64) NOT NULL DEFAULT '' COMMENT '当前快照SHA256',
  `published_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近发布时间',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_app_env` (`app_id`,`env`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='运行配置当前发布状态';

CREATE TABLE IF NOT EXISTS `runtime_config_release` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `app_id` varchar(64) NOT NULL COMMENT '应用ID',
  `env` varchar(64) NOT NULL COMMENT '运行环境',
  `version_no` bigint unsigned NOT NULL COMMENT '发布版本号',
  `snapshot_json` json NOT NULL COMMENT '发布快照JSON',
  `snapshot_yaml` mediumtext NOT NULL COMMENT '发布快照YAML',
  `checksum` char(64) NOT NULL COMMENT '快照SHA256',
  `base_release_id` bigint unsigned NOT NULL DEFAULT '0' COMMENT '来源发布ID',
  `restart_required` tinyint(1) NOT NULL DEFAULT '0' COMMENT '是否需要重启',
  `restart_reason` varchar(500) NOT NULL DEFAULT '' COMMENT '重启原因',
  `remark` varchar(500) NOT NULL DEFAULT '' COMMENT '发布备注',
  `published_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '发布管理员ID',
  `published_by_name` varchar(64) NOT NULL DEFAULT '' COMMENT '发布管理员账号',
  `published_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '发布时间',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_app_env_version` (`app_id`,`env`,`version_no`),
  KEY `idx_app_env_published` (`app_id`,`env`,`published_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='运行配置发布快照';

CREATE TABLE IF NOT EXISTS `runtime_task_periodic` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `app_id` varchar(64) NOT NULL COMMENT '应用ID',
  `env` varchar(64) NOT NULL COMMENT '运行环境',
  `name` varchar(128) NOT NULL COMMENT '周期任务名称',
  `enabled` tinyint(1) NOT NULL DEFAULT '1' COMMENT '是否启用',
  `cron` varchar(128) NOT NULL DEFAULT '' COMMENT 'cron表达式',
  `every_seconds` int NOT NULL DEFAULT '0' COMMENT '固定间隔秒数',
  `workflow` varchar(128) NOT NULL DEFAULT '' COMMENT '工作流名称',
  `queue` varchar(64) NOT NULL DEFAULT '' COMMENT '投递队列',
  `targets_json` json DEFAULT NULL COMMENT '执行目标列表JSON',
  `shard_total` int NOT NULL DEFAULT '0' COMMENT '分片总数',
  `gray_percent` int NOT NULL DEFAULT '0' COMMENT '灰度比例',
  `retry` int NOT NULL DEFAULT '0' COMMENT '覆盖重试次数',
  `timeout_seconds` int NOT NULL DEFAULT '0' COMMENT '任务超时秒数',
  `deadline` varchar(64) NOT NULL DEFAULT '' COMMENT '截止时间RFC3339',
  `unique_key` varchar(255) NOT NULL DEFAULT '' COMMENT '去重键',
  `unique_ttl_seconds` int NOT NULL DEFAULT '0' COMMENT '去重TTL秒数',
  `sort_order` int NOT NULL DEFAULT '0' COMMENT '排序值',
  `remark` varchar(500) NOT NULL DEFAULT '' COMMENT '备注',
  `created_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '创建管理员ID',
  `updated_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '更新管理员ID',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_app_env_name` (`app_id`,`env`,`name`),
  KEY `idx_workflow_enabled_sort` (`app_id`,`env`,`workflow`,`enabled`,`sort_order`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='周期任务运行配置草稿';

CREATE TABLE IF NOT EXISTS `runtime_archive_job` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `app_id` varchar(64) NOT NULL COMMENT '应用ID',
  `env` varchar(64) NOT NULL COMMENT '运行环境',
  `name` varchar(128) NOT NULL COMMENT '归档任务名称',
  `enabled` tinyint(1) NOT NULL DEFAULT '1' COMMENT '是否启用',
  `database_name` varchar(64) NOT NULL DEFAULT 'main' COMMENT '热表数据库',
  `table_name` varchar(128) NOT NULL DEFAULT '' COMMENT '热表名',
  `time_column` varchar(64) NOT NULL DEFAULT '' COMMENT '归档时间列',
  `time_column_type` varchar(32) NOT NULL DEFAULT '' COMMENT '时间列类型',
  `time_column_format` varchar(64) NOT NULL DEFAULT '' COMMENT '字符串时间格式',
  `time_column_unix_unit` varchar(32) NOT NULL DEFAULT '' COMMENT 'Unix时间单位',
  `primary_key` varchar(64) NOT NULL DEFAULT '' COMMENT '主键列',
  `archive_condition` varchar(500) NOT NULL DEFAULT '' COMMENT '归档过滤条件',
  `delete_condition` varchar(500) NOT NULL DEFAULT '' COMMENT '清理过滤条件',
  `split_unit` varchar(32) NOT NULL DEFAULT '' COMMENT '历史表拆分粒度',
  `custom_days` int NOT NULL DEFAULT '0' COMMENT '自定义分段天数',
  `hot_keep_days` int NOT NULL DEFAULT '0' COMMENT '热表保留天数',
  `archive_delay_days` int NOT NULL DEFAULT '0' COMMENT '归档延迟天数',
  `archive_window_seconds` int NOT NULL DEFAULT '0' COMMENT '归档窗口秒数',
  `archive_window_mode` varchar(32) NOT NULL DEFAULT '' COMMENT '归档窗口模式',
  `archive_max_windows_per_run` int NOT NULL DEFAULT '0' COMMENT '单次最大归档窗口数',
  `archive_auto_max_windows` int NOT NULL DEFAULT '0' COMMENT 'auto最大追赶窗口数',
  `archive_auto_light_rows` int NOT NULL DEFAULT '0' COMMENT 'auto轻量行数阈值',
  `archive_auto_light_ms` int NOT NULL DEFAULT '0' COMMENT 'auto轻量耗时阈值毫秒',
  `delete_disabled` tinyint(1) NOT NULL DEFAULT '0' COMMENT '是否禁用删除',
  `delete_delay_days` int NOT NULL DEFAULT '0' COMMENT '删除延迟天数',
  `delete_window_seconds` int NOT NULL DEFAULT '0' COMMENT '删除窗口秒数',
  `delete_max_windows_per_run` int NOT NULL DEFAULT '0' COMMENT '单次最大删除窗口数',
  `batch_size` int NOT NULL DEFAULT '0' COMMENT '归档批次大小',
  `delete_batch_size` int NOT NULL DEFAULT '0' COMMENT '删除批次大小',
  `max_history_tables` int NOT NULL DEFAULT '0' COMMENT '最大历史表数量',
  `history_table_prefix` varchar(128) NOT NULL DEFAULT '' COMMENT '历史表前缀',
  `history_table_name_rule` varchar(255) NOT NULL DEFAULT '' COMMENT '历史表命名规则',
  `start_at` varchar(64) NOT NULL DEFAULT '' COMMENT '首次归档起点',
  `query_write_db` tinyint(1) NOT NULL DEFAULT '0' COMMENT '查询是否强制走主库',
  `sort_order` int NOT NULL DEFAULT '0' COMMENT '排序值',
  `remark` varchar(500) NOT NULL DEFAULT '' COMMENT '备注',
  `created_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '创建管理员ID',
  `updated_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '更新管理员ID',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_app_env_name` (`app_id`,`env`,`name`),
  KEY `idx_archive_table` (`app_id`,`env`,`database_name`,`table_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='归档任务运行配置草稿';

INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES
(139, '200050', '运行配置', 'runtime.config.index', 71, '71', 5, '运行配置(菜单,页面)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(140, '200051', '查询', 'runtime.config.list', 139, '71,139', 0, '查询运行配置(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(141, '200052', '保存', 'runtime.config.save', 139, '71,139', 2, '保存运行配置草稿(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(142, '200053', '预检', 'runtime.config.validate', 139, '71,139', 0, '预检运行配置(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(143, '200054', '发布', 'runtime.config.publish', 139, '71,139', 2, '发布运行配置(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(144, '200055', '回滚', 'runtime.config.rollback', 139, '71,139', 2, '回滚运行配置(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(145, '200056', '导入', 'runtime.config.import', 139, '71,139', 1, '导入运行配置(新增)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON DUPLICATE KEY UPDATE
  `title` = VALUES(`title`),
  `module` = VALUES(`module`),
  `pid` = VALUES(`pid`),
  `pids` = VALUES(`pids`),
  `type` = VALUES(`type`),
  `description` = VALUES(`description`),
  `status` = VALUES(`status`),
  `updated_at` = CURRENT_TIMESTAMP;

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES
(1, 139, CURRENT_TIMESTAMP),
(1, 140, CURRENT_TIMESTAMP),
(1, 141, CURRENT_TIMESTAMP),
(1, 142, CURRENT_TIMESTAMP),
(1, 143, CURRENT_TIMESTAMP),
(1, 144, CURRENT_TIMESTAMP),
(1, 145, CURRENT_TIMESTAMP);

UPDATE `sys_config`
SET `example` = JSON_SET(CAST(`example` AS JSON), '$."12"', '运行配置发布/回滚/导入'),
    `updated_at` = CURRENT_TIMESTAMP
WHERE `uuid` = 'adminDisableMFACheckScenario';
