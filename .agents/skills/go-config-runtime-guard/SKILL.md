---
name: go-config-runtime-guard
description: "审查 Go 配置和运行时边界。用于 config struct、YAML/env、默认值、normalize/getter、hot_reload、feature flag、重启边界、配置不生效。"
---

# Go 配置运行时护栏

## 工作流程

1. 追踪配置来源链路：结构体 tag、默认值、normalize/getter、YAML 样例、Helm values、env override、文档和 runtime reload。
2. 区分运行时参数和启动期注册；除非源码证明，不要声称 hot reload 能重建路由、listener、DB client、Redis client、Kafka、task plugin 或 worker 拓扑。
3. 除非用户明确要求变更契约，否则保持外部 YAML/API 结构稳定。
4. 描述 `0`、空字符串、`false` 行为前，先走仓库的 normalize/getter 逻辑。
5. feature flag 要同时追 worker 行为和管理/API/前端入口；worker 关闭不代表路由不可见。
6. 为默认值、零值安全、reload merge 和 restart-required 输出补测试。
7. 同步配置样例、文档、API 响应、前端展示和运维说明。

## 验证

- 运行 config、bootstrap、svc、logic 或 task 等相关包测试。
- 检查 YAML 样例和 Helm values 是否漂移。
- 运行 `git diff --check` 并检查 `git status --short`。

## 交付证据

说明配置来源、运行时与需重启边界、默认值验证、测试结果、文档更新，以及上线需要重启还是 reload。
