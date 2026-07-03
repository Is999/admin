CREATE TABLE IF NOT EXISTS `secret_key` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
  `uuid` varchar(64) NOT NULL DEFAULT '' COMMENT 'API KEY 唯一标识',
  `title` varchar(100) NOT NULL DEFAULT '' COMMENT '接入应用或供应商标题',
  `stable_version` varchar(64) NOT NULL DEFAULT '' COMMENT '当前稳定生效版本',
  `gray_version` varchar(64) NOT NULL DEFAULT '' COMMENT '当前灰度版本',
  `gray_percent` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '灰度流量百分比 0-100',
  `gray_salt` varchar(64) NOT NULL DEFAULT '' COMMENT '灰度哈希盐值',
  `status` tinyint NOT NULL DEFAULT '1' COMMENT '状态：1 启用，0 禁用',
  `sign_status` tinyint NOT NULL DEFAULT '1' COMMENT '签名验签状态：1启用，0停用',
  `crypto_status` tinyint NOT NULL DEFAULT '1' COMMENT '加密解密状态：1启用，0停用',
  `remark` varchar(255) NOT NULL DEFAULT '' COMMENT '备注',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '修改时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uuid` (`uuid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='秘钥';
