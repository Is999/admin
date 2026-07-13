CREATE TABLE IF NOT EXISTS `admin_role_doc_permission_rel` (
  `role_id` int unsigned NOT NULL COMMENT '角色ID',
  `doc_permission_id` int unsigned NOT NULL COMMENT '文档权限ID',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '关联创建时间',
  PRIMARY KEY (`role_id`,`doc_permission_id`),
  KEY `idx_doc_permission_id` (`doc_permission_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='角色-文档权限关系表';
