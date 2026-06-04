-- 用途：把历史文档目录权限补齐到单篇文档权限，并补齐文档入口权限。

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`)
SELECT DISTINCT
  rel.`role_id`,
  doc.`id`,
  NOW()
FROM `admin_role_permission_rel` AS rel
INNER JOIN `admin_permission` AS parent
  ON parent.`id` = rel.`permission_id`
  AND parent.`status` = 1
INNER JOIN `admin_permission` AS doc
  ON doc.`status` = 1
  AND (
    (parent.`module` = 'docs.index' AND doc.`module` = 'docs.file.文档首页.md')
    OR (parent.`module` = 'docs.role.ops' AND doc.`module` LIKE 'docs.file.角色文档/运维/%')
    OR (parent.`module` = 'docs.role.backend' AND doc.`module` LIKE 'docs.file.角色文档/后端开发/%')
    OR (parent.`module` = 'docs.role.frontend' AND doc.`module` LIKE 'docs.file.角色文档/前端与测试/%')
    OR (parent.`module` = 'docs.feature.task' AND doc.`module` LIKE 'docs.file.功能模块/任务系统/%')
    OR (parent.`module` = 'docs.feature.user_tag' AND doc.`module` LIKE 'docs.file.功能模块/用户标签/%')
    OR (parent.`module` = 'docs.api.index' AND doc.`module` IN ('docs.file.接口文档/接口文档首页.md', 'docs.file.接口文档/接口文档统一规范.md'))
    OR (parent.`module` = 'docs.api.admin' AND doc.`module` LIKE 'docs.file.接口文档/后台系统/%')
    OR (parent.`module` = 'docs.api.task' AND doc.`module` LIKE 'docs.file.接口文档/任务系统/%')
    OR (parent.`module` = 'docs.user_tag' AND doc.`module` LIKE 'docs.file.接口文档/用户标签/%')
    OR (parent.`module` = 'docs.api_service.index' AND doc.`module` IN ('docs.file.api/接口文档/接口文档统一规范.md'))
    OR (parent.`module` = 'docs.api_service.index' AND doc.`module` LIKE 'docs.file.api/角色文档/%')
    OR (parent.`module` = 'docs.api_service.front' AND doc.`module` LIKE 'docs.file.api/接口文档/前台系统/%')
  )
WHERE rel.`role_id` <> 1;

INSERT IGNORE INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`)
SELECT DISTINCT
  rel.`role_id`,
  entry.`id`,
  NOW()
FROM `admin_role_permission_rel` AS rel
INNER JOIN `admin_permission` AS child
  ON child.`id` = rel.`permission_id`
  AND child.`status` = 1
INNER JOIN `admin_permission` AS entry
  ON entry.`module` = CASE
    WHEN child.`module` IN (
      'docs.role.ops',
      'docs.role.backend',
      'docs.role.frontend',
      'docs.feature.task',
      'docs.feature.user_tag',
      'docs.api.index',
      'docs.api.admin',
      'docs.api.task',
      'docs.user_tag'
    ) THEN 'docs.index'
    WHEN child.`module` = 'docs.api_service.front' THEN 'docs.api_service.index'
    WHEN child.`module` LIKE 'docs.file.api/%' THEN 'docs.api_service.index'
    WHEN child.`module` LIKE 'docs.file.%' THEN 'docs.index'
    ELSE ''
  END
  AND entry.`status` = 1
WHERE rel.`role_id` <> 1
  AND (
    child.`module` IN (
      'docs.role.ops',
      'docs.role.backend',
      'docs.role.frontend',
      'docs.feature.task',
      'docs.feature.user_tag',
      'docs.api.index',
      'docs.api.admin',
      'docs.api.task',
      'docs.user_tag',
      'docs.api_service.front'
    )
    OR child.`module` LIKE 'docs.file.%'
  );
