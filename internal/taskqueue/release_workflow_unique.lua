-- 代码资产：释放工作流唯一键，工作流结束后解除同实例重复触发保护。
-- 调用方：internal/taskqueue.Manager.releaseWorkflowUniqueReservation；go:embed 加载后由 embedasset 剥离本说明再发送 Redis。
-- KEYS[1]：workflow 唯一精确 key，来自 workflow 名称和业务唯一键。
-- ARGV[1]：当前 workflowID，由预占唯一键的工作流实例传入。
-- 原子性边界：仅 owner workflowID 匹配时 DEL，避免误删其他并发触发实例的唯一键。
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
