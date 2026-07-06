-- 雪花 node_id 租约续期脚本。
-- KEYS[1]: node_id 租约 key。
-- ARGV[1]: 当前实例 owner。
-- ARGV[2]: 新 TTL 秒数。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("EXPIRE", KEYS[1], ARGV[2])
end
return 0
