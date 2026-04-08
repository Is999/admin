# 用户标签实现说明

`internal/jobs/usertag` 当前只保留用户标签工作流骨架，不内置具体标签规则。

## 当前边界与职责

- `types`：核心负载、模式、节点常量和标签事件类型。
- `options`：运行参数解析，明确拒绝 `filterHash` 作为标签计算来源。
- `runtimectx`：统一 `workflow_id / mode / node / shard / batch` 日志字段和错误链路包装。
- `route`：统一 `uid%N`、运行期 UID 和标签结果分片口径。
- `queryplan`：批次级查询计划和默认拒绝无条件全表扫描的保护。
- `rule`：预留规则扩展包，默认不注册任何指标。
- `repository`：结果表、运行期表、工作流租约和 Kafka outbox 的通用访问层。
- `stage`：`prepare / business_hook / finalize / sync_kafka` 骨架阶段。
- `workflow`：工作流编排服务。
- `validation`：只读校验入口。

## 主工作流

```text
prepare -> business_hook -> finalize -> sync_kafka
```

`business_hook` 默认 no-op。接入具体标签规则时，应新增独立阶段、指标注册、迁移脚本和验收文档。

## 关键约束

- 框架层不读取外部规则来源。
- full/recalculate 应通过分片游标或自定义批处理控制内存占用。
- Kafka outbox 只应基于最终标签态生成，不能在单个阶段提前推送。
