-- 用途：用户标签工作流节点进度表。

CREATE TABLE IF NOT EXISTS `user_tag_runtime_checkpoint` (
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `node` varchar(40) NOT NULL COMMENT '工作流节点',
  `shard_no` int NOT NULL DEFAULT 0 COMMENT '节点内部分片号',
  `cursor_uid` bigint NOT NULL DEFAULT 0 COMMENT 'UID游标',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '状态：0待处理 1处理中 2完成 3失败',
  `error_text` varchar(1000) NOT NULL DEFAULT '' COMMENT '最近失败摘要',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`workflow_id`, `node`, `shard_no`),
  KEY `idx_updated_at` (`updated_at`, `workflow_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签工作流节点进度表';
