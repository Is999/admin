# 用户标签实现说明

`internal/jobs/usertag` 只保留用户标签工作流骨架，不内置具体标签规则、业务事实表或业务清洗 SQL。

## 当前边界与职责

- `types`：核心负载、模式、节点常量和标签事件类型。
- `options`：运行参数解析，明确拒绝 `filterHash` 作为标签计算来源。
- `runtimectx`：统一 `workflow_id / mode / node / shard / batch` 日志字段和错误链路包装。
- `route`：统一 `uid%N`、运行期 UID 和标签结果分片口径。
- `queryplan`：批次级查询计划和默认拒绝无条件全表扫描的保护。
- `repository`：结果表、运行期表、工作流租约和事件 outbox 的通用访问层。
- `stage`：`prepare / collect_scope / evaluate_tags / resolve_changes / persist_results / finalize / dispatch_hooks` 骨架阶段。
- `hook`：标签得到和失去事件 hook 注册表。
- `workflow`：工作流编排服务。
- `validation`：只读校验入口。

## 主工作流

```text
prepare
  -> collect_scope
  -> evaluate_tags
  -> resolve_changes
  -> persist_results
  -> finalize
  -> dispatch_hooks
```

业务扩展只应接入候选范围、规则评估、差异解析或 hook，不应在骨架中加入登录、充值、投注等具体业务事实节点。

## 关键约束

- 框架层不读取外部规则来源。
- full/recalculate 应通过分片游标或自定义批处理控制内存占用。
- 标签得到和失去事件只应基于最终标签态生成，再写入 `user_tag_event_outbox`。
- 未注册事件 hook 时不能把已有 outbox 行标记为成功。
