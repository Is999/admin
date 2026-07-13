# api 项目边界

## 真实结构

- HTTP 主链路从路由和 middleware 进入 `handler -> logic -> model/svc`，最终到 DB、Redis 或外部服务；审查时必须覆盖鉴权和响应语义。
- 启动、组件状态和迁移位于 `cmd`、`internal/bootstrap`、`internal/svc`、`internal/database`；只复用项目已有接线方式。
- `api` 是请求热路径，不复制 `admin` 的任务队列、Collector 或运行时框架。兄弟仓库只同步确实共享的安全语义，不强行抽公共层。
- 新增或重构的变量、常量、方法、结构体、字段、map 字段、路由、GORM Model、接口和关键逻辑必须按项目规范补简洁中文注释。

## 简化重点

- handler 只做参数接收和响应，logic 直接表达业务规则；不为单一调用增加 service、repository 或 mapper 包装。
- middleware 保持顺序和短路语义；鉴权、签名、加密等安全边界不能因减少查询或判断而弱化。
- 每请求数据库或 Redis 操作先核对频率、索引、缓存一致性和即时撤销要求，再决定是否优化。
- 迁移互斥、节点租约和 readiness 若与 `admin` 语义相同，可保持实现对齐；两个独立仓库不为消除少量重复新增共享发布依赖。
- 错误码、响应字段、权限和接口文档属于返回语义，内部重构后必须保持一致。
- 冷路径不以难读的微优化换取理论收益；简单直接的 Go 代码优先交给编译器内联和逃逸分析。

## 验证

按改动风险选择并在最终候选版本运行：

- 有代码改动时运行 `gofmt -w <修改的 Go 文件>`；只读审查只检查 `gofmt -d` 或 `gofmt -l`
- `go test ./...`
- `go vet ./...`
- 并发或共享状态改动补相关包 `go test -race`
- 迁移改动补 `make integration-test`
- `git diff --check`、`git diff --cached --check`、`git status --short`
