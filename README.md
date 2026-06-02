# Admin 服务

`admin` 是后台管理服务端工程，负责管理员认证、权限、审计、系统配置、运行配置、任务系统、文件传输、前台用户管理和用户标签等后台能力。项目采用 go-zero 风格的模块化单体结构，通过同一二进制按 `mode` 组合启动 API、Worker 和 Scheduler。

本文只说明工程框架、启动方式、部署入口和开发边界；接口字段、任务规则和运维细节以 `docs/site` 下的专题文档为准。

> **AI 开发必读**：使用 AI 生成代码、重构、补测试或补文档前，必须先阅读 [AGENTS.md](AGENTS.md)、[AI开发规范](docs/site/角色文档/后端开发/AI开发规范.md) 和 [AI开发提示词](docs/site/角色文档/后端开发/AI开发提示词.md)。

## 技术栈

- Go `1.26.4`
- go-zero HTTP 服务框架
- GORM + MySQL，支持主从读写路由和命名扩展库
- Redis + redsync，支撑缓存、分布式锁、任务调度锁和运行期状态
- Asynq + robfig/cron，承载 Worker、Scheduler、工作流和周期任务
- JWT + MFA + RBAC + 审计日志，支撑后台登录态和权限链路
- 可选签名验签、AES/RSA 加解密和路由安全清单
- Kafka / Redis Stream / DB outbox 通用 Collector
- OpenTelemetry Trace、Prometheus 指标和结构化访问日志

## 运行链路

```text
cmd/admin
  -> bootstrap.WireWithConfigMode
  -> 加载配置、校验安全边界、按配置初始化 logger/trace/MySQL/Redis/Kafka
  -> 创建 ServiceContext
  -> 注册 collector、task_runtime、http_server 启动组件
  -> 按 mode 启动 API、Worker、Scheduler
  -> handler.RegisterHandlersWithModules
  -> recover -> trace -> access log -> recover
  -> handler/shared 解析请求、鉴权、审计和统一响应
  -> logic 编排业务、事务、缓存和任务投递
  -> model / infra / task / pkg 访问数据库、Redis、队列、文件存储和外部服务
```

所有 HTTP 响应保持统一结构：

```json
{
  "status": true,
  "code": 1000,
  "message": "成功",
  "data": {}
}
```

## 运行模式

`-mode` 使用位掩码控制启动单元：

| mode | 含义 |
| --- | --- |
| `1` | 仅启动 API |
| `2` | 仅启动 Worker |
| `4` | 仅启动 Scheduler |
| `3` | 启动 API + Worker |
| `5` | 启动 API + Scheduler |
| `6` | 启动 Worker + Scheduler |
| `7` | 启动 API + Worker + Scheduler |

解析优先级为：命令行 `-mode` > 配置文件 `run_mode` > 默认值 `7`。生产常规部署建议拆成控制面和执行面：

```bash
./bin/admin -f ./etc/config.yaml -mode 5  # API + Scheduler
./bin/admin -f ./etc/config.yaml -mode 2  # Worker
```

## 目录结构

