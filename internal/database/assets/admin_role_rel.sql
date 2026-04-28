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

 Date: 06/05/2026 16:04:13
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
