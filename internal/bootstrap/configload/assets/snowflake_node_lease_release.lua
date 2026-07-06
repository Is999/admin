-- 雪花 node_id 租约释放脚本。
-- KEYS[1]: node_id 租约 key。
-- ARGV[1]: 当前实例 owner。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
