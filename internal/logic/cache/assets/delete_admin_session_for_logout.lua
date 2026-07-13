-- 代码资产：原子校验退出 token 并删除管理员登录态。
-- 调用方：internal/logic/cache.DeleteAdminSessionForLogout；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员缓存精确 key，来自 common/rediskeys.AdminInfoRedisKey。
-- ARGV[1]：当前请求携带的 token。
-- ARGV[2]：当前请求 token 的 SHA-256 摘要。
-- 并发边界：允许本轮刷新前 token 完成退出；重新登录会清空宽限字段，旧请求不得删除新会话。
if redis.call("EXISTS", KEYS[1]) ~= 1 then
    return 0
end

local current_token = redis.call("HGET", KEYS[1], "token")
if current_token == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end

local previous_digest = redis.call("HGET", KEYS[1], "refreshPreviousTokenDigest")
if previous_digest ~= ARGV[2] then
    return 0
end

local redis_time = redis.call("TIME")
local grace_until = tonumber(redis.call("HGET", KEYS[1], "refreshGraceUntil") or "0")
if grace_until < tonumber(redis_time[1]) then
    return 0
end
return redis.call("DEL", KEYS[1])
