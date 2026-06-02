-- 用途：将前台用户列表从系统管理调整到独立用户管理目录。
-- 范围：只修正权限树、账号管理文案和目录授权，不修改业务数据。

INSERT INTO `admin_permission`
  (`uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`)
VALUES
  ('100099', '用户管理', '4', 0, '', 4, '用户管理(目录)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON DUPLICATE KEY UPDATE
  `title` = VALUES(`title`),
  `module` = VALUES(`module`),
  `pid` = VALUES(`pid`),
  `pids` = VALUES(`pids`),
  `type` = VALUES(`type`),
  `description` = VALUES(`description`),
  `status` = VALUES(`status`),
  `updated_at` = CURRENT_TIMESTAMP;

UPDATE `admin_permission`
SET `title` = '账号管理',
    `description` = '账号管理(菜单,页面)',
    `updated_at` = CURRENT_TIMESTAMP
WHERE `uuid` = '100023';

UPDATE `admin_permission` AS user_menu
JOIN `admin_permission` AS user_dir ON user_dir.`uuid` = '100099'
SET user_menu.`title` = '用户列表',
    user_menu.`pid` = user_dir.`id`,
    user_menu.`pids` = CAST(user_dir.`id` AS CHAR),
    user_menu.`description` = '用户列表(菜单,页面)',
    user_menu.`updated_at` = CURRENT_TIMESTAMP
WHERE user_menu.`uuid` = '100092';

UPDATE `admin_permission` AS item
JOIN `admin_permission` AS user_dir ON user_dir.`uuid` = '100099'
JOIN `admin_permission` AS user_menu ON user_menu.`uuid` = '100092'
SET item.`pid` = user_menu.`id`,
    item.`pids` = CONCAT(user_dir.`id`, ',', user_menu.`id`),
    item.`updated_at` = CURRENT_TIMESTAMP
WHERE item.`uuid` IN ('100093', '100094', '100095', '100096', '100097', '100098');

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`)
SELECT DISTINCT rel.`role_id`, user_dir.`id`, CURRENT_TIMESTAMP
FROM `admin_role_permission_rel` AS rel
JOIN `admin_permission` AS user_menu ON user_menu.`uuid` = '100092'
JOIN `admin_permission` AS user_dir ON user_dir.`uuid` = '100099'
WHERE rel.`permission_id` = user_menu.`id`;
