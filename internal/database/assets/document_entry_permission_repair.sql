-- 用途：修正文档入口权限名称和层级，使后台接口文档与前台 API 文档作为项目工具下两个独立菜单权限。

UPDATE `admin_permission`
SET
  `title` = '后台接口文档',
  `description` = '后台接口文档(菜单,页面)',
  `pid` = 65,
  `pids` = '65',
  `type` = 5,
  `updated_at` = NOW()
WHERE `module` = 'docs.index';

UPDATE `admin_permission`
SET
  `title` = '前台 API 文档',
  `description` = '前台 API 文档(菜单,页面)',
  `pid` = 65,
  `pids` = '65',
  `type` = 5,
  `updated_at` = NOW()
WHERE `module` = 'docs.api_service.index';

UPDATE `admin_permission`
SET
  `title` = '前台系统接口文档',
  `description` = '访问前台 API 前台系统接口文档目录(查看)',
  `pid` = 164,
  `pids` = '65,164',
  `type` = 0,
  `updated_at` = NOW()
WHERE `module` = 'docs.api_service.front';

UPDATE `admin_permission`
SET
  `pid` = 165,
  `pids` = '65,164,165',
  `updated_at` = NOW()
WHERE `module` LIKE 'docs.file.api/接口文档/前台系统/%';

UPDATE `admin_permission`
SET
  `pid` = 164,
  `pids` = '65,164',
  `updated_at` = NOW()
WHERE `module` = 'docs.file.api/接口文档/接口文档统一规范.md'
   OR `module` LIKE 'docs.file.api/角色文档/%';
