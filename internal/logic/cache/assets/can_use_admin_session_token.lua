-- 代码资产：校验管理员 token 是否可进入刷新或退出路由。
-- 调用方：internal/logic/cache.CanUseAdminSessionToken；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员缓存精确 key，来自 common/rediskeys.AdminInfoRedisKey。
-- ARGV[1]：当前请求携带的原 token。
-- ARGV[2]：当前请求 token 的 SHA-256 摘要。
-- 安全边界：仅 auth.refresh/auth.logout 调用；普通业务接口仍只接受缓存中的当前 token。
if redis.call("EXISTS", KEYS[1]) ~= 1 then
    return 0
end

local current_token = redis.call("HGET", KEYS[1], "token")
if current_token == ARGV[1] then
    return 1
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
return 1
