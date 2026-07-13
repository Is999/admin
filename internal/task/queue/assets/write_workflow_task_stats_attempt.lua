-- 代码资产：仅允许当前 attempt 更新工作流分片处理量，防止迟到实例覆盖新重试指标。
-- 调用方：internal/task/queue.Manager.recordWorkflowTaskStats；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流节点 hash。
-- ARGV[1]：分片 attempt token 字段。
-- ARGV[2]：当前 attempt token。
-- ARGV[3]：分片处理量字段。
-- ARGV[4]：分片处理量 JSON。
-- 返回：1 表示写入成功，0 表示 attempt 已过期且未写入。
if redis.call("HGET", KEYS[1], ARGV[1]) ~= ARGV[2] then
    return 0
end

redis.call("HSET", KEYS[1], ARGV[3], ARGV[4])
return 1
