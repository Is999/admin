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

 Date: 06/05/2026 16:06:21
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
