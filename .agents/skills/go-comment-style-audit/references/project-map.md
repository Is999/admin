# 项目地图

选择审计范围前先读这里。

## AI 文档

从当前仓库发现 AI 开发文档，不使用其它 checkout 的路径：

```bash
rg --files | rg 'AGENTS\.md$|AI开发规范\.md$|AI开发提示词\.md$'
```

优先读取当前仓库的 `AGENTS.md`、`AI开发规范.md` 和 `AI开发提示词.md`。目标仓库缺少本地 AI 文档时，按最近的 `AGENTS.md`，并从当前源码验证风格。

## 审计范围规则

- 实现后的“重新校验”：审计已改文件和附近共享 struct/helper。
- “全仓库”或“确保没有类似问题”：扫描所有非测试 Go 文件，并包含 private helper、struct field、const/var block、匿名局部 struct field。
- route-security 代码：private validator 和 helper struct 也视为契约的一部分；注释要解释契约含义，不只是 exported API。
- config、API request/response、task payload、GORM model、持久化 JSON struct：相关字段要说明元素形态、单位、默认行为、排序、去重、空值语义或风险。

## 验证模式

1. 运行 AST scanner。
2. 按源码风格修复有价值的发现。
3. 重新运行 scanner。
4. 运行相关 `go test` package。
5. 运行 `git diff --check` 和 `git status --short`。
