CREATE TABLE IF NOT EXISTS `admin_message_receiver` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `message_id` bigint unsigned NOT NULL COMMENT '消息ID',
  `receiver_admin_id` int unsigned NOT NULL COMMENT '接收人管理员ID',
  `read_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否已读(0未读1已读)',
  `read_at` datetime NULL DEFAULT NULL COMMENT '已读时间',
  `delete_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否删除(0未删1已删)',
  `deleted_at` datetime NULL DEFAULT NULL COMMENT '删除时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`id`),
  KEY `idx_message_id` (`message_id`),
  KEY `idx_receiver_state` (`receiver_admin_id`,`read_status`,`id`),
  KEY `idx_receiver_deleted` (`receiver_admin_id`,`delete_status`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='管理员消息收件箱';
