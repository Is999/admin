---
name: go-comment-style-audit
description: "审计 Go AI开发规范、中文注释和可维护性。用于全仓库校验、重新校验、注释缺失、代码难读或多轮后端改动后的复核。"
---

# Go 注释和可维护性审计

## 工作流程

1. 先读取当前生效的 `AGENTS.md` 和仓库内 AI 开发文档。
2. 判断注释前先看改动文件和周边调用链。
3. 先对聚焦路径运行 AST 审计；用户要求全仓库或改动触及共享契约时，再扩大到 repo-wide。
4. 只补有维护价值的注释：函数、方法、类型、结构体字段、const/var 组、route/security metadata、GORM Model、配置结构、payload、持久化 JSON 和关键逻辑。
5. 注释使用中文，短而有诊断价值；说明业务意图、数据形状、单位、边界、默认行为、风险或降级策略，不重复标识符名称。
6. 修复后重新跑审计和相关测试。

## 脚本

使用：

```bash
go run <skill-dir>/scripts/go_comment_audit.go <repo-or-dir>
```

脚本会扫描私有 helper、结构体字段、const/var 声明和匿名局部结构体字段。它是护栏，不替代人工判断；需要结合上下文审查结果，避免给平凡局部逻辑加噪声注释。

## 参考资料

决定当前仓库适用的 AI 文档和验证命令时，读取 `references/project-map.md`。
