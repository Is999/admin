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

 Date: 06/05/2026 16:06:42
*/

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

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

SET FOREIGN_KEY_CHECKS = 1;
