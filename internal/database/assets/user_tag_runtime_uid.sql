-- 用途：用户标签工作流运行期候选 UID 表。

CREATE TABLE IF NOT EXISTS `user_tag_runtime_uid` (
  `workflow_id` varchar(80) NOT NULL COMMENT '工作流ID',
  `uid` bigint NOT NULL COMMENT '用户 ID',
  `shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模1024分片',
  `scope` varchar(40) NOT NULL DEFAULT '' COMMENT '候选范围标识',
  `reason` varchar(80) NOT NULL DEFAULT '' COMMENT '进入候选集合原因',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`workflow_id`, `uid`),
  KEY `idx_workflow_shard_uid` (`workflow_id`, `shard_no`, `uid`),
  KEY `idx_created_uid` (`created_at`, `uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户标签工作流运行期UID表';
