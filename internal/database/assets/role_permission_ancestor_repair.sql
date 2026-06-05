-- 用途：修复历史角色权限半截授权，给已有权限补齐缺失的启用祖先目录/菜单权限。

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`)
SELECT DISTINCT
  rel.`role_id`,
  parent.`id`,
  NOW()
FROM `admin_role_permission_rel` AS rel
JOIN `admin_permission` AS child
  ON child.`id` = rel.`permission_id`
  AND child.`status` = 1
JOIN `admin_permission` AS parent
  ON parent.`status` = 1
  AND FIND_IN_SET(CAST(parent.`id` AS CHAR), child.`pids`)
WHERE rel.`role_id` <> 1
  AND child.`pids` <> '';
