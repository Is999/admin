-- 用途：运行配置当前发布状态表。

CREATE TABLE IF NOT EXISTS `runtime_config_state` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `active_release_id` bigint unsigned NOT NULL DEFAULT '0' COMMENT '当前发布ID',
  `active_version` bigint unsigned NOT NULL DEFAULT '0' COMMENT '当前发布版本号',
  `active_checksum` char(64) NOT NULL DEFAULT '' COMMENT '当前快照SHA256',
  `published_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '最近发布时间',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='运行配置当前发布状态';

INSERT IGNORE INTO `runtime_config_state` (`id`, `active_release_id`, `active_version`, `active_checksum`, `published_at`, `created_at`, `updated_at`) VALUES (1, 0, 0, '', '2026-06-16 00:00:00', '2026-06-16 00:00:00', '2026-06-16 00:00:00');
