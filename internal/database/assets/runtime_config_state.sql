-- 用途：运行配置当前发布状态表。

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

INSERT IGNORE INTO `runtime_config_state` (
  `id`, `app_id`, `env`, `active_release_id`, `active_version`, `active_checksum`,
  `published_at`, `created_at`, `updated_at`
) VALUES (
  1, '1', 'dev', 1, 1,
  'b1dbf79448b31e09700ba3765613936264a2c23254b1270b83a9c3e6c6c2342f',
  '2026-06-16 00:00:00', '2026-06-16 00:00:00', '2026-06-16 00:00:00'
);
