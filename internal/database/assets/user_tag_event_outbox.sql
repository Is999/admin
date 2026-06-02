-- 用途：用户标签得失事件 Outbox 表。

CREATE TABLE IF NOT EXISTS `user_tag_event_outbox` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `event_id` varchar(120) NOT NULL COMMENT '事件幂等ID',
  `uid` bigint NOT NULL COMMENT '用户 ID',
  `tag_type` int NOT NULL COMMENT '标签类型',
  `tag_source` tinyint NOT NULL DEFAULT 0 COMMENT '标签来源：0系统 1人工',
  `action` varchar(20) NOT NULL COMMENT '动作：gain/lost',
  `source_node` varchar(40) NOT NULL DEFAULT '' COMMENT '来源节点',
  `shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模1000分片',
  `payload` varchar(1000) NOT NULL DEFAULT '' COMMENT '扩展载荷JSON',
  `state` tinyint NOT NULL DEFAULT 0 COMMENT '状态：0待派发 1派发中 2已派发 3待重试 4死信',
  `attempt` int NOT NULL DEFAULT 0 COMMENT '派发尝试次数',
  `next_retry_at` datetime NULL COMMENT '下次重试时间',
  `locked_at` datetime NULL COMMENT '最近领取时间',
  `locked_by` varchar(80) NOT NULL DEFAULT '' COMMENT '最近领取者',
  `last_error` varchar(1000) NOT NULL DEFAULT '' COMMENT '最近失败原因',
  `dispatched_at` datetime NULL COMMENT '派发成功时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_event_id` (`event_id`),
  KEY `idx_state_next_id` (`state`, `next_retry_at`, `id`),
  KEY `idx_workflow_state_shard_id` (`workflow_id`, `state`, `shard_no`, `id`),
  KEY `idx_uid_tag_action` (`uid`, `tag_type`, `action`),
  KEY `idx_updated_id` (`updated_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签得失事件Outbox表';
