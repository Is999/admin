-- 代码资产：续租任务调度 leader 锁，保持单实例周期调度权。
-- 调用方：internal/task/queue.LeaderRunner.Start；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：集中定义的 leader 精确 key，来自 taskqueue leader 配置。
-- ARGV[1]：实例 ID，由当前 LeaderRunner 持有。
-- ARGV[2]：毫秒 TTL，来自 LeaderRunner 锁配置。
-- 原子性边界：仅 owner 匹配时 PEXPIRE；续租失败由调度器释放本地执行权。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
