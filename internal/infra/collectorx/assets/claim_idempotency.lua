-- Collector Redis 幂等占用：只有空 key 可领取，终态可跳过，处理中和非法状态必须重试或报错。
local current = redis.call("GET", KEYS[1])
if not current then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
if current == ARGV[3] or current == ARGV[4] then
  return 0
end
if string.sub(current, 1, string.len(ARGV[5])) == ARGV[5] then
  return -1
end
return -2
