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

 Date: 06/05/2026 16:06:03
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
