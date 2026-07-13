-- 代码资产：原子更新管理员会话 hash 中已经存在的受控字段。
-- 调用方：internal/logic/cache.SetAdminSessionFields；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员会话精确 key，来自 common/rediskeys.AdminSessionRedisKey。
-- ARGV：按 field、value 成对传入，字段由 Go 调用侧白名单约束。
-- 原子性边界：缓存不存在时返回 0；字段不完整时删除异常会话并返回 -1；参数不完整时返回 -2。
if #ARGV == 0 or (#ARGV % 2) ~= 0 then
    return -2
end

if redis.call("EXISTS", KEYS[1]) ~= 1 then
    return 0
end

for index = 1, #ARGV, 2 do
    if redis.call("HEXISTS", KEYS[1], ARGV[index]) ~= 1 then
        redis.call("DEL", KEYS[1])
        return -1
    end
end

redis.call("HSET", KEYS[1], unpack(ARGV))
return 1
