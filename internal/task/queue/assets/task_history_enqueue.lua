-- KEYS[1] pending events hash
-- KEYS[2] pending order zset
-- KEYS[3] collector status hash
-- ARGV[1] event id
-- ARGV[2] event json
-- ARGV[3] unix milliseconds
-- ARGV[4] pending count hard limit
-- ARGV[5] pending payload bytes hard limit
-- ARGV[6] single enqueue trim limit

if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 1 then
  return 0
end

redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[1])
local pendingBytes = tonumber(redis.call('HGET', KEYS[3], 'pendingBytes') or '0') + string.len(ARGV[2])
redis.call('HSET', KEYS[3], 'lastQueuedAtMs', ARGV[3], 'pendingBytes', pendingBytes)

local countLimit = tonumber(ARGV[4])
local bytesLimit = tonumber(ARGV[5])
local trimLimit = tonumber(ARGV[6])
local size = redis.call('ZCARD', KEYS[2])
local droppedIDs = {}
if size > countLimit or pendingBytes > bytesLimit then
  local candidates = redis.call('ZRANGE', KEYS[2], 0, trimLimit - 1)
  for _, eventID in ipairs(candidates) do
    if size <= countLimit and pendingBytes <= bytesLimit then
      break
    end
    local eventBytes = redis.call('HSTRLEN', KEYS[1], eventID)
    pendingBytes = math.max(pendingBytes - eventBytes, 0)
    size = size - 1
    table.insert(droppedIDs, eventID)
  end
end
if #droppedIDs > 0 then
  redis.call('ZREM', KEYS[2], unpack(droppedIDs))
  redis.call('HDEL', KEYS[1], unpack(droppedIDs))
end
-- 正常运行时旧缓冲已满足硬上限；若固定裁剪预算仍不足，撤销本次新事件以守住总载荷。
if size > countLimit or pendingBytes > bytesLimit then
  local eventBytes = redis.call('HSTRLEN', KEYS[1], ARGV[1])
  if eventBytes > 0 then
    redis.call('ZREM', KEYS[2], ARGV[1])
    redis.call('HDEL', KEYS[1], ARGV[1])
    pendingBytes = math.max(pendingBytes - eventBytes, 0)
    size = size - 1
    table.insert(droppedIDs, ARGV[1])
  end
end
if size <= 0 then
  pendingBytes = 0
end
redis.call('HSET', KEYS[3], 'pendingBytes', pendingBytes)
if #droppedIDs > 0 then
  redis.call('HINCRBY', KEYS[3], 'dropped', #droppedIDs)
end
return 1 + #droppedIDs
