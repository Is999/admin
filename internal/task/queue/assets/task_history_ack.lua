-- KEYS[1] pending events hash
-- KEYS[2] pending order zset
-- KEYS[3] collector status hash
-- ARGV contains persisted event ids

if #ARGV == 0 then
  return 0
end
local removedBytes = 0
for _, eventID in ipairs(ARGV) do
  removedBytes = removedBytes + redis.call('HSTRLEN', KEYS[1], eventID)
end
redis.call('HDEL', KEYS[1], unpack(ARGV))
local removed = redis.call('ZREM', KEYS[2], unpack(ARGV))
local pendingBytes = tonumber(redis.call('HGET', KEYS[3], 'pendingBytes') or '0')
if redis.call('ZCARD', KEYS[2]) == 0 then
  pendingBytes = 0
else
  pendingBytes = math.max(pendingBytes - removedBytes, 0)
end
redis.call('HSET', KEYS[3], 'pendingBytes', pendingBytes)
return removed
