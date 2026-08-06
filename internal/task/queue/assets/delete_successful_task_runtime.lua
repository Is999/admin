-- 代码资产：仅删除当前 attempt 的成功任务运行快照。
-- 调用方：internal/task/queue.Manager.deleteSuccessfulTaskRuntime；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：按队列和 taskID 隔离的运行快照 hash。
-- ARGV[1]：当前 attempt token。
-- 返回：1 表示删除成功，0 表示快照不存在或已属于新 attempt。
if redis.call("HGET", KEYS[1], "attemptToken") ~= ARGV[1] then
    return 0
end
return redis.call("DEL", KEYS[1])
