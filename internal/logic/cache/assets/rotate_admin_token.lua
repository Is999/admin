-- 代码资产：原子轮换管理员会话 token，并续写当前会话 TTL。
-- 调用方：internal/logic/cache.RotateAdminToken；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员缓存精确 key，来自 common/rediskeys.AdminInfoRedisKey。
-- ARGV[1]：当前请求携带的旧 token，必须与缓存 token 完全一致。
-- ARGV[2]：本次签发的新 token。
-- ARGV[3]：当前请求 token 的 SHA-256 摘要。
-- ARGV[4]：上一枚 token 的刷新幂等宽限秒数。
-- ARGV[5]：新会话 TTL 秒数。
-- 原子性边界：缓存不存在或 token 已失效时返回空字符串；宽限期重试直接返回当前 token。
if redis.call("EXISTS", KEYS[1]) ~= 1 then
    return ""
end

local current_token = redis.call("HGET", KEYS[1], "token")
if not current_token then
    return ""
end

local redis_time = redis.call("TIME")
local now_seconds = tonumber(redis_time[1])
if current_token ~= ARGV[1] then
    local previous_digest = redis.call("HGET", KEYS[1], "refreshPreviousTokenDigest")
    local grace_until = tonumber(redis.call("HGET", KEYS[1], "refreshGraceUntil") or "0")
    if previous_digest == ARGV[3] and grace_until >= now_seconds then
        return current_token
    end
    return ""
end

redis.call(
    "HSET",
    KEYS[1],
    "token", ARGV[2],
    "refreshPreviousTokenDigest", ARGV[3],
    "refreshGraceUntil", now_seconds + tonumber(ARGV[4])
)
redis.call("EXPIRE", KEYS[1], ARGV[5])
return ARGV[2]
