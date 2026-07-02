-- 用途：用户标签结果基表初始物理表。
-- 说明：按量扩容前只创建 user_tag_0，后续物理拆表由迁移任务按规则补表。

CREATE TABLE IF NOT EXISTS `user_tag_0` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uid` bigint unsigned NOT NULL COMMENT '用户 ID',
  `shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模1024分片',
  `tag_type` int NOT NULL COMMENT '标签类型',
  `tag_source` tinyint NOT NULL DEFAULT 0 COMMENT '标签来源：0系统 1人工',
  `tag_data` int NOT NULL DEFAULT 0 COMMENT '标签附加数据',
  `tag_category` varchar(50) NOT NULL DEFAULT '' COMMENT '标签大类',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_uid_tag_type` (`uid`, `tag_type`),
  KEY `idx_shard_uid` (`shard_no`, `uid`),
  KEY `idx_tag_type_uid` (`tag_type`, `uid`),
  KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`),
  KEY `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签表';
