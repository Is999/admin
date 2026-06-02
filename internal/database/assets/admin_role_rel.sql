CREATE TABLE IF NOT EXISTS `admin_role_rel` (
  `user_id` int unsigned NOT NULL COMMENT '用户id',
  `role_id` int unsigned NOT NULL COMMENT '角色id',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`user_id`,`role_id`),
  KEY `idx_role_id` (`role_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户角色';

INSERT IGNORE INTO `admin_role_rel` (`user_id`, `role_id`, `created_at`) VALUES (1, 1, '2022-04-05 16:30:56');
