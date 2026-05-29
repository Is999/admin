/*
 Navicat Premium Dump SQL

 Source Server         : D-M
 Source Server Type    : MySQL
 Source Server Version : 80034 (8.0.34)
 Source Host           : 127.0.0.1:3307
 Source Schema         : admin

 Target Server Type    : MySQL
 Target Server Version : 80034 (8.0.34)
 File Encoding         : 65001

 Date: 28/05/2026 20:29:07
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ----------------------------
-- Table structure for admin
-- ----------------------------
DROP TABLE IF EXISTS `admin`;
CREATE TABLE `admin` (
  `id` int NOT NULL AUTO_INCREMENT COMMENT '主键',
  `name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '用户账号',
  `real_name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '用户名',
  `password` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '密码hash',
  `need_reset_password` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否必须修改登录密码：0 否，1 是',
  `email` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '邮箱',
  `phone` varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '电话',
  `mfa_secure_key` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '基于时间的动态密码 (TOTP) 多重身份验证 (MFA) 秘钥：如Google Authenticator、Microsoft Authenticator',
  `mfa_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '启用 TOTP MFA (两步验证 2FA)：0 不启用，1 启用',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '账户状态: 1正常, 0禁用',
  `avatar` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '头像',
  `description` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '简介描述',
  `last_login_time` timestamp NOT NULL COMMENT '最后登录时间',
  `last_login_ip` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '最后登录ip',
  `last_login_ipaddr` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '最后登录ip区域',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '添加时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_name` (`name`) USING BTREE
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='管理员';

-- ----------------------------
-- Records of admin
-- ----------------------------
BEGIN;
INSERT INTO `admin` (`id`, `name`, `real_name`, `password`, `need_reset_password`, `email`, `phone`, `mfa_secure_key`, `mfa_status`, `status`, `avatar`, `description`, `last_login_time`, `last_login_ip`, `last_login_ipaddr`, `created_at`, `updated_at`) VALUES (1, 'super999', 'super999', '$2a$10$jzJqarh66Le4WHK.kldSS.bZwGB8lkgpMPZb98d9PhzDkNnNqASaC', 0, '', '', '', 0, 1, '', '超级管理员', '2026-05-28 20:08:59', '127.0.0.1', '', '2022-03-21 21:54:26', '2026-05-28 20:09:39');
COMMIT;

-- ----------------------------
-- Table structure for admin_log
-- ----------------------------
DROP TABLE IF EXISTS `admin_log`;
CREATE TABLE `admin_log` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL DEFAULT '0' COMMENT '用户id',
  `user_name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '用户账户',
  `action` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '0' COMMENT '动作名称',
  `route` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '路由名称',
  `method` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '模块/类/方法',
  `describe` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '描述',
  `data` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci COMMENT '操作数据',
  `ip` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'IP地址',
  `ipaddr` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'ip地区信息',
  `trace_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'trace id',
  `span_id` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'span id',
  `http_status` int NOT NULL DEFAULT '200' COMMENT 'http状态码',
  `biz_code` int NOT NULL DEFAULT '0' COMMENT '业务码',
  `latency_ms` bigint NOT NULL DEFAULT '0' COMMENT '请求耗时ms',
  `success` tinyint(1) NOT NULL DEFAULT '1' COMMENT '是否成功',
  `error_message` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '错误信息',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`id`) USING BTREE,
  KEY `idx_created_at` (`created_at`) USING BTREE,
  KEY `idx_trace_id` (`trace_id`),
  KEY `idx_action` (`action`),
  KEY `idx_user_name` (`user_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='管理员操作日志';

-- ----------------------------
-- Records of admin_log
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for admin_message
-- ----------------------------
DROP TABLE IF EXISTS `admin_message`;
CREATE TABLE `admin_message` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `type` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '消息类型',
  `level` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '消息等级：1info 2warning 3error',
  `title` varchar(200) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '消息标题',
  `content` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci COMMENT '消息内容',
  `data` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci COMMENT '扩展数据JSON',
  `link` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '跳转链接(路由或外链)',
  `sender_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '发送人管理员ID',
  `sender_admin_name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '发送人账号快照',
  `handled_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '处理状态(0未处理1已处理)',
  `handled_by_admin_id` int unsigned NOT NULL DEFAULT '0' COMMENT '处理人管理员ID',
  `handled_by_admin_name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '处理人账号快照',
  `handled_at` timestamp NULL DEFAULT NULL COMMENT '处理时间',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`id`) USING BTREE,
  KEY `idx_created_at` (`created_at`) USING BTREE,
  KEY `idx_type` (`type`) USING BTREE,
  KEY `idx_level` (`level`) USING BTREE,
  KEY `idx_sender_admin_id` (`sender_admin_id`) USING BTREE,
  KEY `idx_handled_status` (`handled_status`) USING BTREE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='管理员消息主表';

-- ----------------------------
-- Records of admin_message
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for admin_message_receiver
-- ----------------------------
DROP TABLE IF EXISTS `admin_message_receiver`;
CREATE TABLE `admin_message_receiver` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `message_id` bigint unsigned NOT NULL COMMENT '消息ID',
  `receiver_admin_id` int unsigned NOT NULL COMMENT '接收人管理员ID',
  `read_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否已读(0未读1已读)',
  `read_at` timestamp NULL DEFAULT NULL COMMENT '已读时间',
  `delete_status` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否删除(0未删1已删)',
  `deleted_at` timestamp NULL DEFAULT NULL COMMENT '删除时间',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`id`) USING BTREE,
  KEY `idx_message_id` (`message_id`) USING BTREE,
  KEY `idx_receiver_state` (`receiver_admin_id`,`read_status`,`id`) USING BTREE,
  KEY `idx_receiver_deleted` (`receiver_admin_id`,`delete_status`,`id`) USING BTREE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='管理员消息收件箱';

-- ----------------------------
-- Records of admin_message_receiver
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for admin_permission
-- ----------------------------
DROP TABLE IF EXISTS `admin_permission`;
CREATE TABLE `admin_permission` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uuid` char(8) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '唯一标识',
  `title` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '权限名称',
  `module` varchar(250) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '权限匹配模型(路由名称 | 控制器/方法)',
  `pid` int unsigned NOT NULL DEFAULT '0' COMMENT '父级ID',
  `pids` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '父级ID(族谱)',
  `type` tinyint NOT NULL DEFAULT '0' COMMENT '类型: 0查看, 1新增, 2修改, 3删除, 4目录, 5菜单, 6页面, 7按钮, 8其它',
  `description` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '描述',
  `status` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '状态：1 启用；0 禁用',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_uuid` (`uuid`) USING BTREE,
  KEY `idx_title` (`title`)
) ENGINE=InnoDB AUTO_INCREMENT=155 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='权限';

-- ----------------------------
-- Records of admin_permission
-- ----------------------------
BEGIN;
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (1, '100001', '系统管理', '4', 0, '', 4, '系统管理(目录)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (2, '100002', '角色管理', 'role.tree.list', 1, '1', 5, '角色管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-05 18:04:02');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (3, '100003', '新增角色', '7', 2, '1,2', 7, '新增角色(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (4, '100004', '保存', 'role.add', 3, '1,2,3', 1, '新增角色(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (5, '100005', '编辑角色', '7', 2, '1,2', 7, '编辑角色(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (6, '100006', '保存', 'role.update', 5, '1,2,5', 2, '编辑角色(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (7, '100007', '删除', 'role.delete', 2, '1,2', 3, '删除角色(按钮,删除)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (8, '100008', '启用/禁用', 'role.status.update', 2, '1,2', 2, '启用/禁用角色(按钮,修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (9, '100009', '权限配置', 'role.permission.tree', 2, '1,2', 0, '查询角色权限树(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (10, '100010', '保存', 'role.permission.update', 9, '1,2,9', 2, '编辑角色权限(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (11, '100011', '权限管理', 'permission.tree.list', 1, '1', 5, '权限管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-05 18:04:40');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (12, '100012', '新增权限', '7', 11, '1,11', 7, '新增权限(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (13, '100013', '保存', 'permission.add', 12, '1,11,12', 1, '新增权限(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (14, '100014', '编辑权限', '7', 11, '1,11', 7, '编辑权限(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (15, '100015', '保存', 'permission.update', 14, '1,11,14', 2, '编辑权限(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (16, '100016', '删除', 'permission.delete', 11, '1,11', 3, '删除权限(按钮,删除)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (23, '100023', '账号管理', 'admin.list', 1, '1', 5, '管理员管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (24, '100024', '新增管理员', '7', 23, '1,23', 7, '新增管理员(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (25, '100025', '保存', 'admin.add', 24, '1,23,24', 1, '新增管理员(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (26, '100026', '编辑管理员', '7', 23, '1,23', 7, '编辑管理员(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (27, '100027', '保存', 'admin.update', 26, '1,23,26', 2, '编辑管理员(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (28, '100028', '启用/禁用', 'admin.status.update', 23, '1,23', 2, '启用/禁用管理员(按钮,修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (29, '100029', '用户角色', 'admin.role.list', 23, '1,23', 0, '查询管理员角色(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (33, '100033', '重置密码', 'admin.password.reset', 23, '1,23', 2, '重置管理员密码(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (34, '100034', 'MFA状态', 'admin.mfa_status.update', 23, '1,23', 2, '修改管理员MFA状态(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (35, '100035', '保存角色配置', 'admin.role.update', 29, '1,23,29', 2, '编辑管理员角色(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (36, '100036', '字典管理', 'system.config.list', 1, '1', 5, '字典管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-04 23:28:43');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (37, '100037', '新增配置', '8', 36, '1,36', 7, '新增系统配置(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (38, '100038', '保存', 'system.config.add', 37, '1,36,37', 1, '新增系统配置(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (39, '100039', '编辑配置', '8', 36, '1,36', 7, '编辑系统配置(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (40, '100040', '保存', 'system.config.update', 39, '1,36,39', 2, '编辑系统配置(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (41, '100041', '查看缓存', 'system.config.cache', 36, '1,36', 0, '查看系统配置缓存(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (42, '100042', '刷新缓存', 'system.config.renew', 36, '1,36', 2, '刷新系统配置缓存(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (43, '100043', '缓存管理', 'cache.list', 1, '1', 5, '缓存管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (44, '100044', '查看缓存详情', 'cache.key.info', 43, '1,43', 0, '查看缓存键详情(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (45, '100045', '刷新缓存', 'cache.renew', 43, '1,43', 2, '刷新单个缓存(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (46, '100046', '刷新全部缓存', 'cache.renew.all', 43, '1,43', 2, '刷新全部内置缓存(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (47, '100047', '服务器信息', 'cache.server.info', 43, '1,43', 0, '查看缓存服务器信息(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (48, '100048', '搜索缓存', '8', 43, '1,43', 7, '搜索缓存键(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (49, '100049', '搜索', 'cache.search', 48, '1,43,48', 0, '搜索缓存键(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (50, '100082', '按模板预热', 'cache.warmup', 43, '1,43', 2, '按模板预热缓存(修改)', 1, '2026-05-05 00:00:00', '2026-05-05 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (51, '100051', '生成MFA绑定地址', 'admin.mfa_secret_url', 26, '1,23,26', 0, '生成管理员MFA绑定地址(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (52, '100052', '后台日志', 'admin.log.query', 1, '1', 5, '后台日志(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (53, '100050', '查看缓存详情(搜索)', 'cache.key.info', 43, '1,43', 0, '查询搜索缓存键信息(查看)', 1, '2026-05-05 00:00:00', '2026-05-05 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (58, '100058', '启用/禁用', 'permission.status.update', 11, '1,11', 2, '启用/禁用权限(按钮,修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (59, '100059', '秘钥管理', 'secretKey.index', 1, '1', 5, '秘钥管理(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (60, '100060', '新增秘钥', '8', 59, '1,59', 7, '新增秘钥(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (61, '100061', '保存', 'secretKey.add', 60, '1,59,60', 1, '新增秘钥(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (62, '100062', '编辑秘钥', '8', 59, '1,59', 7, '编辑秘钥(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (63, '100063', '保存', 'secretKey.edit', 62, '1,59,62', 2, '编辑秘钥(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (64, '100064', '启用/禁用', 'secretKey.editStatus', 59, '1,59', 2, '启用/禁用秘钥(按钮,修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (65, '100065', '项目工具', '', 0, '', 4, '项目(目录)', 1, '2026-05-05 15:09:47', '2026-05-13 00:31:44');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (70, '100070', '刷新缓存', 'secretKey.renew', 59, '1,59', 2, '刷新秘钥缓存(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (71, '200001', '任务运维', '4', 0, '', 4, '任务运维(目录)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (72, '200002', '任务总控台', 'task.console.index', 71, '71', 5, '任务总控台(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (73, '200003', '触发工作流', '7', 72, '71,72', 7, '触发工作流(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (74, '200004', '执行', 'task.workflow.trigger', 73, '71,72,73', 1, '手动触发工作流(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (75, '200005', '投递任务', '7', 72, '71,72', 7, '投递任务(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (76, '200006', '执行', 'task.enqueue', 75, '71,72,75', 1, '手动投递通用任务(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (77, '200007', '查询工作流状态', '7', 134, '71,134', 7, '查询工作流状态(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (78, '200008', '查询', 'task.workflow.status', 77, '71,134,77', 0, '查询工作流状态(查看)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (79, '200009', '查询热加载状态', '7', 135, '71,135', 7, '查询热加载状态(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (80, '200010', '查询', 'task.config.reload.status', 79, '71,135,79', 0, '查询配置热加载状态(查看)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (81, '200011', '手动触发热加载', '7', 135, '71,135', 7, '手动触发热加载(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (82, '200012', '执行', 'task.config.reload.run', 81, '71,135,81', 2, '手动触发配置热加载(修改)', 1, '2026-05-01 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (83, '200013', '任务队列', 'task.queue.list', 71, '71', 5, '任务队列(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (84, '200014', '切换消费', '7', 83, '71,83', 7, '切换任务队列消费状态(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (85, '200015', '暂停', 'task.queue.pause', 84, '71,83,84', 2, '暂停任务队列消费(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (86, '200016', '恢复', 'task.queue.resume', 84, '71,83,84', 2, '恢复任务队列消费(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (87, '200017', '任务列表', 'task.items.list', 71, '71', 5, '任务列表(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (88, '200018', '查看详情', '7', 87, '71,87', 7, '查看任务详情(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (89, '200019', '查询', 'task.info.get', 88, '71,87,88', 0, '查询任务详情(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (90, '200020', '立即执行', '7', 87, '71,87', 7, '立即执行任务(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (91, '200021', '执行', 'task.run', 90, '71,87,90', 2, '立即执行任务(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (92, '200022', '删除任务', '7', 87, '71,87', 7, '删除任务(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (93, '200023', '删除', 'task.delete', 92, '71,87,92', 3, '删除任务(删除)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (94, '200024', '用户标签', 'user_tag.index', 71, '71', 5, '用户标签(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (95, '200025', '触发工作流', '7', 94, '71,94', 7, '触发用户标签工作流(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (96, '200026', '执行', 'user_tag.workflow.trigger', 95, '71,94,95', 1, '触发用户标签工作流(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (97, '200027', '指定标签重算', '7', 94, '71,94', 7, '指定标签重算(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (98, '200028', '执行', 'user_tag.recalculate', 97, '71,94,97', 2, '指定标签重算(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (99, '200029', '接口文档', 'docs.index', 65, '65', 5, '接口文档(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-05 15:14:23');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (100, '200030', '安全调试台', 'security.debug.index', 65, '65', 5, '安全调试台(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-05 15:14:03');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (101, '200031', '签名调试', '7', 100, '1,100', 7, '安全调试签名(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (102, '200032', '执行', 'security.debug.sign', 101, '1,100,101', 1, '安全调试签名接口(新增)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (103, '200033', '验签调试', '7', 100, '1,100', 7, '安全调试验签(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (104, '200034', '执行', 'security.debug.verify', 103, '1,100,103', 0, '安全调试验签接口(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (105, '200035', '加密调试', '7', 100, '1,100', 7, '安全调试加密(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (106, '200036', '执行', 'security.debug.encrypt', 105, '1,100,105', 2, '安全调试加密接口(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (107, '200037', '解密调试', '7', 100, '1,100', 7, '安全调试解密(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (108, '200038', '执行', 'security.debug.decrypt', 107, '1,100,107', 0, '安全调试解密接口(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (109, '100071', '删除', 'admin.delete', 23, '1,23', 3, '删除管理员(按钮,删除)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (110, '100072', '查看详情', 'admin.info', 26, '1,23,26', 0, '查询管理员详情(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (111, '100073', '查看详情', 'secretKey.get', 62, '1,59,62', 0, '查询秘钥详情(查看)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (112, '100074', '导出', 'admin.export', 23, '1,23', 0, '异步导出管理员列表(查看)', 1, '2026-05-05 00:00:00', '2026-05-05 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (113, '100075', '重置首次状态', 'admin.reset.initial_state', 23, '1,23', 2, '重置管理员到首次登录前状态(修改)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (114, '100076', '查询导出进度', 'admin.export.status', 23, '1,23', 0, '查询管理员导出任务进度(查看)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (115, '100077', '下载导出文件', 'admin.export.download', 23, '1,23', 0, '下载管理员导出文件(查看)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (116, '100078', '导出', 'system.config.export', 36, '1,36', 0, '导出系统配置(查看)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (117, '100079', '导入', 'system.config.import', 36, '1,36', 1, '导入系统配置(新增)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:01');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (118, '100080', '预检', 'secretKey.validate', 62, '1,59,62', 0, '预检秘钥配置(查看)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:01');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (119, '100081', '运行态自检', 'secretKey.self_check', 59, '1,59', 2, '执行秘钥自检并刷新缓存(修改)', 1, '2026-05-04 15:20:59', '2026-05-04 15:21:01');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (120, '200039', 'Collector任务', 'collector.task.list', 71, '71', 5, 'Collector任务(菜单,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (121, '200040', '手动执行', '7', 120, '71,120', 7, '手动执行Collector(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (122, '200041', '执行', 'collector.run', 121, '71,120,121', 2, '手动执行Collector(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (123, '200042', '重试任务', '7', 120, '71,120', 7, '重试Collector任务(按钮,页面)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (124, '200043', '执行', 'collector.task.retry', 123, '71,120,123', 2, '手动重试Collector任务(修改)', 1, '2026-05-01 00:00:00', '2026-05-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (131, '100090', '查询角色', 'role.list', 2, '1,2', 0, '查询角色列表(查看)', 1, '2026-05-06 00:00:00', '2026-05-06 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (132, '100091', '查询权限', 'permission.list', 11, '1,11', 0, '查询权限列表(查看)', 1, '2026-05-06 00:00:00', '2026-05-06 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (133, '200044', '查询概览', 'collector.overview', 120, '71,120', 0, '查询Collector概览(查看)', 1, '2026-05-11 00:00:00', '2026-05-11 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (134, '200045', '工作流状态', 'task.workflow.status.index', 71, '71', 5, '工作流状态(菜单,页面)', 1, '2026-05-12 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (135, '200046', '配置热加载', 'task.config.reload.index', 71, '71', 5, '配置热加载(菜单,页面)', 1, '2026-05-12 00:00:00', '2026-05-12 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (136, '200047', '释放互斥锁', '7', 94, '71,94', 7, '释放用户标签工作流互斥锁(按钮,页面)', 1, '2026-05-14 00:00:00', '2026-05-14 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (137, '200048', '执行', 'user_tag.workflow_lease.release', 136, '71,94,136', 2, '释放用户标签工作流互斥锁(修改)', 1, '2026-05-14 00:00:00', '2026-05-14 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (138, '200049', '查询配置项', 'task.config.reload.items', 135, '71,135', 0, '查询配置热加载配置项(查看)', 1, '2026-06-01 00:00:00', '2026-06-01 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (139, '200050', '运行配置', 'runtime.config.index', 71, '71', 5, '运行配置(菜单,页面)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (140, '200051', '查询', 'runtime.config.list', 139, '71,139', 0, '查询运行配置(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (141, '200052', '保存', 'runtime.config.save', 139, '71,139', 2, '保存运行配置草稿(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (142, '200053', '预检', 'runtime.config.validate', 139, '71,139', 0, '预检运行配置(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (143, '200054', '发布', 'runtime.config.publish', 139, '71,139', 2, '发布运行配置(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (144, '200055', '回滚', 'runtime.config.rollback', 139, '71,139', 2, '回滚运行配置(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (145, '200056', '导入', 'runtime.config.import', 139, '71,139', 1, '导入运行配置(新增)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (146, '100092', '前台用户', 'api_user.list', 1, '1', 5, '前台用户管理(菜单,页面)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (147, '100093', '查询', 'api_user.info', 146, '1,146', 0, '查询前台用户详情(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (148, '100094', '新增', 'api_user.add', 146, '1,146', 1, '新增前台用户(新增)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (149, '100095', '编辑', 'api_user.update', 146, '1,146', 2, '编辑前台用户资料(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (150, '100096', '启用/禁用', 'api_user.status.update', 146, '1,146', 2, '启用或禁用前台用户(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (151, '100097', '重置密码', 'api_user.password.reset', 146, '1,146', 2, '重置前台用户密码(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (152, '100098', '同步运行态', 'api_user.runtime.sync', 146, '1,146', 2, '同步前台用户API运行态(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (153, '200057', '查询API热加载', 'api_runtime.config_reload.status', 135, '71,135', 0, '查询API配置热加载状态(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (154, '200058', '触发API热加载', 'api_runtime.config_reload.run', 135, '71,135', 2, '手动触发API配置热加载(修改)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (155, '200059', '运维文档', 'docs.role.ops', 99, '65,99', 0, '访问运维角色文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (156, '200060', '后端开发文档', 'docs.role.backend', 99, '65,99', 0, '访问后端开发角色文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (157, '200061', '前端与测试文档', 'docs.role.frontend', 99, '65,99', 0, '访问前端与测试角色文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (158, '200062', '任务系统文档', 'docs.feature.task', 99, '65,99', 0, '访问任务系统功能文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (159, '200063', '用户标签文档', 'docs.feature.user_tag', 99, '65,99', 0, '访问用户标签功能文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (160, '200064', '接口文档规范', 'docs.api.index', 99, '65,99', 0, '访问接口文档首页和统一规范(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (161, '200065', '后台系统接口文档', 'docs.api.admin', 99, '65,99', 0, '访问后台系统接口文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (162, '200066', '任务系统接口文档', 'docs.api.task', 99, '65,99', 0, '访问任务系统接口文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (163, '200067', '用户标签接口文档', 'docs.api.user_tag', 99, '65,99', 0, '访问用户标签接口文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (164, '200068', '前台API接口规范', 'docs.api_service.index', 99, '65,99', 0, '访问前台API接口文档首页和规范(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
INSERT INTO `admin_permission` (`id`, `uuid`, `title`, `module`, `pid`, `pids`, `type`, `description`, `status`, `created_at`, `updated_at`) VALUES (165, '200069', '前台API接口文档', 'docs.api_service.front', 99, '65,99', 0, '访问前台API前台系统接口文档目录(查看)', 1, '2026-06-16 00:00:00', '2026-06-16 00:00:00');
COMMIT;

-- ----------------------------
-- Table structure for admin_role
-- ----------------------------
DROP TABLE IF EXISTS `admin_role`;
CREATE TABLE `admin_role` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `title` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '角色名称',
  `pid` int unsigned NOT NULL DEFAULT '0' COMMENT '父级ID',
  `pids` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '父级ID(族谱)',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '状态：1正常，0禁用',
  `describe` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '描述',
  `is_delete` tinyint NOT NULL DEFAULT '0' COMMENT '是否删除: 1删除(关联有用户或下级角色不能删除)',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_title` (`title`) USING BTREE,
  KEY `idx_pid` (`pid`)
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='角色';

-- ----------------------------
-- Records of admin_role
-- ----------------------------
BEGIN;
INSERT INTO `admin_role` (`id`, `title`, `pid`, `pids`, `status`, `describe`, `is_delete`, `created_at`, `updated_at`) VALUES (1, '超级管理员', 0, '', 1, '超级管理员', 0, '2022-03-21 12:32:16', '2025-11-29 12:17:11');
COMMIT;

-- ----------------------------
-- Table structure for admin_role_permission_rel
-- ----------------------------
DROP TABLE IF EXISTS `admin_role_permission_rel`;
CREATE TABLE `admin_role_permission_rel` (
  `role_id` bigint unsigned NOT NULL COMMENT '角色ID',
  `permission_id` bigint unsigned NOT NULL COMMENT '权限ID',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`role_id`,`permission_id`),
  KEY `idx_permission_id` (`permission_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='角色-权限关系表';

-- ----------------------------
-- Records of admin_role_permission_rel
-- ----------------------------
BEGIN;
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 139, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 140, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 141, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 142, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 143, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 144, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 145, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 146, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 147, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 148, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 149, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 150, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 151, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 152, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 153, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 154, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 155, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 156, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 157, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 158, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 159, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 160, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 161, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 162, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 163, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 164, '2026-06-16 00:00:00');
INSERT INTO `admin_role_permission_rel` (`role_id`, `permission_id`, `created_at`) VALUES (1, 165, '2026-06-16 00:00:00');
COMMIT;

-- ----------------------------
-- Table structure for admin_role_rel
-- ----------------------------
DROP TABLE IF EXISTS `admin_role_rel`;
CREATE TABLE `admin_role_rel` (
  `user_id` int unsigned NOT NULL COMMENT '用户id',
  `role_id` int unsigned NOT NULL COMMENT '角色id',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (`user_id`,`role_id`) USING BTREE,
  KEY `idx_role_id` (`role_id`) USING BTREE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户角色';

-- ----------------------------
-- Records of admin_role_rel
-- ----------------------------
BEGIN;
INSERT INTO `admin_role_rel` (`user_id`, `role_id`, `created_at`) VALUES (1, 1, '2022-04-05 16:30:56');
COMMIT;

-- ----------------------------
-- Table structure for archive_segment
-- ----------------------------
DROP TABLE IF EXISTS `archive_segment`;
CREATE TABLE `archive_segment` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `job_name` varchar(128) NOT NULL COMMENT '归档任务名',
  `table_name` varchar(128) NOT NULL COMMENT '热表名',
  `history_table_name` varchar(128) NOT NULL COMMENT '历史表名',
  `range_start` datetime(6) NOT NULL COMMENT '区间起点含边界',
  `range_end` datetime(6) NOT NULL COMMENT '区间终点排他边界',
  `status` varchar(16) NOT NULL COMMENT '区间状态，done表示已归档，deleted表示热表已删',
  `worker_id` varchar(128) NOT NULL DEFAULT '' COMMENT '当前持有worker',
  `lease_expires_at` datetime(6) DEFAULT NULL COMMENT '租约过期时间',
  `last_archived_id` bigint NOT NULL DEFAULT '0' COMMENT '最近归档主键游标',
  `last_archived_time` datetime(6) DEFAULT NULL COMMENT '最近归档时间游标',
  `rows_archived` bigint NOT NULL DEFAULT '0' COMMENT '累计归档行数',
  `attempt_count` int unsigned NOT NULL DEFAULT '0' COMMENT '领取次数',
  `error_message` varchar(500) NOT NULL DEFAULT '' COMMENT '失败摘要',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '创建时间',
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) COMMENT '更新时间',
  `completed_at` datetime(6) DEFAULT NULL COMMENT '完成时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_job_range` (`job_name`,`range_start`,`range_end`),
  KEY `idx_job_status_lease` (`job_name`,`status`,`lease_expires_at`),
  KEY `idx_job_range` (`job_name`,`range_start`,`range_end`),
  KEY `idx_history_table` (`history_table_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='归档区间与checkpoint控制表';

-- ----------------------------
-- Records of archive_segment
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for archive_watermark
-- ----------------------------
DROP TABLE IF EXISTS `archive_watermark`;
CREATE TABLE `archive_watermark` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  `job_name` varchar(128) NOT NULL COMMENT '归档任务名',
  `table_name` varchar(128) NOT NULL COMMENT '热表名',
  `watermark_time` datetime(6) DEFAULT NULL COMMENT '已完整复制到历史表的排他上界',
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_job_name` (`job_name`),
  KEY `idx_table_name` (`table_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='归档水位线表';

-- ----------------------------
-- Records of archive_watermark
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for collector_outbox
-- ----------------------------
DROP TABLE IF EXISTS `collector_outbox`;
CREATE TABLE `collector_outbox` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `event_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '事件ID(幂等键)',
  `biz_type` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '业务类型',
  `partition_key` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '分区Key(冲突域)',
  `payload` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '事件负载(JSON)',
  `transport` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'db' COMMENT '来源/载体:kafka|redis|db',
  `state` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '状态:0待处理,1处理中,2完成,3重试,4死信',
  `attempt` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '失败重试次数',
  `next_run_at` timestamp NOT NULL COMMENT '下次可处理时间',
  `started_at` timestamp NULL DEFAULT NULL COMMENT '开始处理时间',
  `finished_at` timestamp NULL DEFAULT NULL COMMENT '结束时间(完成/死信)',
  `last_error` varchar(1000) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '最近一次失败原因',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_event_id` (`event_id`) USING BTREE,
  KEY `idx_biz_state_next` (`biz_type`,`state`,`next_run_at`) USING BTREE,
  KEY `idx_state_next` (`state`,`next_run_at`) USING BTREE,
  KEY `idx_state_started` (`state`,`started_at`) USING BTREE,
  KEY `idx_partition_state_next` (`partition_key`,`state`,`next_run_at`) USING BTREE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='通用收集器Outbox(兜底重试/死信)';

-- ----------------------------
-- Records of collector_outbox
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for secret_key
-- ----------------------------
DROP TABLE IF EXISTS `secret_key`;
CREATE TABLE `secret_key` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uuid` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'API KEY 唯一标识',
  `title` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '接入应用或供应商标题',
  `stable_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '当前稳定生效版本',
  `gray_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '当前灰度版本',
  `gray_percent` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '灰度流量百分比 0-100',
  `gray_salt` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '灰度哈希盐值',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '状态：1 启用，0 禁用',
  `sign_status` tinyint NOT NULL DEFAULT '1' COMMENT '签名验签状态：1启用，0停用',
  `crypto_status` tinyint NOT NULL DEFAULT '1' COMMENT '加密解密状态：1启用，0停用',
  `remark` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '备注',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uuid` (`uuid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='秘钥';

-- ----------------------------
-- Records of secret_key
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for secret_key_version
-- ----------------------------
DROP TABLE IF EXISTS `secret_key_version`;
CREATE TABLE `secret_key_version` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `secret_key_id` int unsigned NOT NULL COMMENT '秘钥主表ID',
  `uuid` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'API KEY 唯一标识',
  `key_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '秘钥版本号',
  `aes_key_ref` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'AES KEY 文件绝对路径',
  `aes_iv_ref` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'AES IV 文件绝对路径',
  `rsa_public_key_user_ref` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '用户 RSA 公钥文件绝对路径',
  `rsa_public_key_server_ref` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '服务器 RSA 公钥路径，可为空并由私钥派生',
  `rsa_private_key_server_ref` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '服务器 RSA 私钥文件绝对路径',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '版本状态：1 启用，0 停用',
  `remark` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '版本备注',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_secret_key_version` (`uuid`,`key_version`),
  KEY `idx_secret_key_id` (`secret_key_id`),
  KEY `idx_secret_key_uuid` (`uuid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='秘钥版本';

-- ----------------------------
-- Records of secret_key_version
-- ----------------------------
BEGIN;
COMMIT;

-- ----------------------------
-- Table structure for sys_config
-- ----------------------------
DROP TABLE IF EXISTS `sys_config`;
CREATE TABLE `sys_config` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uuid` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '配置唯一标识,命名规则(驼峰)：项目名+key',
  `title` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '配置标题',
  `type` tinyint unsigned NOT NULL DEFAULT '1' COMMENT '展示和校验类型：0 仅做分组标题（配置归类）; 1 Object; 2 Array; 3 String; 4 Integer; 5 Float; 6 Boolean（0 = false，1 = true）; ',
  `value` json NOT NULL COMMENT '配置值(JSON 格式，可为string/number/bool/array/object)',
  `example` json NOT NULL COMMENT '配置示例，帮助说明结构',
  `remark` varchar(255) NOT NULL DEFAULT '' COMMENT '备注',
  `page` varchar(200) NOT NULL DEFAULT '' COMMENT '配置项所属页面路径，例如 /system/config/base',
  `pid` int unsigned NOT NULL DEFAULT '0' COMMENT '上级ID',
  `pids` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT '上级ID(族谱)',
  `version` int unsigned NOT NULL DEFAULT '0' COMMENT '版本号',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_uuid` (`uuid`)
) ENGINE=InnoDB AUTO_INCREMENT=10 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='参数配置表';

-- ----------------------------
-- Records of sys_config
-- ----------------------------
BEGIN;
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (1, 'adminVerifyIpConfig', 'Admin验证IP配置', 0, '0', '0', 'Admin验证IP配置', '', 0, '', 0, '2025-11-26 10:46:24', '2025-11-29 12:17:50');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (2, 'adminIpWhitelistDisable', 'Admin启用IP白名单', 6, '1', '1', '[生产建议配置：1 启用] 禁用后台IP白名单：1启用；0 禁用', '', 1, '1', 0, '2025-11-26 10:49:01', '2025-11-29 12:17:54');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (3, 'adminIpWhitelist', 'AdminIP白名单', 2, '[]', '[\"8.8.8.8\", \"127.0.0.1\"]', 'IP白名单: 多个IP以英文逗号分割', '', 1, '1', 0, '2025-11-26 10:58:40', '2026-05-06 15:42:51');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (4, 'adminCheckChangeIp', 'Admin验证IP是否变更', 6, '1', '1', '[生产建议配置：1 验证] 验证IP是否变更：1 验证； 0 不验证', '', 1, '1', 0, '2025-11-26 11:08:37', '2025-11-29 12:18:01');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (5, 'adminVerifyMFAConfig', 'Admin验证MFA配置', 0, '0', '0', 'Admin验证MFA配置', '', 0, '', 0, '2025-11-26 11:19:16', '2025-11-29 12:18:09');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (6, 'adminMFACheckEnable', 'Admin校验MFA设备验证码', 6, '0', '1', '[生产建议配置：1 强启用] 强启用MFA设备（身份验证器）登录校验：1 强启用校验（用户设置MFA状态失效）；0 非强启用（默认使用用户设置的MFA状态）', '', 5, '5', 5, '2025-11-26 11:24:42', '2026-05-05 20:11:38');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (7, 'adminMFACheckFrequency', 'Admin校验MFA设备频率', 4, '1800', '300', 'MFA设备校验频率（单位秒），建议配置5分钟(300秒)以上: 0 需要校验的地方每次都校验，大于0 秒在该时间内只不再重复校验（x秒时间内只校验一次）', '', 5, '5', 0, '2025-11-26 11:28:58', '2026-05-05 01:17:21');
INSERT INTO `sys_config` (`id`, `uuid`, `title`, `type`, `value`, `example`, `remark`, `page`, `pid`, `pids`, `version`, `created_at`, `updated_at`) VALUES (8, 'adminDisableMFACheckScenario', 'Admin禁用MFA设备校验应用场景', 1, '{}', '{\"1\": \"修改密码\", \"2\": \"修改MFA状态\", \"3\": \"修改MFA秘钥\", \"4\": \"修改管理员状态\", \"5\": \"新增管理员\", \"6\": \"编辑管理员资料/角色\", \"7\": \"后台重置管理员密码\", \"8\": \"后台重置管理员首次状态\", \"9\": \"后台删除管理员\", \"10\": \"释放用户标签工作流互斥锁\", \"11\": \"秘钥管理敏感操作\", \"12\": \"运行配置发布/回滚/导入\", \"13\": \"前台用户管理\", \"14\": \"API运行态热加载\"}', '配置需要跳过 MFA 二次校验的敏感操作场景；默认空对象，不跳过。', '', 5, '5', 0, '2025-11-26 11:36:14', '2026-06-16 00:00:00');
COMMIT;

SET FOREIGN_KEY_CHECKS = 1;
