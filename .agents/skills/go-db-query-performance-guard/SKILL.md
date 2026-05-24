---
name: go-db-query-performance-guard
description: "审查 Go 数据库查询生产安全。用于 GORM、原生 SQL、索引、读写路由、大表、分页、批量、慢查询、EXPLAIN 和 DB 压力。"
---

# Go 数据库查询性能护栏

## 工作流程

1. 改代码前定位准确查询归属：handler、logic、repository/model、task、SQL template 和文档。
2. MySQL/SQL 优先使用 GORM 链式调用；只有 GORM 无法安全表达或会明显影响性能时才允许原生 SQL。
3. 原生 SQL 必要时必须放入 `*.sql.tmpl`，通过 `go:embed` 加载，执行前剥离文件头说明，并记录例外原因。
4. 确认逻辑库和路由：`ReadDB`/`WriteDB`、事务 `tx`、`dbresolver.Read`/`Write`，以及运行配置是否真的有 replica。
5. 检查查询形状：选择列、谓词、索引顺序、基数、排序/分组/JOIN、`IN` 大小、分页、时间窗口、游标、batch、timeout、concurrency、retry/backoff。
6. 阻止危险在线路径：无界全表扫描、深分页 `OFFSET`、`SELECT *`、无索引排序/分组/JOIN、巨大 `IN/NOT IN`、运行时 schema 探测、HTTP 内重聚合。
7. 大聚合、修复、导出、对账优先使用后台任务、汇总表、ClickHouse/数仓、队列或受控维护任务。
8. 查询行为、索引、SQL 资产或路由假设变化时，同步文档、测试、配置样例和运维说明。

## 验证

- 用户给出 SQL/log 时，先精确搜索原文，再扩展到片段和生成函数。
- 先跑查询归属包的聚焦测试，风险或仓库健康允许时再扩大范围。
- 共享查询 helper 或并发路径变动时，补 `go vet`、race 或 compile-only 检查。
- 只有安全的本地/测试 DB 可用时才跑 `EXPLAIN`；不可用时说明缺失证据和预期索引/范围推理。
- 交付前运行 `git diff --check` 并检查 `git status --short`。

## 交付证据

说明 GORM 链式选择或原生 SQL 例外、索引依据、预估扫描范围、分页/批量上限、超时/并发控制、DB 压力风险、已跑测试和跳过的 DB 计划验证。
