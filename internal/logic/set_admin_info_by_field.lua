-- 代码资产：按字段更新管理员信息 hash 缓存，用于资料变更后精确刷新单个字段。
-- 调用方：internal/logic.setAdminInfoByFieldScript；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：管理员缓存精确 key，来自 common/rediskeys.AdminInfoByID 并叠加站点物理前缀。
-- ARGV[1]：受控字段名，由 Go 调用侧白名单字段传入。
-- ARGV[2]：字段新值，由资料变更链路序列化后传入。
-- 原子性边界：仅当 hash 和字段已存在才 HSET，不创建新 key 或新增未知字段。
if redis.call("EXISTS", KEYS[1]) == 1 then
    if redis.call("HEXISTS", KEYS[1], ARGV[1]) == 1 then
        redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
        return 1
    else
        return 0
    end
else
    return 0
end
