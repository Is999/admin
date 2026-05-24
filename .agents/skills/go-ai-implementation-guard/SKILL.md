---
name: go-ai-implementation-guard
description: "约束 Go 后端实现防漂移。用于实现、修复、重构、迁移和生产复核，检查 AI开发规范、命名、注释、GORM/SQL、Redis、配置、收口。"
---

# Go AI 实现防漂移

## 工作流程

1. 先确认准确仓库，读取生效的 `AGENTS.md` 链路，并用 `rg --files` 发现当前仓库实际存在的 AI 开发文档。
2. 需求含糊、只要求分析、存在多种解释或风险较高时，先使用 `$requirement-intent-gate`，不要猜业务意图。
3. 修改前先读现有代码路径：目录结构、代码风格、调用链、数据契约和测试。
4. 非平凡任务要先说明实现边界：目标行为、源码依据、可能修改文件、明确非目标和已确认假设。
5. 闭合完整生产路径，而不是只写 helper 或脚手架；多分支、多入口、多同步面需求交付前使用 `$implementation-closure-review`。
6. 改动保持小而聚焦；优先复用现有 helper、包、错误包装、logger、配置模式、SQL/Lua 资产加载器和测试风格。
7. 实现后复核命名：目录、变量、方法、常量、结构体、接口和字段必须贴合业务职责，避免泛名和过长命名。
8. 新增或修改稳定契约和关键逻辑要补简洁中文注释；多轮实现后不能留下大片无说明代码。
9. 按仓库要求验证；全量验证太贵或被环境阻塞时，至少跑最接近的相关包检查并说明原因。
10. 交付前在每个触碰的 Git 仓库运行 `git diff --check` 和 `git status --short`；只把本轮新增交付文件加入索引。

## 必读资料

目标仓库、仓库内 AI 文档或验证命令不清楚时，读取 `references/project-map.md`。

## 后端硬规则

- 仓库规范要求时，使用 `github.com/Is999/go-utils/errors` 包装错误。
- `error` 级日志只放在 handler、worker、scheduler、startup、health 等外层；内层逻辑返回包装后的错误。
- MySQL/SQL 默认使用 GORM 链式调用；只有 GORM 无法安全表达或会明显影响性能时才使用原生 SQL。
- 接受原生 SQL 时，必须记录原因、索引依据、预估扫描范围、分页或批量上限、超时、并发和 DB 压力控制。
- 允许的原生 SQL 放在 `*.sql.tmpl`，Redis Lua 放在 `.lua`，并通过 `go:embed` 加载。
- 执行 SQL/Lua 前，使用 `embedasset.StripLeadingLineComments` 或本包 helper 剥离文件头说明。
- 新增或修改 SQL 路径前，评估执行计划、索引、基数、返回行数、batch、cursor、timeout、concurrency 和库压力。
- 除非用户明确要求，保持外部 YAML、API、权限、业务码、前端类型和文档契约稳定。
- 不要把脚手架、样例逻辑、happy path 或多分支需求中的单一分支称为完成。

## 交付检查

最终说明必须包含：

- 修改了哪些文件以及原因。
- 需求闭环状态和明确非目标。
- 验证命令和结果。
- 跳过的检查及原因。
- 是否需要数据迁移、回填、缓存失效或补偿任务。
