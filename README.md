# admin-cron

`admin-cron` 是后台管理服务端工程，采用模块化单体结构组织 API、后台任务和运行时组件。项目基于 go-zero 风格分层，结合 GORM、Redis、Asynq 等基础设施封装统一的服务上下文、任务运行时、配置加载和观测链路。

本文只说明工程框架、启动方式、部署入口和开发边界；接口细节以 `docs` 目录下的专题文档为准。

> **AI 开发必读**：使用 AI 生成代码、重构、补测试或补文档前，必须先阅读 [AGENTS.md](AGENTS.md)、[AI开发规范](docs/site/角色文档/后端开发/AI开发规范.md) 和 [AI开发提示词](docs/site/角色文档/后端开发/AI开发提示词.md)。

## 工程定位

- API、Worker、Scheduler 通过同一二进制按 `mode` 组合启动，便于本地联调、独立部署和横向扩容。
- `internal/bootstrap` 负责配置加载、组件装配和生命周期控制，避免入口文件承载复杂初始化逻辑。
- `internal/svc` 聚合数据库、缓存、队列、配置、观测等基础设施依赖，上层代码通过 `ServiceContext` 获取运行时能力。
- `handler/logic/model/svc/types` 按 go-zero 思路划分边界，HTTP 解析、流程编排、数据访问和请求响应结构保持分层清晰。
- 路由、任务插件、运行组件和嵌入资产集中注册，新增能力应进入已有注册点，不在入口文件散落初始化代码。

## 运行模式

`-mode` 使用位掩码控制启动组件：

| mode | 含义 |
| --- | --- |
| `1` | 仅启动 API |
| `2` | 仅启动 Worker |
| `4` | 仅启动 Scheduler |
| `3` | 启动 API + Worker |
| `5` | 启动 API + Scheduler |
| `6` | 启动 Worker + Scheduler |
| `7` | 启动 API + Worker + Scheduler |

解析优先级为：命令行 `-mode` > 配置文件 `run_mode` > 默认值 `7`。

## 快速开始

### 1. 准备依赖

- Go 版本以 `go.mod` 为准。
- 配置文件以 `etc/config.sample.yaml` 为模板，按环境生成本地 `etc/config.yaml`。
- 外部依赖按配置启用情况准备，未启用的组件不要在本地强行初始化。

### 2. 安装依赖

```bash
go mod download
```

### 3. 启动服务

```bash
go run . -f ./etc/config.yaml -mode 7
```

### 4. 验证工程

```bash
go test ./...
go build -o bin/admin-cron .
```

仓库提供统一检查入口：

```bash
make ci
```

数据库迁移使用独立工具：

```bash
make migrate-status MIGRATE_CONFIG=./etc/config.yaml
make migrate-dry-run MIGRATE_CONFIG=./etc/config.yaml
make migrate-up MIGRATE_CONFIG=./etc/config.yaml
```

## 部署入口

生产推荐拆成控制面和执行面：

```bash
./bin/admin-cron -f ./etc/config.yaml -mode 5  # API + Scheduler
./bin/admin-cron -f ./etc/config.yaml -mode 2  # Worker
```

完整流程见 [部署发布指南](docs/site/角色文档/运维/部署发布指南.md)。

发布模板位于 `deploy/`，包括 Dockerfile、systemd 单元和集成依赖 Compose；Prometheus 告警规则位于 `docs/prometheus/admin-cron-alerts.yml`。

> **首次上线重点**：如果后台无法登录，先确认数据库里已有超级管理员账号，再通过内网接口 `POST /internal/auth/init-admin-bootstrap` 重置该账号为首次登录状态。该接口只允许内网访问，不创建新账号，不提升角色。详细步骤见 [内网初始化管理员接口](docs/site/接口文档/后台系统/内网初始化管理员接口.md)。

## 目录结构

