CREATE TABLE IF NOT EXISTS `admin_role` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `title` varchar(100) NOT NULL DEFAULT '' COMMENT '角色名称',
  `pid` int unsigned NOT NULL DEFAULT '0' COMMENT '父级ID',
  `pids` varchar(500) NOT NULL DEFAULT '' COMMENT '父级ID(族谱)',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '状态：1正常，0禁用',
  `describe` varchar(255) NOT NULL DEFAULT '' COMMENT '描述',
  `is_delete` tinyint NOT NULL DEFAULT '0' COMMENT '是否删除: 1删除(关联有用户或下级角色不能删除)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_title` (`title`),
  KEY `idx_pid` (`pid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='角色';

INSERT IGNORE INTO `admin_role` (`id`, `title`, `pid`, `pids`, `status`, `describe`, `is_delete`, `created_at`, `updated_at`) VALUES (1, '超级管理员', 0, '', 1, '超级管理员', 0, '2022-03-21 12:32:16', '2025-11-29 12:17:11');
