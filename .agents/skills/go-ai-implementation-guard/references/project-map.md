# 项目地图

仅在 skill 触发后且当前仓库结构不明显时使用本地图。

## 发现当前仓库

- 从当前工作目录开始，有 Git 仓库时先运行 `git rev-parse --show-toplevel`。
- 从仓库根目录到当前目录阅读生效的 `AGENTS.md` 链。
- 用 `rg --files | rg 'AGENTS\.md$|AI开发规范\.md$|AI开发提示词\.md$|CLAUDE\.md$'` 发现仓库本地规范文件。
- 存在 `AI开发提示词.md` 或 `AI开发规范.md` 时优先读取；不存在时按最近的 `AGENTS.md`，并从当前源码、测试和文档推断风格。

## 后端入口

从当前项目发现后端面，不假设其它仓库的名称可用：

- Routes 和 handlers：`handler`、`internal/handler`、`routes`、`*.api` 或生成的路由文件。
- 业务逻辑：`logic`、`service`、`internal/logic`、`internal/service`。
- 数据层：`model`、`repository`、`dao`、`internal/model`、`internal/repository`。
- 运行时接线：`svc`、`internal/svc`、`bootstrap`、`config`、`cmd`、worker、scheduler、consumer、plugin。
- 契约：`types`、request/response struct、业务码、permission、RouteMeta、security policy、docs 和 i18n。


## 验证

- Go 后端：优先 `go test ./...`；范围很窄或环境受限时，运行相关 package 测试并说明为什么跳过全量测试。
- 前端：从 package scripts 运行当前仓库的 typecheck/lint/build；hook 失败时复现失败 hook。
- 触达的 Git 仓库都运行 `git diff --check` 和 `git status --short`。
