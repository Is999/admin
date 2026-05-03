-- 用户标签工作流通用骨架运行时表结构。
-- 仅保留结果分片、只读快照、运行期候选、节点进度和标签得失事件 outbox。

CREATE TABLE IF NOT EXISTS `user_tag_0` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uid` bigint unsigned NOT NULL COMMENT '用户ID',
  `tag_type` int NOT NULL COMMENT '标签类型',
  `tag_source` tinyint NOT NULL DEFAULT 0 COMMENT '标签来源：0系统 1人工',
  `tag_data` int NOT NULL DEFAULT 0 COMMENT '标签附加数据',
  `tag_category` varchar(50) NOT NULL DEFAULT '' COMMENT '标签大类',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_uid_tag_type` (`uid`, `tag_type`),
  KEY `idx_tag_type_uid` (`tag_type`, `uid`),
  KEY `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签表';

CREATE TABLE IF NOT EXISTS `user_tag_1` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_2` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_3` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_4` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_5` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_6` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_7` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_8` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_9` LIKE `user_tag_0`;

CREATE TABLE IF NOT EXISTS `user_tag_0_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_1_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_2_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_3_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_4_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_5_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_6_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_7_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_8_tmp` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_9_tmp` LIKE `user_tag_0`;

CREATE TABLE IF NOT EXISTS `user_tag_sync_0` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_1` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_2` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_3` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_4` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_5` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_6` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_7` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_8` LIKE `user_tag_0`;
CREATE TABLE IF NOT EXISTS `user_tag_sync_9` LIKE `user_tag_0`;

CREATE TABLE IF NOT EXISTS `user_tag_runtime_uid` (
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `uid` bigint NOT NULL COMMENT '用户ID',
  `shard_no` tinyint NOT NULL DEFAULT 0 COMMENT 'uid取模分片',
  `scope` varchar(40) NOT NULL DEFAULT '' COMMENT '候选范围标识',
  `reason` varchar(80) NOT NULL DEFAULT '' COMMENT '进入候选集合原因',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`workflow_id`, `uid`),
  KEY `idx_workflow_shard_uid` (`workflow_id`, `shard_no`, `uid`),
  KEY `idx_created_uid` (`created_at`, `uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签工作流运行期UID表';

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

CREATE TABLE IF NOT EXISTS `user_tag_event_outbox` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `event_id` varchar(120) NOT NULL COMMENT '事件幂等ID',
  `uid` bigint NOT NULL COMMENT '用户ID',
  `tag_type` int NOT NULL COMMENT '标签类型',
  `action` varchar(20) NOT NULL COMMENT '动作：gain/lost',
  `source_node` varchar(40) NOT NULL DEFAULT '' COMMENT '来源节点',
  `shard_no` tinyint NOT NULL DEFAULT 0 COMMENT 'uid取模分片',
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
