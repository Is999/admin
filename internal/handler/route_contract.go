package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
)

// RouteAccess 描述路由的入口鉴权类型，供网关、文档和测试统一理解路由边界。
type RouteAccess string

const (
	RouteAccessPublic   RouteAccess = "public"   // 匿名可访问，或由业务自行完成临时凭证校验
	RouteAccessAuth     RouteAccess = "auth"     // 后台登录态鉴权
	RouteAccessInternal RouteAccess = "internal" // 内网 IP 白名单入口
	RouteAccessDocs     RouteAccess = "docs"     // 文档会话 JWT 鉴权
	RouteAccessHealth   RouteAccess = "health"   // 健康检查与指标入口
)

// RouteContract 是 HTTP 路由对外暴露的稳定契约。
type RouteContract struct {
	Module      string      // 路由模块名称
	Method      string      // HTTP Method
	Path        string      // go-zero 路由路径
	Access      RouteAccess // 入口鉴权类型
	Alias       string      // 权限/审计/trace 统一路由别名；空表示不进入业务鉴权链
	Description string      // 业务说明
}

// Key 返回 method+path 组合键，便于和 go-zero 实际注册结果对齐。
func (c RouteContract) Key() string {
	return c.Method + " " + c.Path
}

// DefaultRouteContracts 返回内置路由模块的完整契约清单，顺序与注册顺序保持一致。
func DefaultRouteContracts() []RouteContract {
	out := make([]RouteContract, len(defaultRouteContracts))
	copy(out, defaultRouteContracts)
	return out
}

func metaRoute(module, method, path string, access RouteAccess, meta RouteMeta) RouteContract {
	return RouteContract{
		Module:      module,
		Method:      method,
		Path:        path,
		Access:      access,
		Alias:       string(meta.Alias),
		Description: meta.Describe,
	}
}

func plainRoute(module, method, path string, access RouteAccess, alias, description string) RouteContract {
	return RouteContract{
		Module:      module,
		Method:      method,
		Path:        path,
		Access:      access,
		Alias:       alias,
		Description: description,
	}
}

