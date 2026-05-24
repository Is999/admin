---
name: go-security-route-guard
description: "审查 Go 路由安全和敏感契约。用于 auth、JWT/session、MFA、权限、RouteMeta、签名、加密、公白名单路由、业务鉴权码、前端联动。"
---

# Go 路由安全护栏

## 工作流程

1. 从注册一路追到 middleware、handler、logic、response wrapper、文档和前端调用方。
2. 分类路由：登录态、公开、内网、白名单 token、MFA、签名、加密或混合。
3. 核对 RouteMeta、权限码、审计动作、安全策略、业务码、i18n 和文档是否同步。
4. JWT/session 改动要检查 `jti`、Redis session/index key、用户状态、app ID/tenant scope、登出/刷新生命周期和重放边界。
5. 签名/加密必须列出明确字段；不要默认签名/加密完整 body、大对象、数组或分页列表。
6. 错误消息保持安全：不暴露 SQL、密钥、内部表名、真实下游错误或 key material。
7. 相关时补 handler/middleware smoke test：鉴权 envelope、公开路由兼容、安全失败业务码、字段大小限制。

## 验证

- 运行 handler/middleware/security 相关聚焦测试。
- request/response、业务码、文档、前端类型或 i18n 变化时使用 `$api-contract-sync`。
- 运行 `git diff --check` 并检查 `git status --short`。

## 交付证据

说明路由分类、安全字段、权限/业务码/文档同步、测试结果、跳过检查，以及前端或配置后续事项。
