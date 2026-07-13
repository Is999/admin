# admin 项目边界

## 真实结构

- 外部入口从路由、CLI、scheduler、Asynq worker、Kafka consumer 或 bootstrap component 开始，沿 `handler/logic`、`jobs`、`internal/task`、`internal/infra` 追到 DB、Redis、MQ、文件或告警副作用。
- 运行时接线集中在 `internal/bootstrap`、`internal/svc` 和 `internal/config`；不要再建平行容器或第二套生命周期框架。
- 队列、Collector、CDC、归档和周期任务是长生命周期并发组件。租约、幂等、原子状态迁移、停止期限和 readiness 是已存在的生产边界。
- 数据访问优先沿现有 GORM model；原生 SQL、Lua 和嵌入资产只用于 GORM 无法安全表达、需要数据库锁或 Redis 原子迁移的场景。
- 新增或重构的变量、常量、方法、结构体、字段、map 字段、路由、GORM Model、接口和关键逻辑必须按项目规范补简洁中文注释。

## 简化重点

- `internal/task/queue`：主流程应直接呈现依赖检查、分片投递、结果归并和终态；Go 可做提前返回，Lua CAS 必须权威复核原子迁移前置条件，两处不维护相互冲突的状态规则。
- `internal/infra/collectorx`：批量、分区顺序、失败账本和幂等只保留一套生产实现；没有调用方的并行框架直接删除。
- `internal/jobs/archive`：复制、删除、checkpoint 和租约围绕批次循环就地表达；不要为一种任务建立通用工作流框架。
- `internal/bootstrap`：组件只负责注册、启动、停止和运行态接线；业务逻辑下沉到已有实现，不增加转发层。
- 已纳入 readiness 契约且当前启用的组件必须探测真正依赖的底层能力，但检查函数保持有界、无重查询、无业务副作用。
- 高频接口和 worker 循环先减少 DB/Redis/MQ 往返；错误、告警和启动路径以清晰为先。
- 冷路径不以难读的微优化换取理论收益；简单直接的 Go 代码优先交给编译器内联和逃逸分析。

## 验证

按改动风险选择并在最终候选版本运行：

- 有代码改动时运行 `gofmt -w <修改的 Go 文件>`；只读审查只检查 `gofmt -d` 或 `gofmt -l`
- `go test ./...`
- `go vet ./...`
- 并发组件补相关包 `go test -race`
- 迁移改动补 `make integration-test`
- Prometheus 规则或指标改动补 `make promtool-check`
- `git diff --check`、`git diff --cached --check`、`git status --short`
