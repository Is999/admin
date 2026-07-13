-- 仅当安全缓存阻断键仍与读取快照一致时清理，避免覆盖并发写入的新阻断状态。
local expected_exists = ARGV[1] == "1"
local current_exists = redis.call("EXISTS", KEYS[1]) == 1

if not expected_exists then
    if current_exists then
        return 0
    end
    return 1
end

if not current_exists or redis.call("GET", KEYS[1]) ~= ARGV[2] then
    return 0
end

return redis.call("DEL", KEYS[1])
