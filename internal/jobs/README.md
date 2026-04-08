# 任务业务目录

`internal/jobs` 用于收口定时任务、工作流节点和对应仓储实现。后续新增独立任务时，优先按模块建立子目录，并在该目录下使用
`task` 子包承载 Asynq handler、工作流定义和周期任务。

当前目录约定：

- `archive`：通用热表归档、归档控制表和归档工作流任务定义。
- `usertag`：用户标签工作流骨架、结果表切换、Kafka outbox 和运行期辅助表治理。

`internal/taskruntime` 只保留插件装配入口，避免定时任务实现继续散落在 `internal` 顶层。
