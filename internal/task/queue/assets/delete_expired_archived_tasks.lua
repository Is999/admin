-- 代码资产：按 archived zset 分数清理过期归档失败任务。
-- 调用方：internal/task/queue.Manager.deleteExpiredArchivedTasks；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：asynq:{queue}:archived
-- ARGV[1]：过期分数上界，Unix 秒
-- ARGV[2]：任务 hash key 前缀，例如 asynq:{queue}:t:
-- ARGV[3]：本轮最大清理数量

local ids = redis.call("ZRANGEBYSCORE", KEYS[1], "-inf", ARGV[1], "LIMIT", 0, tonumber(ARGV[3]))
for _, id in ipairs(ids) do
	local task_key = ARGV[2] .. id
	local unique_key = redis.call("HGET", task_key, "unique_key")
	if unique_key and unique_key ~= "" and redis.call("GET", unique_key) == id then
		redis.call("DEL", unique_key)
	end
	redis.call("DEL", task_key)
	redis.call("ZREM", KEYS[1], id)
end
return table.getn(ids)