```text
admin
├── cmd                       # 二进制入口
│   ├── admin                 # HTTP / Worker / Scheduler 组合启动入口
│   └── migrate               # 数据库迁移命令入口
├── common                    # 跨包公共契约：业务码、常量、i18n、Redis Key、嵌入资产、运行态配置
├── docs                      # 文档站、接口文档、运维手册、监控资产
│   ├── site
│   │   ├── 角色文档
│   │   │   ├── 后端开发    # AI 规范、扩展指南、组件清单、任务队列、配置说明
│   │   │   ├── 前端与测试  # 联调、权限码、验收说明
│   │   │   └── 运维        # 部署发布、数据库迁移治理
│   │   ├── 功能模块        # 任务系统、用户标签等功能手册
│   │   └── 接口文档        # 后台系统、任务系统、用户标签接口
│   ├── prometheus            # Prometheus 告警规则
│   ├── grafana               # Grafana 面板
│   └── handler.go            # 文档站资源读取入口
├── deploy                    # Docker、systemd 和本地集成依赖
├── etc                       # 配置模板和运行期配置拆分
│   └── config.d              # runtime.yaml 等运行期大列表配置
├── helper                    # HTTP JSON 响应和轻量通用函数
├── internal                  # 主工程代码
│   ├── audit                 # 管理员审计事件记录与脱敏
│   ├── bootstrap             # 配置加载、组件装配、热加载、启动关闭
│   ├── config                # YAML 配置结构和解析契约
│   ├── database              # 数据库迁移、迁移状态表和 SQL 资产
│   ├── handler               # HTTP 路由注册、RouteMeta、RouteContract 和请求入口
│   ├── infra                 # MySQL、Redis、Kafka、Lark、日志、Trace、Collector 适配
│   ├── jobs                  # 归档、导出、用户标签等后台任务实现
│   ├── logic                 # 用例编排、规则校验、事务边界、缓存和运行配置
│   ├── middleware            # 鉴权、签名、加解密、内网限制、访问日志、Recover
│   ├── model                 # GORM Model、表名、数据访问和运行态模型
│   ├── requestctx            # 链路字段、调用方、任务和 trace 元数据
│   ├── routealias            # 路由别名常量
│   ├── security              # 路由字段级签名、加密、大小限制和测试向量
│   ├── svc                   # ServiceContext 与基础设施依赖聚合
│   ├── task                  # Asynq 队列、工作流、任务插件运行时和进度统计
│   └── types                 # API 请求、响应、列表项和参数校验
└── pkg                       # 可复用工具包：对象存储、文件传输、Excel、批处理
```

`data/` 和 `logs/` 是本地运行输出目录；不要把本地密钥、上传文件、临时数据或日志当作发布资产提交。

## 核心能力

- 后台认证：验证码、登录、刷新、退出、MFA、JWT 登录态和权限码查询。
- 权限治理：管理员、角色、权限树、前端权限码、行级操作审计。
- 系统管理：系统配置、运行配置、缓存管理、秘钥管理、安全调试和消息中心。
- 前台用户管理：后台直连用户表处理资料、状态、密码和 API 运行态同步。
- 任务系统：Asynq 队列、工作流、周期调度、任务列表、队列控制、失败归档和 Lark 告警。
- 用户标签：full / delta / targeted / recalculate 工作流、运行期 UID 索引、事件 outbox 和排障接口。
- 文件传输：本地或 S3 存储、服务端中转、断点续传、导出文件下载和上传策略注册。
- Collector：支持 Kafka、Redis Stream 和 DB outbox，用于后台轻量事件采集和批量消费。
- 可观测性：提供 `/api/live`、`/api/ready`、`/api/metrics`、文档站 `/api/docs`，并配套日志、Trace、指标和告警规则。

## 分层约定

- `cmd` 只处理命令行参数和进程退出码，真实装配进入 `bootstrap`。
- `bootstrap` 负责配置加载、组件生命周期、热加载边界和注册清单派生，不写业务规则。
- `handler` 只声明 `RouteSpecs`、解析请求、执行鉴权审计和统一响应；复杂流程进入 `logic`。
- `logic` 负责用例编排、规则校验、事务边界、缓存保护和错误上下文。
- `model` 优先使用 GORM 链式调用，事务内必须沿用同一个 `tx`。
- `task` 提供队列、工作流、插件运行时和统计基础设施；具体业务任务放在 `internal/jobs/<domain>`。
- `types` 中请求以 `Req` 结尾，响应以 `Resp` 结尾，列表项以 `Item` 结尾；请求结构体需要实现 go-zero `Validate()`。
- `common` 只放跨包稳定契约和小型公共能力，领域规则优先留在所属 `logic/model/jobs` 包。
- `pkg` 只放可复用工具包；没有跨领域复用价值的能力不要放进 `pkg`。

## 统一注册点

