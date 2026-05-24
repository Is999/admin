# 仓库检查面

按当前仓库类型选择检查面，不要求仓库具备不存在的技术面。

## Go 后端

声明后端需求已闭环前，检查当前仓库实际存在且受影响的面：

- 外部入口：route、handler、CLI command、scheduler、worker、consumer、plugin 或 migration。
- 核心路径：logic/service、repository/model、事务边界、cache 行为、error/log 传播。
- 数据资产：GORM model、GORM 链式 query 形态、raw SQL 例外理由、SQL template、Lua script、config YAML、embedded assets、table/index 假设。
- 契约：request/response type、business code、i18n message、permission code、RouteMeta、security policy 和接口文档。
- 运维面：metrics/logs、retry/idempotency、query plan、scan range、batch 或 pagination 边界、timeout/concurrency、DB-pressure 控制、cache invalidation、migration/backfill/compensation 需求。
- 测试：聚焦 unit/integration test、package test、static scan 和 `git diff --check`。


## 跨仓库工作

- 传播模式前，逐文件比较兄弟仓库。
- 重新扫描当前仓库，不假设兄弟 worktree 或复制模块完全一致。
- 除非用户明确要求拆分，保留共享 cache/API 契约。
