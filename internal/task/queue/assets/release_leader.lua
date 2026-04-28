-- 代码资产：释放任务调度 leader 锁，服务停机或失去调度权时使用。
-- 调用方：internal/task/queue.LeaderRunner.release；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：集中定义的 leader 精确 key，来自 taskqueue leader 配置。
-- ARGV[1]：当前实例 ID，由 LeaderRunner 启动时生成或注入。
-- 原子性边界：仅当锁值与实例 ID 完全一致才 DEL，避免误删其他实例新获得的 leader 锁。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
