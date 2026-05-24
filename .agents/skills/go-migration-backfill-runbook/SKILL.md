---
name: go-migration-backfill-runbook
description: "安全规划 Go 迁移和回填。用于 DDL、表/字段/索引、Model 对齐、历史回填、数据修复、清理、回滚和补偿。"
---

# Go 迁移回填 Runbook

## 工作流程

1. 确认数据契约：源表、目标表、Model、索引、归属、读路径、写路径和 API/前端交付面。
2. DDL 是明确上线资产；不要把运行时表/字段/索引探测放进正常业务路径。
3. 保持 Model、DDL、SQL template、文档和测试在表名、字段类型、默认值、NULL、主键、唯一/索引、时间和软删除语义上对齐。
4. backfill 设计为有边界、可恢复、幂等：cursor 或时间窗口进度、batch 限制、timeout、concurrency、sleep/backoff、可取消。
5. 明确回滚或缓解：停止开关、重放路径、缓存失效、可重复执行和可观测进度。
6. 避免长事务、表锁、无界删除、巨大 `IN/NOT IN` 和 HTTP 在线回填。
7. 可行时补 schema 渲染、Model/DDL 对齐、batch 计划、幂等和失败恢复测试。

## 验证

- 运行 model/repository/task 聚焦测试。
- 迁移查询使用 `$go-db-query-performance-guard` 做性能复核。
- 运行 `git diff --check` 并检查 `git status --short`。

## 交付证据

说明上线顺序、DDL/Model 同步、回填边界、回滚/补偿方案、缓存影响、验证命令，以及是否需要生产执行。
