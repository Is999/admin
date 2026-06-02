-- 用途：用户标签结果基表重算临时表。
-- 依赖：先创建 user_tag_0，保持临时表结构与基表一致。

CREATE TABLE IF NOT EXISTS `user_tag_0_tmp` LIKE `user_tag_0`;
