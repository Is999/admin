-- 代码资产：原子确认或续期工作流唯一键，避免 owner 检查与续期之间发生抢占。
-- 调用方：internal/task/queue.Manager.ensureWorkflowUnique；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：workflow 唯一精确 key，来自 workflow 名称和业务唯一键。
-- ARGV[1]：当前 workflowID。
-- ARGV[2]：唯一键 TTL，毫秒。
-- 返回：1 表示当前实例持有或成功抢占空 key，0 表示 key 已由其它实例持有。
local owner = redis.call("GET", KEYS[1])
if owner == ARGV[1] then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 1
end
if owner then
    return 0
end
if redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2], "NX") then
    return 1
end
return 0
