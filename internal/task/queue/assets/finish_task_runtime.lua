-- 代码资产：仅允许当前 attempt 回写任务运行终态，防止租约丢失后迟到实例覆盖新实例。
-- 调用方：internal/task/queue.Manager.finishTaskRuntime；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：按队列和 taskID 隔离的运行快照 hash。
-- ARGV[1]：当前 attempt token。
-- ARGV[2]：终态 TTL，毫秒。
-- ARGV[3]：需清理的上一轮终态字段数量。
-- 后续参数：先传待清理字段，再传 HSET field/value 对。
-- 返回：1 表示写入成功，0 表示 attempt 已过期且未写入。
if redis.call("HGET", KEYS[1], "attemptToken") ~= ARGV[1] then
    return 0
end

local clear_count = tonumber(ARGV[3])
if not clear_count or clear_count < 0 then
    return redis.error_reply("invalid clear field count")
end
if clear_count > 0 then
    local clear_fields = {}
    for index = 1, clear_count do
        clear_fields[index] = ARGV[3 + index]
    end
    redis.call("HDEL", KEYS[1], unpack(clear_fields))
end

local values_start = 4 + clear_count
if values_start <= #ARGV then
    redis.call("HSET", KEYS[1], unpack(ARGV, values_start))
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
