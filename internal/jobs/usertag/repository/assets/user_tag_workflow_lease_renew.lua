-- 代码资产：续租用户标签全局写租约，长批次计算期间保持写入互斥。
-- 调用方：internal/jobs/usertag/repository.renewWorkflowLease；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：用户标签写租约精确 key，来自 UserTagWorkflowLeaseRedisKey。
-- ARGV[1]：workflowID|mode owner，由当前工作流租约持有方传入。
-- ARGV[2]：TTL 秒数，来自 userTagWorkflowLeaseTTL 换算后的正整数。
-- 原子性边界：仅 owner 匹配才 EXPIRE；续租失败由调用方按 workflow_id/mode/shard 记录并降级退出。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("EXPIRE", KEYS[1], ARGV[2])
end
return 0
