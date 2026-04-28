-- 代码资产：记录工作流节点分片结果，用于原子去重并维护 succeeded/failed 计数。
-- 调用方：internal/task/queue.Manager.markTaskSuccess/markTaskFailure；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流节点 hash 精确 key，来自 workflowID 和 node。
-- ARGV[1]：分片标记字段名，由 shardIndex 生成。
-- ARGV[2]：节点状态 TTL 毫秒数，来自 workflowRetention。
-- ARGV[3]：受控 outcome 字段，只允许 succeeded 或 failed。
-- 原子性边界：只更新当前节点 hash；成功分片作为终态，迟到失败不会回滚已成功计数。
local previous = redis.call("HGET", KEYS[1], ARGV[1])
local outcome = ARGV[3]
if previous == outcome then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 0
end

-- 分片成功是终态；迟到的失败回执只能视为重复噪声，不能把已完成节点重新打回失败。
if previous == "succeeded" and outcome == "failed" then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 0
end

local opposite = "failed"
if outcome == "failed" then
    opposite = "succeeded"
end

if previous == opposite then
    local opposite_count = tonumber(redis.call("HGET", KEYS[1], opposite) or "0")
    if opposite_count > 0 then
        redis.call("HINCRBY", KEYS[1], opposite, -1)
    end
end

redis.call("HSET", KEYS[1], ARGV[1], outcome)
redis.call("HINCRBY", KEYS[1], outcome, 1)
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