var defaultRouteContracts = []RouteContract{
	plainRoute("health", http.MethodGet, "/api/health", RouteAccessHealth, "", "兼容健康检查入口"),
	plainRoute("health", http.MethodGet, "/api/live", RouteAccessHealth, "", "存活检查"),
	plainRoute("health", http.MethodGet, "/api/ready", RouteAccessHealth, "", "就绪检查"),
	plainRoute("health", http.MethodGet, "/api/metrics", RouteAccessHealth, "", "Prometheus 指标抓取"),

	metaRoute("auth", http.MethodGet, "/api/auth/captcha", RouteAccessPublic, AuthCaptcha),
	metaRoute("auth", http.MethodPost, "/api/auth/login", RouteAccessPublic, AuthLogin),
	metaRoute("auth", http.MethodPost, "/api/auth/refresh", RouteAccessAuth, AuthRefresh),
	metaRoute("auth", http.MethodPost, "/api/auth/logout", RouteAccessAuth, AuthLogout),
	metaRoute("auth", http.MethodGet, "/api/auth/codes", RouteAccessAuth, AuthCodes),
	metaRoute("auth", http.MethodGet, "/api/login/after/info", RouteAccessAuth, AuthLoginAfterInfo),
	metaRoute("auth", http.MethodPost, "/api/admin/buildSecretVerifyAccount", RouteAccessPublic, UserBuildSecret),
	metaRoute("auth", http.MethodGet, "/api/admin/mine", RouteAccessAuth, UserMine),
	metaRoute("auth", http.MethodPost, "/api/admin/checkSecure", RouteAccessAuth, UserCheckSecure),
	metaRoute("auth", http.MethodPost, "/api/admin/checkMfaSecure", RouteAccessAuth, UserCheckMFA),

	metaRoute("admin", http.MethodGet, "/api/admins", RouteAccessAuth, AdminList),
	metaRoute("admin", http.MethodPost, "/api/admins/exports", RouteAccessAuth, AdminExportTrigger),
	metaRoute("admin", http.MethodGet, "/api/admins/exports/status/:jobId", RouteAccessAuth, AdminExportStatus),
	metaRoute("admin", http.MethodGet, "/api/admins/exports/download/:jobId", RouteAccessAuth, AdminExportDownload),
	metaRoute("admin", http.MethodPost, "/api/admins", RouteAccessAuth, AdminAdd),
	metaRoute("admin", http.MethodPatch, "/api/admins/status/:id", RouteAccessAuth, AdminStatusUpdate),
	metaRoute("admin", http.MethodPost, "/api/admins/password/reset/:id", RouteAccessAuth, AdminPasswordReset),
	metaRoute("admin", http.MethodPost, "/api/admins/initial-state/reset/:id", RouteAccessAuth, AdminResetInitialState),
	metaRoute("admin", http.MethodGet, "/api/admins/roles/:id", RouteAccessAuth, AdminRoleList),
	metaRoute("admin", http.MethodPatch, "/api/admins/roles/:id", RouteAccessAuth, AdminRoleUpdate),
	metaRoute("admin", http.MethodGet, "/api/admins/:id", RouteAccessAuth, AdminInfo),
	metaRoute("admin", http.MethodPatch, "/api/admins/:id", RouteAccessAuth, AdminUpdate),
	metaRoute("admin", http.MethodDelete, "/api/admins/:id", RouteAccessAuth, AdminDelete),
	metaRoute("admin", http.MethodPatch, "/api/admins/mfa-status/:id", RouteAccessAuth, UserAdminMFAStatus),
	metaRoute("admin", http.MethodGet, "/api/admins/mfa-secret-key-url/:id", RouteAccessAuth, UserBuildMFAURL),
	metaRoute("admin", http.MethodPost, "/api/admin/updatePassword", RouteAccessAuth, UserUpdatePassword),
	metaRoute("admin", http.MethodPost, "/api/admin/updateMine", RouteAccessAuth, UserUpdateMine),
	metaRoute("admin", http.MethodPost, "/api/admin/updateMfaStatus", RouteAccessAuth, UserUpdateMFA),
	metaRoute("admin", http.MethodPost, "/api/admin/updateMfaSecureKey", RouteAccessAuth, UserUpdateMFAKey),
	metaRoute("admin", http.MethodPost, "/api/admin/refreshMfaSecretKey", RouteAccessAuth, UserRefreshMFAKey),
	metaRoute("admin", http.MethodPost, "/api/admin/updateAvatar", RouteAccessAuth, UserUpdateAvatar),
	metaRoute("admin", http.MethodGet, "/api/roles", RouteAccessAuth, RoleList),
	metaRoute("admin", http.MethodGet, "/api/roles/tree", RouteAccessAuth, RoleTreeList),
	metaRoute("admin", http.MethodGet, "/api/roles/tree-options", RouteAccessAuth, RoleTreeOptions),
	metaRoute("admin", http.MethodPost, "/api/roles", RouteAccessAuth, RoleAdd),
	metaRoute("admin", http.MethodPatch, "/api/roles/:id", RouteAccessAuth, RoleUpdate),
	metaRoute("admin", http.MethodPatch, "/api/roles/status/:id", RouteAccessAuth, RoleStatusUpdate),
	metaRoute("admin", http.MethodPatch, "/api/roles/permissions/:id", RouteAccessAuth, RolePermissionUpdate),
	metaRoute("admin", http.MethodGet, "/api/roles/permissions/tree/:id/:isPid", RouteAccessAuth, RolePermissionTree),
	metaRoute("admin", http.MethodDelete, "/api/roles/:id", RouteAccessAuth, RoleDelete),
	metaRoute("admin", http.MethodGet, "/api/permissions", RouteAccessAuth, PermissionList),
	metaRoute("admin", http.MethodGet, "/api/permissions/tree", RouteAccessAuth, PermissionTreeList),
	metaRoute("admin", http.MethodGet, "/api/permissions/max-uuid", RouteAccessAuth, PermissionMaxUUID),
	metaRoute("admin", http.MethodPost, "/api/permissions", RouteAccessAuth, PermissionAdd),
	metaRoute("admin", http.MethodPatch, "/api/permissions/:id", RouteAccessAuth, PermissionUpdate),
	metaRoute("admin", http.MethodPatch, "/api/permissions/status/:id", RouteAccessAuth, PermissionStatus),
	metaRoute("admin", http.MethodDelete, "/api/permissions/:id", RouteAccessAuth, PermissionDelete),
	metaRoute("admin", http.MethodGet, "/api/dicts", RouteAccessAuth, SysConfigList),
	metaRoute("admin", http.MethodPost, "/api/dicts", RouteAccessAuth, SysConfigAdd),
	metaRoute("admin", http.MethodGet, "/api/dicts/export", RouteAccessAuth, SysConfigExport),
	metaRoute("admin", http.MethodPost, "/api/dicts/import", RouteAccessAuth, SysConfigImport),
	metaRoute("admin", http.MethodPatch, "/api/dicts/:id", RouteAccessAuth, SysConfigUpdate),
	metaRoute("admin", http.MethodGet, "/api/dicts/cache/:uuid", RouteAccessAuth, SysConfigCache),
	metaRoute("admin", http.MethodPost, "/api/dicts/cache/refresh/:uuid", RouteAccessAuth, SysConfigRenew),
	metaRoute("admin", http.MethodGet, "/api/caches", RouteAccessAuth, CacheList),
	metaRoute("admin", http.MethodGet, "/api/caches/server-info", RouteAccessAuth, CacheServerInfo),
	metaRoute("admin", http.MethodGet, "/api/caches/key-info", RouteAccessAuth, CacheKeyInfo),
	metaRoute("admin", http.MethodGet, "/api/caches/keys", RouteAccessAuth, CacheSearch),
	metaRoute("admin", http.MethodGet, "/api/caches/key-info/search", RouteAccessAuth, CacheKeyInfo),
	metaRoute("admin", http.MethodPost, "/api/caches/refresh", RouteAccessAuth, CacheRenew),
	metaRoute("admin", http.MethodPost, "/api/caches/refresh-all", RouteAccessAuth, CacheRenewAll),
	metaRoute("admin", http.MethodPost, "/api/caches/warmup", RouteAccessAuth, CacheWarmup),
	metaRoute("admin", http.MethodGet, "/api/admin-logs", RouteAccessAuth, AdminLogQuery),
	metaRoute("admin", http.MethodGet, "/api/secret-keys", RouteAccessAuth, SecretKeyList),
	metaRoute("admin", http.MethodGet, "/api/secret-keys/:id", RouteAccessAuth, SecretKeyGet),
	metaRoute("admin", http.MethodPost, "/api/secret-keys", RouteAccessAuth, SecretKeyAdd),
	metaRoute("admin", http.MethodPatch, "/api/secret-keys/:id", RouteAccessAuth, SecretKeyUpdate),
	metaRoute("admin", http.MethodPatch, "/api/secret-keys/status/:id", RouteAccessAuth, SecretKeyStatus),
	metaRoute("admin", http.MethodPost, "/api/secret-keys/cache/refresh/:uuid", RouteAccessAuth, SecretKeyRenew),
	metaRoute("admin", http.MethodPost, "/api/secret-keys/validate", RouteAccessAuth, SecretKeyValidate),
	metaRoute("admin", http.MethodPost, "/api/secret-keys/self-check/:uuid", RouteAccessAuth, SecretKeySelfCheck),
	metaRoute("admin", http.MethodPost, "/api/security/debug/sign", RouteAccessAuth, SecurityDebugSign),
	metaRoute("admin", http.MethodPost, "/api/security/debug/verify", RouteAccessAuth, SecurityDebugVerify),
	metaRoute("admin", http.MethodPost, "/api/security/debug/encrypt", RouteAccessAuth, SecurityDebugEncrypt),
	metaRoute("admin", http.MethodPost, "/api/security/debug/decrypt", RouteAccessAuth, SecurityDebugDecrypt),

	metaRoute("message", http.MethodGet, "/api/admin-messages", RouteAccessAuth, AdminMessageList),
	metaRoute("message", http.MethodGet, "/api/admin-messages/sent", RouteAccessAuth, AdminMessageSentList),
	metaRoute("message", http.MethodGet, "/api/admin-messages/:id/receivers", RouteAccessAuth, AdminMessageReceivers),
	metaRoute("message", http.MethodGet, "/api/admin-messages/unread-count", RouteAccessAuth, AdminMessageUnreadCount),
	metaRoute("message", http.MethodGet, "/api/admin-messages/notifications", RouteAccessAuth, AdminMessageNotifications),
	metaRoute("message", http.MethodPatch, "/api/admin-messages/read", RouteAccessAuth, AdminMessageMarkRead),
	metaRoute("message", http.MethodPost, "/api/admin-messages/delete", RouteAccessAuth, AdminMessageDelete),
	metaRoute("message", http.MethodPost, "/api/admin-messages/send", RouteAccessAuth, AdminMessageSend),
	metaRoute("message", http.MethodPost, "/api/admin-messages/handle", RouteAccessAuth, AdminMessageHandle),

	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/init", RouteAccessAuth, string(middleware.Ignore), "初始化断点续传上传会话"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/upload/status", RouteAccessAuth, string(middleware.Ignore), "查询断点续传上传状态"),
	plainRoute("transfer", http.MethodPut, "/api/file-transfer/upload/chunk", RouteAccessAuth, string(middleware.Ignore), "上传单个文件分片"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/download", RouteAccessAuth, string(middleware.Ignore), "下载当前管理员上传完成的文件"),
	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/complete", RouteAccessAuth, string(middleware.Ignore), "完成断点续传上传会话"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/access", RouteAccessPublic, "", "公开访问允许匿名预览的上传文件"),

	metaRoute("task", http.MethodPost, "/api/tasks", RouteAccessAuth, TaskEnqueue),
	metaRoute("task", http.MethodGet, "/api/tasks", RouteAccessAuth, TaskItemsList),
	metaRoute("task", http.MethodGet, "/api/tasks/overview", RouteAccessAuth, TaskItemsList),
	metaRoute("task", http.MethodPost, "/api/tasks/workflows", RouteAccessAuth, TaskWorkflowTrigger),
	metaRoute("task", http.MethodGet, "/api/tasks/workflows/:workflowId", RouteAccessAuth, TaskWorkflowStatus),
	metaRoute("task", http.MethodGet, "/api/tasks/queues", RouteAccessAuth, TaskQueueList),
	metaRoute("task", http.MethodGet, "/api/tasks/registry/task-types", RouteAccessAuth, TaskQueueList),
	metaRoute("task", http.MethodGet, "/api/tasks/registry/workflows", RouteAccessAuth, TaskWorkflowStatus),
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload", RouteAccessAuth, TaskConfigReload),
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload/items", RouteAccessAuth, TaskConfigItems),
	metaRoute("task", http.MethodPost, "/api/tasks/config-reload", RouteAccessAuth, TaskConfigReloadRun),
	metaRoute("task", http.MethodGet, "/api/tasks/:taskId", RouteAccessAuth, TaskInfoGet),
	metaRoute("task", http.MethodPost, "/api/tasks/queues/pause/:queue", RouteAccessAuth, TaskQueuePause),
	metaRoute("task", http.MethodPost, "/api/tasks/run/:taskId", RouteAccessAuth, TaskRun),
	metaRoute("task", http.MethodDelete, "/api/tasks/:taskId", RouteAccessAuth, TaskDelete),
	metaRoute("task", http.MethodPost, "/api/tasks/queues/resume/:queue", RouteAccessAuth, TaskQueueResume),

	metaRoute("collector", http.MethodGet, "/api/collector/overview", RouteAccessAuth, CollectorOverview),
	metaRoute("collector", http.MethodGet, "/api/collector/tasks", RouteAccessAuth, CollectorTaskList),
	metaRoute("collector", http.MethodPost, "/api/collector/run", RouteAccessAuth, CollectorRun),
	metaRoute("collector", http.MethodPost, "/api/collector/tasks/retry", RouteAccessAuth, CollectorRetry),

	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflows", RouteAccessAuth, UserTagWorkflowTrigger),
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/recalculations", RouteAccessAuth, UserTagRecalculate),
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflow-lease/release", RouteAccessAuth, UserTagWorkflowLeaseRelease),
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/workflows", RouteAccessInternal, UserTagWorkflowTrigger),
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/recalculations", RouteAccessInternal, UserTagRecalculate),
	metaRoute("internal_auth", http.MethodPost, "/internal/auth/init-admin-bootstrap", RouteAccessInternal, InternalInitAdminBootstrap),

	metaRoute("docs", http.MethodPost, "/api/docs/session", RouteAccessAuth, DocsSession),
	plainRoute("docs", http.MethodGet, "/api/docs", RouteAccessDocs, "", "文档首页"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path", RouteAccessDocs, "", "文档静态资源"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub", RouteAccessDocs, "", "二级文档资源"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub/:file", RouteAccessDocs, "", "三级文档资源"),
}
