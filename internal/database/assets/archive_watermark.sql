CREATE TABLE IF NOT EXISTS `archive_watermark` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `job_name` varchar(128) NOT NULL COMMENT '归档任务名',
  `table_name` varchar(128) NOT NULL COMMENT '热表名',
  `watermark_time` datetime(6) DEFAULT NULL COMMENT '已完整复制到历史表的排他上界',
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_job_name` (`job_name`),
  KEY `idx_table_name` (`table_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='归档水位线表';
