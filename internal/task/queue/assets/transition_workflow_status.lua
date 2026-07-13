-- 代码资产：原子写入工作流终态，禁止 success/failed 之间相互覆盖。
-- 调用方：internal/task/queue.Manager.completeWorkflow/failWorkflow；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流 meta hash 精确 key。
-- ARGV[1]：目标终态，只允许 success 或 failed。
-- ARGV[2]：RFC3339 更新时间。
-- ARGV[3]：失败摘要，success 时为空字符串。
-- ARGV[4]：meta 状态 TTL 毫秒数。
local current = redis.call("HGET", KEYS[1], "status")
if not current then
    return -2
end
if current == "success" or current == "failed" then
    if current == ARGV[1] then
        redis.call("PEXPIRE", KEYS[1], ARGV[4])
        return 0
    end
    return -1
end

redis.call("HSET", KEYS[1],
    "status", ARGV[1],
    "updatedAt", ARGV[2],
    "finishedAt", ARGV[2],
    "errorMessage", ARGV[3])
redis.call("HDEL", KEYS[1], "manualRerun")
redis.call("PEXPIRE", KEYS[1], ARGV[4])
return 1