| 注册对象 | 统一入口 | 说明 |
| --- | --- | --- |
| 启动组件 | `internal/bootstrap/registrations.go:defaultComponentSpecs` | Collector、任务运行时、HTTP Server 从规格派生真实组件和注册清单 |
| HTTP 路由 | `internal/handler/routes.go:builtinRouteModuleSpecs` + 各模块 `RouteSpecs` | 真实路由、RouteContract、访问日志降噪和文档校验都从路由规格派生 |
| RouteMeta | `internal/handler/shared/route_meta.go:DefaultRouteMetas` | 路由别名、中文说明和审计动作集中登记 |
| 路由安全清单 | `internal/handler/route_security_manifest.go:DefaultRouteSecurityManifest` | 汇总 method、path、chain 和字段级签名加密策略，用于文档和前端同步 |
| 任务插件 | `internal/task/runtime/builtins.go:BuiltinPluginSpecs` | core、archive、admin_export、user_tag 等任务插件从规格派生 |
| 运行时扩展 | 各能力归属包 `RuntimeRegistrySpecs` | `pkg/storage`、`internal/logic/file`、`internal/infra/collectorx` 分别声明自身扩展入口 |
| 数据库迁移 | `internal/database/migrations.go:defaultMigrationSpecs` | 迁移版本、名称、SQL 资产和 bootstrap-only 边界集中登记 |
| Redis Key | `common/rediskeys` | Key 模板集中定义并带静态检查，业务代码不散落高基数字符串 |

详细注册表见 [组件注册清单](docs/site/角色文档/后端开发/组件注册清单.md)。

## 本地启动

准备 MySQL、Redis 后，复制样例配置并调整连接信息、`app_id`、`jwt_secret`、安全秘钥、运维令牌和文件存储路径：

```bash
cp etc/config.dnmp.sample.yaml etc/config.yaml
cp etc/config.d/runtime.sample.yaml etc/config.d/runtime.yaml
go mod download
```

执行迁移前先查看和预览：

```bash
make migrate-status MIGRATE_CONFIG=./etc/config.yaml
make migrate-dry-run MIGRATE_CONFIG=./etc/config.yaml
```

空库初始化才允许执行 bootstrap-only 基线迁移：

```bash
make migrate-bootstrap MIGRATE_CONFIG=./etc/config.yaml
```

已有库只执行普通迁移：

```bash
make migrate-up MIGRATE_CONFIG=./etc/config.yaml
```

启动开发环境：

```bash
go run ./cmd/admin -f ./etc/config.yaml -mode 7
```

查看构建版本：

```bash
go run ./cmd/admin -version
go run ./cmd/migrate -version
```

## 配置边界

配置文件入口：

- `etc/config.sample.yaml`：标准样例。
- `etc/config.dnmp.sample.yaml`：本地 dnmp 环境样例。
- `etc/config.yaml`：本地实际运行配置，不应提交生产秘钥。
- `etc/config.d/runtime.sample.yaml`：运行期大列表配置样例。
- `etc/config.d/runtime.yaml`：本地运行期配置文件。

当前 `runtime_config.source=database` 时，`task_periodic` 和 `archive_jobs` 由运行配置 active release 接管；运行期文件中的同名配置只作为首次导入种子。`workflows` 仍保留在运行期文件中并参与热加载。

新增配置时必须区分：

- 运行期参数：可热加载，例如部分任务批次、分片、限速、归档窗口和用户标签参数。
- 启动期能力：必须重启，例如 HTTP 监听、MySQL、Redis、Kafka、OTLP、路由、任务插件和 workflow 定义注册。

配置字段说明见 [配置字段说明](docs/site/角色文档/后端开发/配置字段说明.md)。

## 数据库迁移

迁移入口是 `cmd/migrate`，迁移定义来自 `internal/database.defaultMigrationSpecs`，SQL 资产放在 `internal/database/assets/`。执行结果会登记到 `schema_migrations`。

迁移分两类：

- bootstrap-only 基线迁移：只允许空库初始化时通过 `make migrate-bootstrap` 显式执行。
- 普通迁移：通过 `make migrate-up` 执行，适合已有库增量升级。

