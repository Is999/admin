-- 代码资产：释放用户标签全局写租约，保障 full/delta/targeted/recalculate 写入互斥。
-- 调用方：internal/jobs/usertag/repository.TagRepository.releaseWorkflowLeaseOwner；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：用户标签写租约精确 key，来自 UserTagWorkflowLeaseKeyPrefix 和 app_id。
-- ARGV[1]：workflowID|mode owner，由 AcquireWorkflowLease 生成并传递到释放链路。
-- 原子性边界：仅 owner 完全匹配才 DEL，避免误删其他模式或新工作流持有的租约。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
