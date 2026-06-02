-- 用途：用户标签结果表、运行期 UID 和事件 outbox 增加 0-999 shard_no 分片能力。
-- 范围：未发布版本结构迁移；结果表仍按需由运行期 CREATE TABLE LIKE 扩展到 1000 物理分表。

ALTER TABLE `user_tag_0` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_1` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_2` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_3` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_4` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_5` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_6` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_7` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_8` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_9` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);

ALTER TABLE `user_tag_0_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_1_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_2_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_3_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_4_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_5_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_6_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_7_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_8_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_9_tmp` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);

ALTER TABLE `user_tag_sync_0` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_1` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_2` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_3` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_4` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_5` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_6` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_7` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_8` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);
ALTER TABLE `user_tag_sync_9` ADD COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT '取模分片' AFTER `uid`, ADD KEY `idx_shard_uid` (`shard_no`, `uid`), ADD KEY `idx_tag_type_shard_uid` (`tag_type`, `shard_no`, `uid`);

ALTER TABLE `user_tag_runtime_uid` MODIFY COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模分片';
ALTER TABLE `user_tag_event_outbox` MODIFY COLUMN `shard_no` int NOT NULL DEFAULT 0 COMMENT 'uid取模分片';

UPDATE `user_tag_0` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_1` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_2` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_3` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_4` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_5` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_6` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_7` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_8` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_9` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_runtime_uid` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
UPDATE `user_tag_event_outbox` SET `shard_no` = MOD(`uid`, 1000) WHERE `uid` > 0;
