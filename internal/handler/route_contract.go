package handler

import (
	"admin/internal/handler/shared"
	"net/http"

	"admin/internal/middleware"
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

func metaRoute(module, method, path string, access RouteAccess, meta shared.RouteMeta) RouteContract {
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
	plainRoute("health", http.MethodGet, "/api/live", RouteAccessHealth, "", "存活检查"),
	plainRoute("health", http.MethodGet, "/api/ready", RouteAccessHealth, "", "就绪检查"),
	plainRoute("health", http.MethodGet, "/api/metrics", RouteAccessHealth, "", "Prometheus 指标抓取"),

	metaRoute("auth", http.MethodGet, "/api/auth/captcha", RouteAccessPublic, shared.AuthCaptcha),
	metaRoute("auth", http.MethodPost, "/api/auth/login", RouteAccessPublic, shared.AuthLogin),
	metaRoute("auth", http.MethodPost, "/api/auth/refresh", RouteAccessAuth, shared.AuthRefresh),
	metaRoute("auth", http.MethodPost, "/api/auth/logout", RouteAccessAuth, shared.AuthLogout),
	metaRoute("auth", http.MethodGet, "/api/auth/codes", RouteAccessAuth, shared.AuthCodes),
	metaRoute("auth", http.MethodGet, "/api/auth/profile", RouteAccessAuth, shared.AuthProfile),
	metaRoute("auth", http.MethodPost, "/api/auth/verify-account", RouteAccessPublic, shared.AuthVerifyAccount),
	metaRoute("profile", http.MethodGet, "/api/profile", RouteAccessAuth, shared.ProfileMine),
	metaRoute("profile", http.MethodPost, "/api/profile/check-secure", RouteAccessAuth, shared.ProfileCheckSecure),
	metaRoute("profile", http.MethodPost, "/api/profile/check-mfa", RouteAccessAuth, shared.ProfileCheckMFA),

	metaRoute("admin", http.MethodGet, "/api/admins", RouteAccessAuth, shared.AdminList),
	metaRoute("admin", http.MethodPost, "/api/admins/exports", RouteAccessAuth, shared.AdminExportTrigger),
	metaRoute("admin", http.MethodGet, "/api/admins/exports/status/:jobId", RouteAccessAuth, shared.AdminExportStatus),
	metaRoute("admin", http.MethodGet, "/api/admins/exports/download/:jobId", RouteAccessAuth, shared.AdminExportDownload),
	metaRoute("admin", http.MethodPost, "/api/admins", RouteAccessAuth, shared.AdminAdd),
	metaRoute("admin", http.MethodPatch, "/api/admins/status/:id", RouteAccessAuth, shared.AdminStatusUpdate),
	metaRoute("admin", http.MethodPost, "/api/admins/password/reset/:id", RouteAccessAuth, shared.AdminPasswordReset),
	metaRoute("admin", http.MethodPost, "/api/admins/initial-state/reset/:id", RouteAccessAuth, shared.AdminResetInitialState),
	metaRoute("admin", http.MethodGet, "/api/admins/roles/:id", RouteAccessAuth, shared.AdminRoleList),
	metaRoute("admin", http.MethodPatch, "/api/admins/roles/:id", RouteAccessAuth, shared.AdminRoleUpdate),
	metaRoute("admin", http.MethodGet, "/api/admins/:id", RouteAccessAuth, shared.AdminInfo),
	metaRoute("admin", http.MethodPatch, "/api/admins/:id", RouteAccessAuth, shared.AdminUpdate),
	metaRoute("admin", http.MethodDelete, "/api/admins/:id", RouteAccessAuth, shared.AdminDelete),
	metaRoute("profile", http.MethodPatch, "/api/admins/mfa-status/:id", RouteAccessAuth, shared.AdminMFAStatus),
	metaRoute("profile", http.MethodGet, "/api/admins/mfa-secret-key-url/:id", RouteAccessAuth, shared.AdminBuildMFAURL),
	metaRoute("profile", http.MethodPatch, "/api/profile/password", RouteAccessAuth, shared.ProfileUpdatePassword),
	metaRoute("profile", http.MethodPatch, "/api/profile", RouteAccessAuth, shared.ProfileUpdateMine),
	metaRoute("profile", http.MethodPatch, "/api/profile/mfa-status", RouteAccessAuth, shared.ProfileUpdateMFA),
	metaRoute("profile", http.MethodPatch, "/api/profile/mfa-secret", RouteAccessAuth, shared.ProfileUpdateMFAKey),
	metaRoute("profile", http.MethodPost, "/api/profile/mfa-secret/refresh", RouteAccessAuth, shared.ProfileRefreshMFAKey),
	metaRoute("profile", http.MethodPatch, "/api/profile/avatar", RouteAccessAuth, shared.ProfileUpdateAvatar),
	metaRoute("rbac", http.MethodGet, "/api/roles", RouteAccessAuth, shared.RoleList),
	metaRoute("rbac", http.MethodGet, "/api/roles/tree", RouteAccessAuth, shared.RoleTreeList),
	metaRoute("rbac", http.MethodGet, "/api/roles/tree-options", RouteAccessAuth, shared.RoleTreeOptions),
	metaRoute("rbac", http.MethodPost, "/api/roles", RouteAccessAuth, shared.RoleAdd),
	metaRoute("rbac", http.MethodPatch, "/api/roles/:id", RouteAccessAuth, shared.RoleUpdate),
	metaRoute("rbac", http.MethodPatch, "/api/roles/status/:id", RouteAccessAuth, shared.RoleStatusUpdate),
	metaRoute("rbac", http.MethodPatch, "/api/roles/permissions/:id", RouteAccessAuth, shared.RolePermissionUpdate),
	metaRoute("rbac", http.MethodGet, "/api/roles/permissions/tree/:id/:isPid", RouteAccessAuth, shared.RolePermissionTree),
	metaRoute("rbac", http.MethodDelete, "/api/roles/:id", RouteAccessAuth, shared.RoleDelete),
	metaRoute("rbac", http.MethodGet, "/api/permissions", RouteAccessAuth, shared.PermissionList),
	metaRoute("rbac", http.MethodGet, "/api/permissions/tree", RouteAccessAuth, shared.PermissionTreeList),
	metaRoute("rbac", http.MethodGet, "/api/permissions/max-uuid", RouteAccessAuth, shared.PermissionMaxUUID),
	metaRoute("rbac", http.MethodPost, "/api/permissions", RouteAccessAuth, shared.PermissionAdd),
	metaRoute("rbac", http.MethodPatch, "/api/permissions/:id", RouteAccessAuth, shared.PermissionUpdate),
	metaRoute("rbac", http.MethodPatch, "/api/permissions/status/:id", RouteAccessAuth, shared.PermissionStatus),
	metaRoute("rbac", http.MethodDelete, "/api/permissions/:id", RouteAccessAuth, shared.PermissionDelete),
	metaRoute("config", http.MethodGet, "/api/dicts", RouteAccessAuth, shared.SysConfigList),
	metaRoute("config", http.MethodPost, "/api/dicts", RouteAccessAuth, shared.SysConfigAdd),
	metaRoute("config", http.MethodGet, "/api/dicts/export", RouteAccessAuth, shared.SysConfigExport),
	metaRoute("config", http.MethodPost, "/api/dicts/import", RouteAccessAuth, shared.SysConfigImport),
	metaRoute("config", http.MethodPatch, "/api/dicts/:id", RouteAccessAuth, shared.SysConfigUpdate),
	metaRoute("config", http.MethodGet, "/api/dicts/cache/:uuid", RouteAccessAuth, shared.SysConfigCache),
	metaRoute("config", http.MethodPost, "/api/dicts/cache/refresh/:uuid", RouteAccessAuth, shared.SysConfigRenew),
	metaRoute("cache", http.MethodGet, "/api/caches", RouteAccessAuth, shared.CacheList),
	metaRoute("cache", http.MethodGet, "/api/caches/server-info", RouteAccessAuth, shared.CacheServerInfo),
	metaRoute("cache", http.MethodGet, "/api/caches/key-info", RouteAccessAuth, shared.CacheKeyInfo),
	metaRoute("cache", http.MethodGet, "/api/caches/keys", RouteAccessAuth, shared.CacheSearch),
	metaRoute("cache", http.MethodGet, "/api/caches/key-info/search", RouteAccessAuth, shared.CacheKeyInfo),
	metaRoute("cache", http.MethodPost, "/api/caches/refresh", RouteAccessAuth, shared.CacheRenew),
	metaRoute("cache", http.MethodPost, "/api/caches/refresh-all", RouteAccessAuth, shared.CacheRenewAll),
	metaRoute("cache", http.MethodPost, "/api/caches/warmup", RouteAccessAuth, shared.CacheWarmup),
	metaRoute("admin_log", http.MethodGet, "/api/admin-logs", RouteAccessAuth, shared.AdminLogQuery),
	metaRoute("secret_key", http.MethodGet, "/api/secret-keys", RouteAccessAuth, shared.SecretKeyList),
	metaRoute("secret_key", http.MethodGet, "/api/secret-keys/:id", RouteAccessAuth, shared.SecretKeyGet),
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys", RouteAccessAuth, shared.SecretKeyAdd),
	metaRoute("secret_key", http.MethodPatch, "/api/secret-keys/:id", RouteAccessAuth, shared.SecretKeyUpdate),
	metaRoute("secret_key", http.MethodPatch, "/api/secret-keys/status/:id", RouteAccessAuth, shared.SecretKeyStatus),
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/cache/refresh/:uuid", RouteAccessAuth, shared.SecretKeyRenew),
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/validate", RouteAccessAuth, shared.SecretKeyValidate),
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/self-check/:uuid", RouteAccessAuth, shared.SecretKeySelfCheck),
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/sign", RouteAccessAuth, shared.SecurityDebugSign),
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/verify", RouteAccessAuth, shared.SecurityDebugVerify),
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/encrypt", RouteAccessAuth, shared.SecurityDebugEncrypt),
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/decrypt", RouteAccessAuth, shared.SecurityDebugDecrypt),

	metaRoute("message", http.MethodGet, "/api/admin-messages", RouteAccessAuth, shared.AdminMessageList),
	metaRoute("message", http.MethodGet, "/api/admin-messages/sent", RouteAccessAuth, shared.AdminMessageSentList),
	metaRoute("message", http.MethodGet, "/api/admin-messages/:id/receivers", RouteAccessAuth, shared.AdminMessageReceivers),
	metaRoute("message", http.MethodGet, "/api/admin-messages/unread-count", RouteAccessAuth, shared.AdminMessageUnreadCount),
	metaRoute("message", http.MethodGet, "/api/admin-messages/notifications", RouteAccessAuth, shared.AdminMessageNotifications),
	metaRoute("message", http.MethodPatch, "/api/admin-messages/read", RouteAccessAuth, shared.AdminMessageMarkRead),
	metaRoute("message", http.MethodPost, "/api/admin-messages/delete", RouteAccessAuth, shared.AdminMessageDelete),
	metaRoute("message", http.MethodPost, "/api/admin-messages/send", RouteAccessAuth, shared.AdminMessageSend),
	metaRoute("message", http.MethodPost, "/api/admin-messages/handle", RouteAccessAuth, shared.AdminMessageHandle),

	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/init", RouteAccessAuth, string(middleware.Ignore), "初始化断点续传上传会话"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/upload/status", RouteAccessAuth, string(middleware.Ignore), "查询断点续传上传状态"),
	plainRoute("transfer", http.MethodPut, "/api/file-transfer/upload/chunk", RouteAccessAuth, string(middleware.Ignore), "上传单个文件分片"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/download", RouteAccessAuth, string(middleware.Ignore), "下载当前管理员上传完成的文件"),
	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/complete", RouteAccessAuth, string(middleware.Ignore), "完成断点续传上传会话"),
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/access", RouteAccessPublic, "", "公开访问允许匿名预览的上传文件"),

	metaRoute("task", http.MethodPost, "/api/tasks", RouteAccessAuth, shared.TaskEnqueue),
	metaRoute("task", http.MethodGet, "/api/tasks", RouteAccessAuth, shared.TaskItemsList),
	metaRoute("task", http.MethodGet, "/api/tasks/overview", RouteAccessAuth, shared.TaskItemsList),
	metaRoute("task", http.MethodPost, "/api/tasks/workflows", RouteAccessAuth, shared.TaskWorkflowTrigger),
	metaRoute("task", http.MethodGet, "/api/tasks/workflows/:workflowId", RouteAccessAuth, shared.TaskWorkflowStatus),
	metaRoute("task", http.MethodGet, "/api/tasks/queues", RouteAccessAuth, shared.TaskQueueList),
	metaRoute("task", http.MethodGet, "/api/tasks/registry/task-types", RouteAccessAuth, shared.TaskQueueList),
	metaRoute("task", http.MethodGet, "/api/tasks/registry/workflows", RouteAccessAuth, shared.TaskWorkflowStatus),
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload", RouteAccessAuth, shared.TaskConfigReload),
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload/items", RouteAccessAuth, shared.TaskConfigItems),
	metaRoute("task", http.MethodPost, "/api/tasks/config-reload", RouteAccessAuth, shared.TaskConfigReloadRun),
	metaRoute("task", http.MethodGet, "/api/tasks/:taskId", RouteAccessAuth, shared.TaskInfoGet),
	metaRoute("task", http.MethodPost, "/api/tasks/queues/pause/:queue", RouteAccessAuth, shared.TaskQueuePause),
	metaRoute("task", http.MethodPost, "/api/tasks/run/:taskId", RouteAccessAuth, shared.TaskRun),
	metaRoute("task", http.MethodDelete, "/api/tasks/:taskId", RouteAccessAuth, shared.TaskDelete),
	metaRoute("task", http.MethodPost, "/api/tasks/queues/resume/:queue", RouteAccessAuth, shared.TaskQueueResume),

	metaRoute("collector", http.MethodGet, "/api/collector/overview", RouteAccessAuth, shared.CollectorOverview),
	metaRoute("collector", http.MethodGet, "/api/collector/tasks", RouteAccessAuth, shared.CollectorTaskList),
	metaRoute("collector", http.MethodPost, "/api/collector/run", RouteAccessAuth, shared.CollectorRun),
	metaRoute("collector", http.MethodPost, "/api/collector/tasks/retry", RouteAccessAuth, shared.CollectorRetry),

	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflows", RouteAccessAuth, shared.UserTagWorkflowTrigger),
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/recalculations", RouteAccessAuth, shared.UserTagRecalculate),
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflow-lease/release", RouteAccessAuth, shared.UserTagWorkflowLeaseRelease),
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/workflows", RouteAccessInternal, shared.UserTagWorkflowTrigger),
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/recalculations", RouteAccessInternal, shared.UserTagRecalculate),
	metaRoute("internal_auth", http.MethodPost, "/internal/auth/init-admin-bootstrap", RouteAccessInternal, shared.InternalInitAdminBootstrap),

	metaRoute("docs", http.MethodPost, "/api/docs/session", RouteAccessAuth, shared.DocsSession),
	plainRoute("docs", http.MethodGet, "/api/docs", RouteAccessDocs, "", "文档首页"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path", RouteAccessDocs, "", "文档静态资源"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub", RouteAccessDocs, "", "二级文档资源"),
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub/:file", RouteAccessDocs, "", "三级文档资源"),
}
