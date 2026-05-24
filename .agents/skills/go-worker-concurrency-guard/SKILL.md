---
name: go-worker-concurrency-guard
description: "审查 Go 后台任务并发安全。用于 worker、scheduler、queue、Kafka/Asynq、cron、lock、retry、timeout、幂等、归档和告警。"
---

# Go Worker 并发护栏

## 工作流程

1. 确认真实入口：scheduler、worker、consumer、task plugin、queue handler、CLI 或维护路由。
2. 端到端追状态转换：claim、lock、execute、retry、success、failure、archive、cleanup、alert、compensation。
3. 核对幂等 key、事务边界、lock owner 校验、TTL、retry/backoff、deadline 和 cancellation。
4. 区分可预期的窗口耗尽和真正终态失败；只对需要人工介入的状态告警。
5. 避免无界并发、长事务、超大 batch、忽略 context deadline 的循环。
6. 保持已注册 worker/plugin 的重启和热加载边界清晰。
7. 为 retrying vs terminal、锁竞争、超时路径、幂等重跑和 cleanup 补测试。

## 验证

- 先跑 worker/task 相关包聚焦测试。
- 共享状态、goroutine、锁或 cancellation 变化时跑 race 测试。
- 变更队列契约或公共 helper 时扩大测试范围。
- 运行 `git diff --check` 并检查 `git status --short`。

## 交付证据

说明入口、状态机影响、并发上限、幂等/锁行为、重试/告警语义、测试结果、跳过检查和补偿/重放需求。
