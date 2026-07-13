-- 代码资产：手工重跑 archived 分片前原子重开工作流主状态。
-- 调用方：internal/task/queue.Manager.prepareWorkflowArchivedTaskRerun；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流 meta hash 精确 key。
-- ARGV[1]：RFC3339 更新时间。
-- 原子性边界：只允许 failed 首次重开；后续失败分片仅能复用同一 manualRerun 窗口，success 永不复活。
local current = redis.call("HGET", KEYS[1], "status")
if not current then
    return -3
end
if current == "success" then
    return -1
end
if current == "failed" then
    redis.call("HSET", KEYS[1],
        "status", "running",
        "errorMessage", "",
        "finishedAt", "",
        "updatedAt", ARGV[1],
        "manualRerun", "1")
    redis.call("PERSIST", KEYS[1])
    return 1
end
if current == "running" and redis.call("HGET", KEYS[1], "manualRerun") == "1" then
    redis.call("HSET", KEYS[1], "updatedAt", ARGV[1])
    redis.call("PERSIST", KEYS[1])
    return 0
end
return -2
