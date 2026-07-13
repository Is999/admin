-- 代码资产：原子更新管理员会话 hash 中已经存在的受控字段。
-- 调用方：internal/logic/cache.SetAdminInfoFields；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员缓存精确 key，来自 common/rediskeys.AdminInfoRedisKey。
-- ARGV：按 field、value 成对传入，字段由 Go 调用侧白名单约束。
-- 原子性边界：缓存不存在、参数不完整或任一字段不存在时返回 0，不创建 key 且不做部分更新。
if redis.call("EXISTS", KEYS[1]) ~= 1 or #ARGV == 0 or (#ARGV % 2) ~= 0 then
    return 0
end

for index = 1, #ARGV, 2 do
    if redis.call("HEXISTS", KEYS[1], ARGV[index]) ~= 1 then
        return 0
    end
end

redis.call("HSET", KEYS[1], unpack(ARGV))
return 1