```text
admin-cron
├── common                    # 跨包公共能力：状态码、常量、i18n、Redis Key、嵌入资产
├── docs                      # 架构、开发规范、接口文档、运维手册
│   └── site
│       ├── 角色文档
│       │   ├── 运维          # 部署发布、管理员自举
│       │   ├── 后端开发      # AI 规范、提示词、扩展指南、任务队列、配置说明
│       │   └── 前端与测试    # 联调、权限码、验收说明
│       ├── 功能模块          # 任务系统、用户标签等功能手册
│       └── 接口文档          # 后台系统、任务系统、用户标签接口
├── etc                       # 配置模板和运行期配置拆分
│   └── config.d              # task_periodic、workflows 等运行期大列表
├── internal                  # 主工程代码
│   ├── bootstrap             # 配置加载、组件装配、启动关闭
│   ├── config                # 配置结构和解析入口
│   ├── handler               # HTTP 路由注册和请求入口
│   ├── logic                 # 用例编排、规则校验、事务边界
│   ├── model                 # GORM Model 与数据访问
│   ├── svc                   # ServiceContext 与基础设施依赖聚合
│   ├── types                 # API 请求、响应、列表项结构体
│   ├── taskqueue             # Asynq 队列、工作流、周期调度基础设施
│   ├── taskruntime           # 任务插件注册和运行时装配
│   ├── jobs                  # 后台任务骨架与模块任务实现
│   ├── infra                 # MySQL、Redis、Kafka、日志、Trace 适配
│   ├── middleware            # HTTP 中间件
│   ├── requestctx            # 链路字段和调用方上下文
│   └── security              # 签名、加密、MFA、安全策略
├── helper                    # 本地辅助工具
├── pkg                       # 可复用工具包
└── scripts                   # 检查、构建、发布辅助脚本
```

## 分层约定

- `handler` 只负责参数解析、上下文透传和统一响应，不承载流程分支。
- `logic` 负责用例编排、规则校验、事务边界和错误上下文补充。
- `model` 优先使用 GORM 链式调用，事务内必须沿用同一个 `tx`。
- `types` 中请求结构体以 `Req` 结尾，响应结构体以 `Resp` 结尾，列表项以 `Item` 结尾。
- 常量、Redis Key、接口状态码和国际化文案集中定义，禁止在调用点散落魔法字符串。
- `*.sql.tmpl` 和 `.lua` 属于 `go:embed` 代码资产，SQL 模板文件头必须标注 SQL 方言、执行意图和安全边界，执行前由统一工具剥离注释。
- 错误使用 `github.com/Is999/go-utils/errors` 包装；中间层返回错误，顶层入口统一记录日志。

## 配置约定

- 主配置由 `-f` 指定，运行期大列表配置可通过 `config_files.runtime` 拆到独立文件。
- 默认使用单一 MySQL 主库；扩展库、多缓存、多队列连接必须通过已有配置结构和 `ServiceContext` 注入，不在调用路径临时创建连接。
- 可观测配置集中在配置文件中管理，日志、Trace、慢查询和指标按统一入口初始化。
- 初始化 SQL、嵌入 SQL 模板和脚本资产应保留在仓库中，改名、移动或删除时同步更新 `//go:embed` 和测试。
- 数据库变更先登记到 `internal/database.DefaultMigrations()`；现有后台基线迁移是 bootstrap-only/destructive，只能用于空库初始化。
- HTTP 路由新增或调整时同步更新 `internal/handler/route_contract.go`，并运行 `go test ./internal/handler`。

## 开发流程

1. 先阅读目标链路的 handler、logic、model、svc、types 和相关文档。
2. 按现有分层补充结构体、常量、接口状态码、国际化文案、路由元数据和审计信息。
3. 需要 SQL 或 Lua 时优先放入独立资产文件，通过统一渲染和剥离注释工具执行。
4. 新增后台任务时进入现有任务插件注册点，并补齐幂等、批量大小、日志字段和错误包装。
5. 改动后运行相关包测试；影响面不明确时运行 `go test ./...`。

## 常用命令

```bash
go test ./...
go test ./internal/...
go run . -f ./etc/config.yaml -mode 7
go build -o bin/admin-cron .
```

## 文档索引

- `AGENTS.md`
- `docs/site/角色文档/后端开发/AI开发规范.md`
- `docs/site/角色文档/后端开发/AI开发提示词.md`
- `docs/架构说明.md`
- `docs/开发规范.md`
- `docs/质量审计.md`
- `docs/site/接口文档/接口文档统一规范.md`
- `docs/site/角色文档/运维/数据库迁移治理.md`

## License

Internal use only.
