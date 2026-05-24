---
name: api-contract-sync
description: "同步后端 API 契约。用于 handler、route、request/response、业务码、权限、RouteMeta、安全字段变化，同步文档、前端类型、i18n。"
---

# API 契约同步

## 工作流程

1. 先读取当前生效的 `AGENTS.md`，用 `rg --files` 发现仓库内实际存在的 AI/接口文档，并读取 `references/project-map.md`。
2. 从 route/handler 追到 logic/model，再向外核对接口文档和前端调用方。
3. 新增或修改路由时，核对当前仓库实际存在的 RouteMeta、权限码、审计动作、公开/白名单原因、鉴权中间件和安全策略；不存在的面标记不适用。
4. 修改 request/response 时，同步 Go 类型、接口文档、前端 API wrapper、TypeScript 类型和 i18n 可见文案。
5. 修改业务错误时，使用独立业务码并补中文/英文消息；message 禁止暴露 SQL、密钥、表名或真实下游错误。
6. 涉及敏感操作时，保持 MFA、签名、加密、密钥版本和字段大小限制一致。
7. 契约影响前端时，后端测试之外还要跑前端 typecheck。

## 参考资料

当不确定当前仓库文档位置或验证命令时，读取 `references/project-map.md`。

## 交付检查

- 代码、文档、前端 wrapper/type、业务码、i18n、权限、RouteMeta 等实际存在的同步面已处理；不存在的面明确不适用。
- 安全字段、公开路由和白名单边界已写清。
- 验证至少包含相关后端测试；契约到达前端时包含前端 typecheck。
- 明确说明是否需要数据迁移、缓存失效或补偿任务。
