-- 原子写入管理员 MFA 二次票据，并只延长 Hash TTL，避免较短的新票据提前淘汰仍有效的旧票据。
redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])

local requestedTTL = tonumber(ARGV[3])
local currentTTL = redis.call("PTTL", KEYS[1])
if requestedTTL > 0 and currentTTL < requestedTTL then
    redis.call("PEXPIRE", KEYS[1], requestedTTL)
end

return 1
