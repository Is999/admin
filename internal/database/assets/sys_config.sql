CREATE TABLE IF NOT EXISTS `sys_config` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uuid` varchar(100) NOT NULL COMMENT '配置唯一标识,命名规则(驼峰)：项目名+key',
  `title` varchar(100) NOT NULL DEFAULT '' COMMENT '配置标题',
  `type` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '展示和校验类型：0 仅做分组标题（配置归类）; 1 Object; 2 Array; 3 String; 4 Integer; 5 Float; 6 Boolean（0 = false，1 = true）; ',
  `value` json NOT NULL COMMENT '配置值(JSON 格式，可为string/number/bool/array/object)',
  `example` json NOT NULL COMMENT '配置示例，帮助说明结构',
  `remark` varchar(255) NOT NULL DEFAULT '' COMMENT '备注',
  `page` varchar(200) NOT NULL DEFAULT '' COMMENT '配置项所属页面路径，例如 /system/config/base',
  `pid` int unsigned NOT NULL DEFAULT '0' COMMENT '上级ID',
  `pids` varchar(255) NOT NULL DEFAULT '' COMMENT '上级ID(族谱)',
  `version` int unsigned NOT NULL DEFAULT '0' COMMENT '版本号',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_uuid` (`uuid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='参数配置表';

INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (1, 'adminVerifyIpConfig', 'Admin验证IP配置', 0, '0', '0', 'Admin验证IP配置', '', 0, '', 0, '2025-11-26 10:46:24', '2025-11-29 12:17:50');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (2, 'adminIpWhitelistDisable', 'Admin启用IP白名单', 6, '1', '1', '[生产建议配置：1 启用] 禁用后台IP白名单：1启用；0 禁用', '', 1, '1', 0, '2025-11-26 10:49:01', '2025-11-29 12:17:54');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (3, 'adminIpWhitelist', 'AdminIP白名单', 2, '[]', '[\"8.8.8.8\", \"127.0.0.1\"]', 'IP白名单: 多个IP以英文逗号分割', '/system/config/admin-ip-whitelist', 1, '1', 0, '2025-11-26 10:58:40', '2026-07-08 00:00:00');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (4, 'adminCheckChangeIp', 'Admin验证IP是否变更', 6, '1', '1', '[生产建议配置：1 验证] 验证IP是否变更：1 验证； 0 不验证', '', 1, '1', 0, '2025-11-26 11:08:37', '2025-11-29 12:18:01');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (5, 'adminVerifyMFAConfig', 'Admin验证MFA配置', 0, '0', '0', 'Admin验证MFA配置', '', 0, '', 0, '2025-11-26 11:19:16', '2025-11-29 12:18:09');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (6, 'adminMFACheckEnable', 'Admin校验MFA设备验证码', 6, '0', '1', '[生产建议配置：1 强启用] 强启用MFA设备（身份验证器）登录校验：1 强启用校验（用户设置MFA状态失效）；0 非强启用（默认使用用户设置的MFA状态）', '', 5, '5', 5, '2025-11-26 11:24:42', '2026-05-05 20:11:38');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (7, 'adminMFACheckFrequency', 'Admin校验MFA设备频率', 4, '1800', '300', 'MFA设备校验频率（单位秒），建议配置5分钟(300秒)以上: 0 需要校验的地方每次都校验，大于0 秒在该时间内只不再重复校验（x秒时间内只校验一次）', '', 5, '5', 0, '2025-11-26 11:28:58', '2026-05-05 01:17:21');
INSERT IGNORE INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (8, 'adminDisableMFACheckScenario', 'Admin禁用MFA设备校验应用场景', 1, '{}', '{\"1\":\"修改密码\",\"2\":\"修改MFA状态\",\"3\":\"修改MFA秘钥\",\"4\":\"修改管理员状态\",\"5\":\"新增管理员\",\"6\":\"编辑管理员资料/角色\",\"7\":\"后台重置管理员密码\",\"8\":\"后台重置管理员首次状态\",\"9\":\"后台删除管理员\",\"10\":\"释放用户标签工作流互斥锁\",\"11\":\"秘钥管理敏感操作\",\"12\":\"运行配置发布/回滚/导入\",\"13\":\"前台用户管理\",\"14\":\"API运行态热加载\"}', '配置需要跳过 MFA 二次校验的敏感操作场景；默认空对象，不跳过。', '/system/config/admin-disable-mfa-check-scenario', 5, '5', 0, '2025-11-26 11:36:14', '2026-07-08 00:00:00');