迁移治理要求见 [数据库迁移治理](docs/site/角色文档/运维/数据库迁移治理.md)。

## 接口与文档

接口文档位于 `docs/site/接口文档/`：

- `后台系统/`：认证、管理员、角色权限、系统配置、文件传输、消息、缓存、秘钥、安全调试、前台用户管理等接口。
- `任务系统/`：任务总控、队列、任务列表、监控、运行配置、Collector 等接口。
- `用户标签/`：用户标签业务、工作流、内网接口和指定标签重算接口。

文档站入口是 `/api/docs`。新增或调整接口时，需要同步 Go types、RouteMeta、权限码、审计动作、安全字段、接口文档、业务码和 i18n 文案。

## 验证命令

日常开发优先运行：

```bash
make fmt-check
make test
make build
make build-tools
git diff --check
```

发布前运行完整检查：

```bash
make ci
```

`make ci` 会执行格式检查、全量测试、主服务构建、迁移工具构建、秘钥扫描、Prometheus 规则检查和 `git diff --check`。如果本机没有 `promtool`，规则检查会优先尝试 Docker 镜像。

## 发布与观测

发布资产：

- `deploy/docker/Dockerfile`
- `deploy/systemd/admin-control.service`
- `deploy/systemd/admin-worker.service`
- `deploy/integration/docker-compose.yml`
- `docs/prometheus/admin-alerts.yml`
- `docs/grafana/`

发布前至少确认：

- `make migrate-dry-run` 输出符合预期。
- `make migrate-up` 或首次空库 `make migrate-bootstrap` 已按环境正确执行。
- `/api/live`、`/api/ready`、`/api/metrics` 和 `/api/docs` 可访问。
- Worker、Scheduler、Collector、任务插件和运行配置 active release 状态正常。
- MySQL、Redis、Kafka、Lark、Trace、文件存储均指向目标环境。
- 生产配置中没有样例 `jwt_secret`、私钥、AES Key、对象存储密钥或运维令牌。

完整流程见 [部署发布指南](docs/site/角色文档/运维/部署发布指南.md)。

> **首次上线重点**：如果后台无法登录，先确认数据库里已有超级管理员账号，再通过内网接口 `POST /internal/auth/init-admin-bootstrap` 重置该账号为首次登录状态。该接口只允许内网访问，不创建新账号，不提升角色。详细步骤见 [内网初始化管理员接口](docs/site/接口文档/后台系统/内网初始化管理员接口.md)。

## 开发约束

修改代码、配置、SQL、脚本或文档前先读：

- `AGENTS.md`
- `docs/site/角色文档/后端开发/AI开发规范.md`
- `docs/site/角色文档/后端开发/AI开发提示词.md`

关键要求：

- 请求参数放在 `internal/types`，需要实现 go-zero `Validate()`。
- handler 只负责解析、鉴权审计和响应写出，业务编排放在 `internal/logic`。
- 新增接口必须同步接口文档、RouteMeta、权限码、审计动作、业务码和 i18n。
- 新增任务必须进入 `internal/task/runtime/builtins.go:BuiltinPluginSpecs` 或明确说明为何只作为外部注入插件。
- Redis Key 必须走 `common/rediskeys`，禁止在业务代码散落高基数通配 key。
- 原生 SQL / Lua 必须作为代码资产，通过 `go:embed` 加载并在执行前剥离文件头说明。
- 新增运行期能力必须同步注册清单和测试，不能只加业务实现。

## 文档索引

- [架构说明](docs/架构说明.md)
- [开发规范](docs/开发规范.md)
- [质量审计](docs/质量审计.md)
- [接口文档统一规范](docs/site/接口文档/接口文档统一规范.md)
- [开发扩展指南](docs/site/角色文档/后端开发/开发扩展指南.md)
- [组件注册清单](docs/site/角色文档/后端开发/组件注册清单.md)
- [任务队列与工作流指南](docs/site/角色文档/后端开发/任务队列与工作流指南.md)
- [数据库迁移治理](docs/site/角色文档/运维/数据库迁移治理.md)

## License

Internal use only.
