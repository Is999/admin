-- 用途：接入前台用户管理和 API 运行态热加载权限。
-- 范围：仅补后台权限、超管权限关联和 MFA 场景示例；不创建或修改前台用户表。

INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES
(167, '100099', '用户管理', '4', 0, '', 4, '用户管理(目录)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(146, '100092', '用户列表', 'user.list', 167, '167', 5, '用户列表(菜单,页面)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(147, '100093', '查询', 'user.info', 146, '167,146', 0, '查询前台用户详情(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(148, '100094', '新增', 'user.add', 146, '167,146', 1, '新增前台用户(新增)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(149, '100095', '编辑', 'user.update', 146, '167,146', 2, '编辑前台用户资料(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(150, '100096', '启用/禁用', 'user.status.update', 146, '167,146', 2, '启用或禁用前台用户(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(151, '100097', '重置密码', 'user.password.reset', 146, '167,146', 2, '重置前台用户密码(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(152, '100098', '同步运行态', 'user.runtime.sync', 146, '167,146', 2, '同步前台用户API运行态(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(153, '200057', '查询API热加载', 'api_runtime.config_reload.status', 135, '71,135', 0, '查询API配置热加载状态(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(154, '200058', '触发API热加载', 'api_runtime.config_reload.run', 135, '71,135', 2, '手动触发API配置热加载(修改)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(166, '200070', '查询API配置项', 'api_runtime.config_reload.items', 135, '71,135', 0, '查询API运行态配置项(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON DUPLICATE KEY UPDATE
  `title` = VALUES(`title`),
  `module` = VALUES(`module`),
  `pid` = VALUES(`pid`),
  `pids` = VALUES(`pids`),
  `type` = VALUES(`type`),
  `description` = VALUES(`description`),
  `status` = VALUES(`status`),
  `updated_at` = CURRENT_TIMESTAMP;

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES
(1, 167, CURRENT_TIMESTAMP),
(1, 146, CURRENT_TIMESTAMP),
(1, 147, CURRENT_TIMESTAMP),
(1, 148, CURRENT_TIMESTAMP),
(1, 149, CURRENT_TIMESTAMP),
(1, 150, CURRENT_TIMESTAMP),
(1, 151, CURRENT_TIMESTAMP),
(1, 152, CURRENT_TIMESTAMP),
(1, 153, CURRENT_TIMESTAMP),
(1, 154, CURRENT_TIMESTAMP),
(1, 166, CURRENT_TIMESTAMP);

UPDATE `sys_config`
SET `example` = JSON_SET(CAST(`example` AS JSON), '$."13"', '前台用户管理', '$."14"', 'API运行态热加载'),
    `updated_at` = CURRENT_TIMESTAMP
WHERE `uuid` = 'adminDisableMFACheckScenario';
