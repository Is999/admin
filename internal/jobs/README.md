# 任务业务目录

`internal/jobs` 用于收口定时任务、工作流节点和对应仓储实现。后续新增独立任务时，优先按模块建立子目录，并在该目录下使用
`task` 子包承载 Asynq handler、工作流定义和周期任务。

当前目录约定：

- `export`：管理员列表异步导出 handler。
- `archive`：通用热表归档、归档控制表、管理员日志查询、SQL 模板资产和归档工作流任务定义。
- `taskreport`：周期任务和工作流运行日报聚合、发送任务和日报工作流。
- `usertag`：用户标签工作流骨架、结果表切换、Kafka outbox 和运行期辅助表治理。

`internal/task/runtime` 只保留插件运行时和核心插件，业务任务实现放在各自 `internal/jobs/<业务>` 目录。
