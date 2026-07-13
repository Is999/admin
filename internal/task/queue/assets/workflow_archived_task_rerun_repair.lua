-- 代码资产：修复归档任务重跑前的工作流节点失败计数，避免 archived 分片重跑后状态残留。
-- 调用方：internal/task/queue.Manager.prepareWorkflowArchivedTaskRerun；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：工作流节点 hash 精确 key，来自 workflowID 和 node。
-- ARGV[1]：分片标记字段名，由 shardIndex 生成。
-- ARGV[2]：分片业务失败次数字段名。
-- ARGV[3]：更新时间文本，由调用方按 RFC3339 生成。
-- 原子性边界：success/skipped 节点不回滚；失败分片改为 rerun_prepared，使 RunTask 失败后可安全重试。
local status = redis.call("HGET", KEYS[1], "status")
if status == "success" or status == "skipped" then
    return -1
end

local marker = redis.call("HGET", KEYS[1], ARGV[1])
if marker ~= "failed" and marker ~= "rerun_prepared" then
    return -2
end

local prepared = marker == "rerun_prepared"
if not prepared then
    redis.call("HSET", KEYS[1], ARGV[1], "rerun_prepared")
    local failed = tonumber(redis.call("HGET", KEYS[1], "failed") or "0")
    if failed > 0 then
        redis.call("HINCRBY", KEYS[1], "failed", -1)
    end
end
redis.call("HDEL", KEYS[1], ARGV[2])
redis.call("HSET", KEYS[1], "status", "running", "errorMessage", "", "finishedAt", "", "updatedAt", ARGV[3])
redis.call("PERSIST", KEYS[1])
if prepared then
    return 0
end
return 1
