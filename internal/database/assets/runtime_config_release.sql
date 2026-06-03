-- 用途：运行配置发布快照表。
-- 范围：仅 task_periodic 与 archive_jobs；基础设施配置和 workflows.user_tag 仍由 YAML 管理。

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

-- 本地开发默认运行配置种子；生产变更 app_id/env 后仍按当前配置首次导入或后台发布。
INSERT IGNORE INTO `runtime_config_release` (
  `id`, `app_id`, `env`, `version_no`, `snapshot_json`, `snapshot_yaml`, `checksum`,
  `base_release_id`, `restart_required`, `restart_reason`, `remark`,
  `published_by_admin_id`, `published_by_name`, `published_at`, `created_at`
) VALUES (
  1, '1', 'dev', 1,
  '{"archive_jobs":[{"name":"admin_log","enabled":true,"database":"main","table_name":"admin_log","time_column":"","time_column_type":"","time_column_format":"","time_column_unix_unit":"","primary_key":"","archive_condition":"","delete_condition":"","split_unit":"","custom_days":0,"hot_keep_days":32,"archive_delay_days":2,"archive_window_seconds":3600,"archive_window_mode":"auto","archive_max_windows_per_run":2,"archive_auto_max_windows":200,"archive_auto_light_rows":20000,"archive_auto_light_ms":3000,"delete_disabled":false,"delete_delay_days":32,"delete_window_seconds":0,"delete_max_windows_per_run":2,"batch_size":3000,"delete_batch_size":1000,"max_history_tables":12,"history_table_prefix":"","history_table_name_rule":"","start_at":"","query_write_db":false}],"task_periodic":[{"enabled":true,"name":"archive-admin-log-hourly","cron":"5 * * * *","every_seconds":0,"workflow":"archive.run","queue":"maintenance","targets":["admin_log"],"shard_total":2,"gray_percent":0,"retry":2,"timeout_seconds":7200,"deadline":"","unique_key":"periodic:archive.run:admin_log","unique_ttl_seconds":3300},{"enabled":false,"name":"user-tag-delta-daily","cron":"15 3 * * *","every_seconds":0,"workflow":"user_tag.delta.refresh","queue":"maintenance","targets":[],"shard_total":1,"gray_percent":0,"retry":2,"timeout_seconds":0,"deadline":"","unique_key":"periodic:user_tag.delta.refresh","unique_ttl_seconds":82800},{"enabled":false,"name":"user-tag-runtime-cleanup","cron":"25 */6 * * *","every_seconds":0,"workflow":"user_tag.runtime.cleanup","queue":"maintenance","targets":null,"shard_total":0,"gray_percent":0,"retry":2,"timeout_seconds":3600,"deadline":"","unique_key":"periodic:user_tag.runtime.cleanup","unique_ttl_seconds":7200},{"enabled":false,"name":"user-tag-event-outbox-retry-scan","cron":"*/10 * * * *","every_seconds":0,"workflow":"user_tag.event_outbox.retry_scan","queue":"maintenance","targets":null,"shard_total":0,"gray_percent":0,"retry":1,"timeout_seconds":540,"deadline":"","unique_key":"periodic:user_tag.event_outbox.retry_scan","unique_ttl_seconds":540}]}',
  'archive_jobs:
  - name: admin_log
    enabled: true
    database: main
    table_name: admin_log
    hot_keep_days: 32
    archive_delay_days: 2
    archive_window_seconds: 3600
    archive_window_mode: auto
    archive_max_windows_per_run: 2
    archive_auto_max_windows: 200
    archive_auto_light_rows: 20000
    archive_auto_light_ms: 3000
    delete_disabled: false
    delete_delay_days: 32
    delete_window_seconds: 0
    delete_max_windows_per_run: 2
    batch_size: 3000
    delete_batch_size: 1000
    max_history_tables: 12
task_periodic:
  - enabled: true
    name: archive-admin-log-hourly
    cron: "5 * * * *"
    workflow: archive.run
    queue: maintenance
    targets:
      - admin_log
    shard_total: 2
    retry: 2
    timeout_seconds: 7200
    unique_key: periodic:archive.run:admin_log
    unique_ttl_seconds: 3300
  - enabled: false
    name: user-tag-delta-daily
    cron: "15 3 * * *"
    workflow: user_tag.delta.refresh
    queue: maintenance
    targets: []
    shard_total: 1
    retry: 2
    timeout_seconds: 0
    unique_key: periodic:user_tag.delta.refresh
    unique_ttl_seconds: 82800
  - enabled: false
    name: user-tag-runtime-cleanup
    cron: "25 */6 * * *"
    workflow: user_tag.runtime.cleanup
    queue: maintenance
    retry: 2
    timeout_seconds: 3600
    unique_key: periodic:user_tag.runtime.cleanup
    unique_ttl_seconds: 7200
  - enabled: false
    name: user-tag-event-outbox-retry-scan
    cron: "*/10 * * * *"
    workflow: user_tag.event_outbox.retry_scan
    queue: maintenance
    retry: 1
    timeout_seconds: 540
    unique_key: periodic:user_tag.event_outbox.retry_scan
    unique_ttl_seconds: 540
',
  'a7c2508fdc7b46830b31ee7f7d82ea1946db7f5a5a974fe588f72fa7442d74af',
  0, 0, '', 'baseline local runtime config seed', 0, 'system',
  '2026-06-16 00:00:00', '2026-06-16 00:00:00'
);
