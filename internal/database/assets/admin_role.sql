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

 Date: 06/05/2026 16:03:36
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
