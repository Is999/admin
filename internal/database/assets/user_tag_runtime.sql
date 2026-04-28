-- 用户标签工作流骨架运行时表结构。
-- 当前只保留框架运行需要的结果表、同步快照、运行期 UID 和 outbox。

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

CREATE TABLE IF NOT EXISTS `user_tag_kafka_outbox` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `shard_no` tinyint NOT NULL DEFAULT 0 COMMENT 'uid取模分片',
  `uid` bigint NOT NULL COMMENT '用户ID',
  `tag_type` int NOT NULL COMMENT '标签类型',
  `action_type` varchar(20) NOT NULL COMMENT '动作:getTag/lostTag',
  `payload` varchar(500) NOT NULL DEFAULT '' COMMENT 'Kafka消息JSON',
  `state` tinyint NOT NULL DEFAULT 0 COMMENT '状态：0待推送 1推送中 2已推送 3待重试 4死信',
  `attempt` int NOT NULL DEFAULT 0 COMMENT '推送尝试次数',
  `last_error` varchar(1000) NOT NULL DEFAULT '' COMMENT '最近失败原因',
  `sent_at` datetime NULL COMMENT '推送成功时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_workflow_uid_tag_action` (`workflow_id`, `uid`, `tag_type`, `action_type`),
  KEY `idx_workflow_state_shard_uid` (`workflow_id`, `state`, `shard_no`, `uid`),
  KEY `idx_state_updated_id` (`state`, `updated_at`, `id`),
  KEY `idx_uid` (`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签Kafka差异Outbox表';

CREATE TABLE IF NOT EXISTS `user_tag_runtime_uid` (
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `uid` bigint NOT NULL COMMENT '用户ID',
  `shard_no` tinyint NOT NULL DEFAULT 0 COMMENT 'uid取模分片',
  `source` varchar(40) NOT NULL DEFAULT '' COMMENT '命中来源节点',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`workflow_id`, `uid`),
  KEY `idx_workflow_shard_uid` (`workflow_id`, `shard_no`, `uid`),
  KEY `idx_created_uid` (`created_at`, `uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签工作流运行期UID表';
