-- 用途：补齐文档站目录级访问权限。
-- 范围：仅新增 docs.index 下的目录查看权限，不改文档存储结构。

INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES
(155, '200059', '运维文档', 'docs.role.ops', 99, '65,99', 0, '访问运维角色文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(156, '200060', '后端开发文档', 'docs.role.backend', 99, '65,99', 0, '访问后端开发角色文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(157, '200061', '前端与测试文档', 'docs.role.frontend', 99, '65,99', 0, '访问前端与测试角色文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(158, '200062', '任务系统文档', 'docs.feature.task', 99, '65,99', 0, '访问任务系统功能文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(159, '200063', '用户标签文档', 'docs.feature.user_tag', 99, '65,99', 0, '访问用户标签功能文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(160, '200064', '接口文档规范', 'docs.api.index', 99, '65,99', 0, '访问接口文档首页和统一规范(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(161, '200065', '后台系统接口文档', 'docs.api.admin', 99, '65,99', 0, '访问后台系统接口文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(162, '200066', '任务系统接口文档', 'docs.api.task', 99, '65,99', 0, '访问任务系统接口文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(163, '200067', '用户标签接口文档', 'docs.user_tag', 99, '65,99', 0, '访问用户标签接口文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(164, '200068', '前台API接口规范', 'docs.api_service.index', 99, '65,99', 0, '访问前台API接口文档首页和规范(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(165, '200069', '前台API接口文档', 'docs.api_service.front', 99, '65,99', 0, '访问前台API前台系统接口文档目录(查看)', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
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
(1, 155, CURRENT_TIMESTAMP),
(1, 156, CURRENT_TIMESTAMP),
(1, 157, CURRENT_TIMESTAMP),
(1, 158, CURRENT_TIMESTAMP),
(1, 159, CURRENT_TIMESTAMP),
(1, 160, CURRENT_TIMESTAMP),
(1, 161, CURRENT_TIMESTAMP),
(1, 162, CURRENT_TIMESTAMP),
(1, 163, CURRENT_TIMESTAMP),
(1, 164, CURRENT_TIMESTAMP),
(1, 165, CURRENT_TIMESTAMP);
