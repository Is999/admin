-- Collector 处理中幂等 key 安全释放脚本。
-- 调用方：internal/infra/collectorx.idempotencyStore.release；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
