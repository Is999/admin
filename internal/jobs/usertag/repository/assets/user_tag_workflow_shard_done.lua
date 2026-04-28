-- 代码资产：记录用户标签工作流分片完成集合，用于判断非 full 模式所有分片是否结束。
-- 调用方：TagRepository.ReleaseWorkflowLeaseAfterShardDone；发送 Redis 前会剥离本说明。
-- KEYS[1]：workflow sync_done 精确 key，来自 UserTagWorkflowSyncDoneRedisKey。
-- ARGV[1]：shardIndex，由当前完成分片传入。
-- ARGV[2]：集合 TTL 秒数，来自 userTagWorkflowLeaseTTL 换算后的正整数。
-- 原子性边界：只 SADD 当前 workflow 的分片号并刷新 TTL，不扫描或写入其他工作流 key。
redis.call("SADD", KEYS[1], ARGV[1])
redis.call("EXPIRE", KEYS[1], ARGV[2])
return redis.call("SCARD", KEYS[1])
