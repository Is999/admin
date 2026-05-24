# 项目地图

## 文档发现

从当前仓库发现契约文档，不依赖其它 checkout 的绝对路径。

常用搜索：

- `rg --files | rg '接口文档统一规范\.md$|docs/site/接口文档'`
- `rg --files | rg 'API|api|openapi|apifox|route|permission|i18n'`

如果存在统一文档格式，按统一格式处理；如果没有，按当前仓库最近的 API 文档和生效的 `AGENTS.md` 处理。

## 契约面

- Go handler、logic、model/repository、svc、config 和 types。
- Route metadata、permission code、audit action，以及存在时的 RouteSecurityContract/RouteSecurityPolicy。
- 业务错误码和中英文消息。
- 当前任务同时触达前端仓库时，才同步前端 API wrapper、TypeScript request/response type、i18n 文案、routes 和 permission UI。
- 行为变化时同步 YAML config 示例、SQL/Lua 资产和生成文档。

## 仓库提示

- 用 `rg -n 'RouteSecurity|RouteMeta|permission|Permission|MFA|signature|encrypt|cipher'` 发现路由安全面。
- 验证前端可见的 `code/message` 行为，不只看 HTTP status。
- public 或 whitelist route 要说明为什么公开，以及 token/signature/encryption 边界在哪里。

## 检查

- 后端：相关 package 的 `go test`，条件允许时运行 `go test ./...`。
- 前端契约：仅当任务触达前端仓库时，到对应仓库读取其 AGENTS.md 和 package scripts 后运行真实命令。
- 必跑：`git diff --check`、`git status --short`。
