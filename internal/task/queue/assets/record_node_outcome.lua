-- 代码资产：记录工作流节点分片结果，用于原子去重并维护 succeeded/failed 计数。
-- 调用方：internal/task/queue.Manager.markTaskSuccess/markTaskFailure；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流节点 hash 精确 key，来自 workflowID 和 node。
-- ARGV[1]：分片标记字段名，由 shardIndex 生成。
-- ARGV[2]：受控 outcome 字段，只允许 succeeded 或 failed。
-- 原子性边界：只更新当前节点 hash；成功和失败均为单调终态，失败只能由显式 archived rerun repair 清除。
local previous = redis.call("HGET", KEYS[1], ARGV[1])
local outcome = ARGV[2]
if outcome ~= "succeeded" and outcome ~= "failed" then
    return -1
end

if previous then
    if previous == outcome then
        return 0
    end
    if previous == "rerun_prepared" then
        redis.call("HSET", KEYS[1], ARGV[1], outcome)
        redis.call("HINCRBY", KEYS[1], outcome, 1)
        return 1
    end
    return -2
end

redis.call("HSET", KEYS[1], ARGV[1], outcome)
redis.call("HINCRBY", KEYS[1], outcome, 1)
return 1
