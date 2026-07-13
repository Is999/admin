CREATE TABLE IF NOT EXISTS `security_cache_sync_task` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `app_id` varchar(64) NOT NULL COMMENT '应用ID',
  `digest` char(64) NOT NULL COMMENT '任务摘要',
  `payload_json` json NOT NULL COMMENT '失效计划JSON',
  `revision` bigint unsigned NOT NULL DEFAULT 1 COMMENT '任务修订号',
  `attempts` int unsigned NOT NULL DEFAULT 0 COMMENT '已重试次数',
  `next_retry_at` datetime(3) NOT NULL COMMENT '下次重试时间',
  `last_error` varchar(1000) NOT NULL DEFAULT '' COMMENT '最近错误',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_app_digest` (`app_id`,`digest`),
  KEY `idx_app_next_id` (`app_id`,`next_retry_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='安全缓存失效补偿任务表';
