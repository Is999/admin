-- KEYS[1] pending events hash
-- KEYS[2] pending order zset
-- KEYS[3] collector status hash
-- ARGV[1] 本次隔离的非法事件数
-- ARGV[2..] 待确认事件 ID

if #ARGV < 2 then
  return 0
end
local dropped = math.max(tonumber(ARGV[1]) or 0, 0)
local removedBytes = 0
local eventIDs = {}
for index = 2, #ARGV do
  local eventID = ARGV[index]
  table.insert(eventIDs, eventID)
  removedBytes = removedBytes + redis.call('HSTRLEN', KEYS[1], eventID)
end
redis.call('HDEL', KEYS[1], unpack(eventIDs))
local removed = redis.call('ZREM', KEYS[2], unpack(eventIDs))
local pendingBytes = tonumber(redis.call('HGET', KEYS[3], 'pendingBytes') or '0')
if redis.call('ZCARD', KEYS[2]) == 0 then
  pendingBytes = 0
else
  pendingBytes = math.max(pendingBytes - removedBytes, 0)
end
redis.call('HSET', KEYS[3], 'pendingBytes', pendingBytes)
if dropped > 0 then
  redis.call('HINCRBY', KEYS[3], 'dropped', dropped)
end
return removed
