-- Collector Redis 幂等状态 CAS：只有当前值仍属于预期领取者时才允许续租或写入终态。
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
return 1
