/*
 Navicat Premium Dump SQL

 Source Server         : D-M
 Source Server Type    : MySQL
 Source Server Version : 80034 (8.0.34)
 Source Host           : 127.0.0.1:3307
 Source Schema         : admin_cron

 Target Server Type    : MySQL
 Target Server Version : 80034 (8.0.34)
 File Encoding         : 65001

 Date: 06/05/2026 16:02:57
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
