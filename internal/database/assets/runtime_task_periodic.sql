-- 用途：周期任务运行配置草稿表。

CREATE TABLE IF NOT EXISTS `runtime_task_periodic` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
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
  UNIQUE KEY `uk_name` (`name`),
  KEY `idx_workflow_enabled_sort` (`workflow`,`enabled`,`sort_order`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='周期任务运行配置草稿';

INSERT IGNORE INTO `runtime_task_periodic` (`name`, `enabled`, `cron`, `every_seconds`, `workflow`, `queue`, `targets_json`, `shard_total`, `gray_percent`, `retry`, `timeout_seconds`, `deadline`, `unique_key`, `unique_ttl_seconds`, `sort_order`, `remark`, `created_by_admin_id`, `updated_by_admin_id`, `created_at`, `updated_at`) VALUES ('archive-admin-log-hourly', 1, '5 * * * *', 0, 'archive.run', 'maintenance', '["admin_log"]', 2, 0, 2, 7200, '', 'periodic:archive.run:admin_log', 3300, 1, '', 0, 0, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT IGNORE INTO `runtime_task_periodic` (`name`, `enabled`, `cron`, `every_seconds`, `workflow`, `queue`, `targets_json`, `shard_total`, `gray_percent`, `retry`, `timeout_seconds`, `deadline`, `unique_key`, `unique_ttl_seconds`, `sort_order`, `remark`, `created_by_admin_id`, `updated_by_admin_id`, `created_at`, `updated_at`) VALUES ('user-tag-delta-daily', 0, '15 3 * * *', 0, 'user_tag.delta.refresh', 'maintenance', NULL, 1, 0, 2, 0, '', 'periodic:user_tag.delta.refresh', 82800, 2, '', 0, 0, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT IGNORE INTO `runtime_task_periodic` (`name`, `enabled`, `cron`, `every_seconds`, `workflow`, `queue`, `targets_json`, `shard_total`, `gray_percent`, `retry`, `timeout_seconds`, `deadline`, `unique_key`, `unique_ttl_seconds`, `sort_order`, `remark`, `created_by_admin_id`, `updated_by_admin_id`, `created_at`, `updated_at`) VALUES ('user-tag-runtime-cleanup', 0, '25 */6 * * *', 0, 'user_tag.runtime.cleanup', 'maintenance', NULL, 0, 0, 2, 3600, '', 'periodic:user_tag.runtime.cleanup', 7200, 3, '', 0, 0, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT IGNORE INTO `runtime_task_periodic` (`name`, `enabled`, `cron`, `every_seconds`, `workflow`, `queue`, `targets_json`, `shard_total`, `gray_percent`, `retry`, `timeout_seconds`, `deadline`, `unique_key`, `unique_ttl_seconds`, `sort_order`, `remark`, `created_by_admin_id`, `updated_by_admin_id`, `created_at`, `updated_at`) VALUES ('user-tag-event-outbox-retry-scan', 0, '*/10 * * * *', 0, 'user_tag.event_outbox.retry_scan', 'maintenance', NULL, 0, 0, 1, 540, '', 'periodic:user_tag.event_outbox.retry_scan', 540, 4, '', 0, 0, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT IGNORE INTO `runtime_task_periodic` (`name`, `enabled`, `cron`, `every_seconds`, `workflow`, `queue`, `targets_json`, `shard_total`, `gray_percent`, `retry`, `timeout_seconds`, `deadline`, `unique_key`, `unique_ttl_seconds`, `sort_order`, `remark`, `created_by_admin_id`, `updated_by_admin_id`, `created_at`, `updated_at`) VALUES ('task-report-daily-summary', 1, '0 10 * * *', 0, 'task_report.daily_summary', 'maintenance', NULL, 0, 0, 1, 300, '', 'periodic:task_report.daily_summary', 82800, 5, '周期任务与工作流运行日报 Lark 通知', 0, 0, '2026-06-30 00:00:00', '2026-06-30 00:00:00');
