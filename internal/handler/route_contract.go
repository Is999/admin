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

// metaRoute 根据统一 RouteMeta 构造带权限、审计和说明的路由契约。
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

// plainRoute 构造不依赖业务 RouteMeta 的基础路由契约。
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

// defaultRouteContracts 定义 admin HTTP 路由对外暴露的完整契约清单。
var defaultRouteContracts = []RouteContract{
	// GET /api/live：存活检查。
	plainRoute("health", http.MethodGet, "/api/live", RouteAccessHealth, "", "存活检查"),
	// GET /api/ready：就绪检查。
	plainRoute("health", http.MethodGet, "/api/ready", RouteAccessHealth, "", "就绪检查"),
	// GET /api/metrics：Prometheus 指标抓取。
	plainRoute("health", http.MethodGet, "/api/metrics", RouteAccessHealth, "", "Prometheus 指标抓取"),

	// GET /api/auth/captcha：获取登录图形验证码。
	metaRoute("auth", http.MethodGet, "/api/auth/captcha", RouteAccessPublic, shared.AuthCaptcha),
	// POST /api/auth/login：管理员登录。
	metaRoute("auth", http.MethodPost, "/api/auth/login", RouteAccessPublic, shared.AuthLogin),
	// POST /api/auth/refresh：刷新访问令牌。
	metaRoute("auth", http.MethodPost, "/api/auth/refresh", RouteAccessAuth, shared.AuthRefresh),
	// POST /api/auth/logout：管理员退出登录。
	metaRoute("auth", http.MethodPost, "/api/auth/logout", RouteAccessAuth, shared.AuthLogout),
	// GET /api/auth/codes：获取当前用户权限码。
	metaRoute("auth", http.MethodGet, "/api/auth/codes", RouteAccessAuth, shared.AuthCodes),
	// GET /api/auth/profile：获取当前登录资料。
	metaRoute("auth", http.MethodGet, "/api/auth/profile", RouteAccessAuth, shared.AuthProfile),
	// POST /api/auth/verify-account：验证账号并生成MFA绑定信息。
	metaRoute("auth", http.MethodPost, "/api/auth/verify-account", RouteAccessPublic, shared.AuthVerifyAccount),
	// GET /api/profile：获取当前管理员资料。
	metaRoute("profile", http.MethodGet, "/api/profile", RouteAccessAuth, shared.ProfileMine),
	// POST /api/profile/check-secure：校验当前管理员密码。
	metaRoute("profile", http.MethodPost, "/api/profile/check-secure", RouteAccessAuth, shared.ProfileCheckSecure),
	// POST /api/profile/check-mfa：校验当前管理员MFA动态码。
	metaRoute("profile", http.MethodPost, "/api/profile/check-mfa", RouteAccessAuth, shared.ProfileCheckMFA),

	// GET /api/admins：查询管理员列表。
	metaRoute("admin", http.MethodGet, "/api/admins", RouteAccessAuth, shared.AdminList),
	// POST /api/admins/exports：异步导出管理员列表。
	metaRoute("admin", http.MethodPost, "/api/admins/exports", RouteAccessAuth, shared.AdminExportTrigger),
	// GET /api/admins/exports/status/:jobId：查询管理员导出进度。
	metaRoute("admin", http.MethodGet, "/api/admins/exports/status/:jobId", RouteAccessAuth, shared.AdminExportStatus),
	// GET /api/admins/exports/download/:jobId：下载管理员导出文件。
	metaRoute("admin", http.MethodGet, "/api/admins/exports/download/:jobId", RouteAccessAuth, shared.AdminExportDownload),
	// POST /api/admins：新增管理员。
	metaRoute("admin", http.MethodPost, "/api/admins", RouteAccessAuth, shared.AdminAdd),
	// PATCH /api/admins/status/:id：修改管理员状态。
	metaRoute("admin", http.MethodPatch, "/api/admins/status/:id", RouteAccessAuth, shared.AdminStatusUpdate),
	// POST /api/admins/password/reset/:id：重置管理员密码。
	metaRoute("admin", http.MethodPost, "/api/admins/password/reset/:id", RouteAccessAuth, shared.AdminPasswordReset),
	// POST /api/admins/initial-state/reset/:id：重置管理员到首次登录前状态。
	metaRoute("admin", http.MethodPost, "/api/admins/initial-state/reset/:id", RouteAccessAuth, shared.AdminResetInitialState),
	// GET /api/admins/roles/:id：查询管理员角色。
	metaRoute("admin", http.MethodGet, "/api/admins/roles/:id", RouteAccessAuth, shared.AdminRoleList),
	// PATCH /api/admins/roles/:id：编辑管理员角色。
	metaRoute("admin", http.MethodPatch, "/api/admins/roles/:id", RouteAccessAuth, shared.AdminRoleUpdate),
	// GET /api/admins/:id：查询管理员详情。
	metaRoute("admin", http.MethodGet, "/api/admins/:id", RouteAccessAuth, shared.AdminInfo),
	// PATCH /api/admins/:id：编辑管理员。
	metaRoute("admin", http.MethodPatch, "/api/admins/:id", RouteAccessAuth, shared.AdminUpdate),
	// DELETE /api/admins/:id：删除管理员。
	metaRoute("admin", http.MethodDelete, "/api/admins/:id", RouteAccessAuth, shared.AdminDelete),
	// PATCH /api/admins/mfa-status/:id：修改管理员MFA状态。
	metaRoute("profile", http.MethodPatch, "/api/admins/mfa-status/:id", RouteAccessAuth, shared.AdminMFAStatus),
	// GET /api/admins/mfa-secret-key-url/:id：生成管理员MFA绑定地址。
	metaRoute("profile", http.MethodGet, "/api/admins/mfa-secret-key-url/:id", RouteAccessAuth, shared.AdminBuildMFAURL),
	// PATCH /api/profile/password：个人中心修改密码。
	metaRoute("profile", http.MethodPatch, "/api/profile/password", RouteAccessAuth, shared.ProfileUpdatePassword),
	// PATCH /api/profile：个人中心修改资料。
	metaRoute("profile", http.MethodPatch, "/api/profile", RouteAccessAuth, shared.ProfileUpdateMine),
	// PATCH /api/profile/mfa-status：个人中心修改MFA状态。
	metaRoute("profile", http.MethodPatch, "/api/profile/mfa-status", RouteAccessAuth, shared.ProfileUpdateMFA),
	// PATCH /api/profile/mfa-secret：个人中心修改MFA秘钥。
	metaRoute("profile", http.MethodPatch, "/api/profile/mfa-secret", RouteAccessAuth, shared.ProfileUpdateMFAKey),
	// POST /api/profile/mfa-secret/refresh：个人中心重新生成MFA秘钥。
	metaRoute("profile", http.MethodPost, "/api/profile/mfa-secret/refresh", RouteAccessAuth, shared.ProfileRefreshMFAKey),
	// PATCH /api/profile/avatar：个人中心修改头像。
	metaRoute("profile", http.MethodPatch, "/api/profile/avatar", RouteAccessAuth, shared.ProfileUpdateAvatar),
	// GET /api/roles：查询角色列表。
	metaRoute("rbac", http.MethodGet, "/api/roles", RouteAccessAuth, shared.RoleList),
	// GET /api/roles/tree：查询角色树。
	metaRoute("rbac", http.MethodGet, "/api/roles/tree", RouteAccessAuth, shared.RoleTreeList),
	// GET /api/roles/tree-options：查询角色树下拉。
	metaRoute("rbac", http.MethodGet, "/api/roles/tree-options", RouteAccessAuth, shared.RoleTreeOptions),
	// POST /api/roles：新增角色。
	metaRoute("rbac", http.MethodPost, "/api/roles", RouteAccessAuth, shared.RoleAdd),
	// PATCH /api/roles/:id：编辑角色。
	metaRoute("rbac", http.MethodPatch, "/api/roles/:id", RouteAccessAuth, shared.RoleUpdate),
	// PATCH /api/roles/status/:id：修改角色状态。
	metaRoute("rbac", http.MethodPatch, "/api/roles/status/:id", RouteAccessAuth, shared.RoleStatusUpdate),
	// PATCH /api/roles/permissions/:id：编辑角色权限。
	metaRoute("rbac", http.MethodPatch, "/api/roles/permissions/:id", RouteAccessAuth, shared.RolePermissionUpdate),
	// GET /api/roles/permissions/tree/:id/:isPid：查询角色权限树。
	metaRoute("rbac", http.MethodGet, "/api/roles/permissions/tree/:id/:isPid", RouteAccessAuth, shared.RolePermissionTree),
	// DELETE /api/roles/:id：删除角色。
	metaRoute("rbac", http.MethodDelete, "/api/roles/:id", RouteAccessAuth, shared.RoleDelete),
	// GET /api/permissions：查询权限列表。
	metaRoute("rbac", http.MethodGet, "/api/permissions", RouteAccessAuth, shared.PermissionList),
	// GET /api/permissions/tree：查询权限树。
	metaRoute("rbac", http.MethodGet, "/api/permissions/tree", RouteAccessAuth, shared.PermissionTreeList),
	// GET /api/permissions/max-uuid：查询下一个权限UUID。
	metaRoute("rbac", http.MethodGet, "/api/permissions/max-uuid", RouteAccessAuth, shared.PermissionMaxUUID),
	// POST /api/permissions：新增权限。
	metaRoute("rbac", http.MethodPost, "/api/permissions", RouteAccessAuth, shared.PermissionAdd),
	// PATCH /api/permissions/:id：编辑权限。
	metaRoute("rbac", http.MethodPatch, "/api/permissions/:id", RouteAccessAuth, shared.PermissionUpdate),
	// PATCH /api/permissions/status/:id：修改权限状态。
	metaRoute("rbac", http.MethodPatch, "/api/permissions/status/:id", RouteAccessAuth, shared.PermissionStatus),
	// DELETE /api/permissions/:id：删除权限。
	metaRoute("rbac", http.MethodDelete, "/api/permissions/:id", RouteAccessAuth, shared.PermissionDelete),
	// GET /api/dicts：查询系统配置。
	metaRoute("config", http.MethodGet, "/api/dicts", RouteAccessAuth, shared.SysConfigList),
	// POST /api/dicts：新增系统配置。
	metaRoute("config", http.MethodPost, "/api/dicts", RouteAccessAuth, shared.SysConfigAdd),
	// GET /api/dicts/export：导出系统配置。
	metaRoute("config", http.MethodGet, "/api/dicts/export", RouteAccessAuth, shared.SysConfigExport),
	// POST /api/dicts/import：导入系统配置。
	metaRoute("config", http.MethodPost, "/api/dicts/import", RouteAccessAuth, shared.SysConfigImport),
	// PATCH /api/dicts/:id：编辑系统配置。
	metaRoute("config", http.MethodPatch, "/api/dicts/:id", RouteAccessAuth, shared.SysConfigUpdate),
	// GET /api/dicts/cache/:uuid：查看系统配置缓存。
	metaRoute("config", http.MethodGet, "/api/dicts/cache/:uuid", RouteAccessAuth, shared.SysConfigCache),
	// POST /api/dicts/cache/refresh/:uuid：刷新系统配置缓存。
	metaRoute("config", http.MethodPost, "/api/dicts/cache/refresh/:uuid", RouteAccessAuth, shared.SysConfigRenew),
	// GET /api/caches：查询缓存列表。
	metaRoute("cache", http.MethodGet, "/api/caches", RouteAccessAuth, shared.CacheList),
	// GET /api/caches/server-info：查看缓存服务器信息。
	metaRoute("cache", http.MethodGet, "/api/caches/server-info", RouteAccessAuth, shared.CacheServerInfo),
	// GET /api/caches/key-info：查看缓存键信息。
	metaRoute("cache", http.MethodGet, "/api/caches/key-info", RouteAccessAuth, shared.CacheKeyInfo),
	// GET /api/caches/keys：搜索缓存键。
	metaRoute("cache", http.MethodGet, "/api/caches/keys", RouteAccessAuth, shared.CacheSearch),
	// GET /api/caches/key-info/search：查看缓存键信息。
	metaRoute("cache", http.MethodGet, "/api/caches/key-info/search", RouteAccessAuth, shared.CacheKeyInfo),
	// POST /api/caches/refresh：刷新缓存。
	metaRoute("cache", http.MethodPost, "/api/caches/refresh", RouteAccessAuth, shared.CacheRenew),
	// POST /api/caches/refresh-all：刷新全部缓存。
	metaRoute("cache", http.MethodPost, "/api/caches/refresh-all", RouteAccessAuth, shared.CacheRenewAll),
	// POST /api/caches/warmup：按模板预热缓存。
	metaRoute("cache", http.MethodPost, "/api/caches/warmup", RouteAccessAuth, shared.CacheWarmup),
	// GET /api/admin-logs：查询管理员审计日志。
	metaRoute("admin_log", http.MethodGet, "/api/admin-logs", RouteAccessAuth, shared.AdminLogQuery),
	// GET /api/secret-keys：查询秘钥列表。
	metaRoute("secret_key", http.MethodGet, "/api/secret-keys", RouteAccessAuth, shared.SecretKeyList),
	// GET /api/secret-keys/:id：查询秘钥详情。
	metaRoute("secret_key", http.MethodGet, "/api/secret-keys/:id", RouteAccessAuth, shared.SecretKeyGet),
	// POST /api/secret-keys：新增秘钥。
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys", RouteAccessAuth, shared.SecretKeyAdd),
	// PATCH /api/secret-keys/:id：编辑秘钥。
	metaRoute("secret_key", http.MethodPatch, "/api/secret-keys/:id", RouteAccessAuth, shared.SecretKeyUpdate),
	// PATCH /api/secret-keys/status/:id：修改秘钥状态。
	metaRoute("secret_key", http.MethodPatch, "/api/secret-keys/status/:id", RouteAccessAuth, shared.SecretKeyStatus),
	// POST /api/secret-keys/cache/refresh/:uuid：刷新秘钥缓存。
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/cache/refresh/:uuid", RouteAccessAuth, shared.SecretKeyRenew),
	// POST /api/secret-keys/validate：预检秘钥配置。
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/validate", RouteAccessAuth, shared.SecretKeyValidate),
	// POST /api/secret-keys/self-check/:uuid：执行秘钥自检。
	metaRoute("secret_key", http.MethodPost, "/api/secret-keys/self-check/:uuid", RouteAccessAuth, shared.SecretKeySelfCheck),
	// POST /api/security/debug/sign：安全调试签名。
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/sign", RouteAccessAuth, shared.SecurityDebugSign),
	// POST /api/security/debug/verify：安全调试验签。
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/verify", RouteAccessAuth, shared.SecurityDebugVerify),
	// POST /api/security/debug/encrypt：安全调试加密。
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/encrypt", RouteAccessAuth, shared.SecurityDebugEncrypt),
	// POST /api/security/debug/decrypt：安全调试解密。
	metaRoute("security_debug", http.MethodPost, "/api/security/debug/decrypt", RouteAccessAuth, shared.SecurityDebugDecrypt),

	// GET /api/admin-messages：查询管理员消息收件箱。
	metaRoute("message", http.MethodGet, "/api/admin-messages", RouteAccessAuth, shared.AdminMessageList),
	// GET /api/admin-messages/sent：查询管理员已发送消息。
	metaRoute("message", http.MethodGet, "/api/admin-messages/sent", RouteAccessAuth, shared.AdminMessageSentList),
	// GET /api/admin-messages/:id/receivers：查询管理员消息收件人明细。
	metaRoute("message", http.MethodGet, "/api/admin-messages/:id/receivers", RouteAccessAuth, shared.AdminMessageReceivers),
	// GET /api/admin-messages/unread-count：查询管理员未读消息数量。
	metaRoute("message", http.MethodGet, "/api/admin-messages/unread-count", RouteAccessAuth, shared.AdminMessageUnreadCount),
	// GET /api/admin-messages/notifications：查询管理员通知列表。
	metaRoute("message", http.MethodGet, "/api/admin-messages/notifications", RouteAccessAuth, shared.AdminMessageNotifications),
	// PATCH /api/admin-messages/read：标记管理员消息已读。
	metaRoute("message", http.MethodPatch, "/api/admin-messages/read", RouteAccessAuth, shared.AdminMessageMarkRead),
	// POST /api/admin-messages/delete：删除管理员消息。
	metaRoute("message", http.MethodPost, "/api/admin-messages/delete", RouteAccessAuth, shared.AdminMessageDelete),
	// POST /api/admin-messages/send：发送管理员消息。
	metaRoute("message", http.MethodPost, "/api/admin-messages/send", RouteAccessAuth, shared.AdminMessageSend),
	// POST /api/admin-messages/handle：标记管理员消息已处理。
	metaRoute("message", http.MethodPost, "/api/admin-messages/handle", RouteAccessAuth, shared.AdminMessageHandle),

	// POST /api/file-transfer/upload/init：初始化断点续传上传会话。
	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/init", RouteAccessAuth, string(middleware.Ignore), "初始化断点续传上传会话"),
	// GET /api/file-transfer/upload/status：查询断点续传上传状态。
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/upload/status", RouteAccessAuth, string(middleware.Ignore), "查询断点续传上传状态"),
	// PUT /api/file-transfer/upload/chunk：上传单个文件分片。
	plainRoute("transfer", http.MethodPut, "/api/file-transfer/upload/chunk", RouteAccessAuth, string(middleware.Ignore), "上传单个文件分片"),
	// GET /api/file-transfer/download：下载当前管理员上传完成的文件。
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/download", RouteAccessAuth, string(middleware.Ignore), "下载当前管理员上传完成的文件"),
	// POST /api/file-transfer/upload/complete：完成断点续传上传会话。
	plainRoute("transfer", http.MethodPost, "/api/file-transfer/upload/complete", RouteAccessAuth, string(middleware.Ignore), "完成断点续传上传会话"),
	// GET /api/file-transfer/access：公开访问允许匿名预览的上传文件。
	plainRoute("transfer", http.MethodGet, "/api/file-transfer/access", RouteAccessPublic, "", "公开访问允许匿名预览的上传文件"),

	// POST /api/tasks：手动投递任务。
	metaRoute("task", http.MethodPost, "/api/tasks", RouteAccessAuth, shared.TaskEnqueue),
	// GET /api/tasks：查询任务列表。
	metaRoute("task", http.MethodGet, "/api/tasks", RouteAccessAuth, shared.TaskItemsList),
	// GET /api/tasks/overview：查询任务列表。
	metaRoute("task", http.MethodGet, "/api/tasks/overview", RouteAccessAuth, shared.TaskItemsList),
	// POST /api/tasks/workflows：手动触发工作流。
	metaRoute("task", http.MethodPost, "/api/tasks/workflows", RouteAccessAuth, shared.TaskWorkflowTrigger),
	// GET /api/tasks/workflows/:workflowId：查询工作流状态。
	metaRoute("task", http.MethodGet, "/api/tasks/workflows/:workflowId", RouteAccessAuth, shared.TaskWorkflowStatus),
	// GET /api/tasks/queues：查询任务队列概览。
	metaRoute("task", http.MethodGet, "/api/tasks/queues", RouteAccessAuth, shared.TaskQueueList),
	// GET /api/tasks/registry/task-types：查询任务队列概览。
	metaRoute("task", http.MethodGet, "/api/tasks/registry/task-types", RouteAccessAuth, shared.TaskQueueList),
	// GET /api/tasks/registry/workflows：查询工作流状态。
	metaRoute("task", http.MethodGet, "/api/tasks/registry/workflows", RouteAccessAuth, shared.TaskWorkflowStatus),
	// GET /api/tasks/config-reload：查询配置热加载状态。
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload", RouteAccessAuth, shared.TaskConfigReload),
	// GET /api/tasks/config-reload/items：查询配置热加载配置项。
	metaRoute("task", http.MethodGet, "/api/tasks/config-reload/items", RouteAccessAuth, shared.TaskConfigItems),
	// POST /api/tasks/config-reload：手动触发配置热加载。
	metaRoute("task", http.MethodPost, "/api/tasks/config-reload", RouteAccessAuth, shared.TaskConfigReloadRun),
	// GET /api/tasks/:taskId：查询任务详情。
	metaRoute("task", http.MethodGet, "/api/tasks/:taskId", RouteAccessAuth, shared.TaskInfoGet),
	// POST /api/tasks/queues/pause/:queue：暂停任务队列。
	metaRoute("task", http.MethodPost, "/api/tasks/queues/pause/:queue", RouteAccessAuth, shared.TaskQueuePause),
	// POST /api/tasks/run/:taskId：立即执行任务。
	metaRoute("task", http.MethodPost, "/api/tasks/run/:taskId", RouteAccessAuth, shared.TaskRun),
	// DELETE /api/tasks/:taskId：删除任务。
	metaRoute("task", http.MethodDelete, "/api/tasks/:taskId", RouteAccessAuth, shared.TaskDelete),
	// POST /api/tasks/queues/resume/:queue：恢复任务队列。
	metaRoute("task", http.MethodPost, "/api/tasks/queues/resume/:queue", RouteAccessAuth, shared.TaskQueueResume),

	// GET /api/collector/overview：查询Collector概览。
	metaRoute("collector", http.MethodGet, "/api/collector/overview", RouteAccessAuth, shared.CollectorOverview),
	// GET /api/collector/tasks：查询Collector任务。
	metaRoute("collector", http.MethodGet, "/api/collector/tasks", RouteAccessAuth, shared.CollectorTaskList),
	// POST /api/collector/run：手动执行Collector。
	metaRoute("collector", http.MethodPost, "/api/collector/run", RouteAccessAuth, shared.CollectorRun),
	// POST /api/collector/tasks/retry：手动重试Collector任务。
	metaRoute("collector", http.MethodPost, "/api/collector/tasks/retry", RouteAccessAuth, shared.CollectorRetry),

	// POST /api/user-tags/workflows：触发用户标签工作流。
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflows", RouteAccessAuth, shared.UserTagWorkflowTrigger),
	// POST /api/user-tags/recalculations：指定标签重新计算。
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/recalculations", RouteAccessAuth, shared.UserTagRecalculate),
	// POST /api/user-tags/workflow-lease/release：释放用户标签工作流互斥锁。
	metaRoute("user_tag", http.MethodPost, "/api/user-tags/workflow-lease/release", RouteAccessAuth, shared.UserTagWorkflowLeaseRelease),
	// POST /internal/user-tags/workflows：触发用户标签工作流。
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/workflows", RouteAccessInternal, shared.UserTagWorkflowTrigger),
	// POST /internal/user-tags/recalculations：指定标签重新计算。
	metaRoute("internal_user_tag", http.MethodPost, "/internal/user-tags/recalculations", RouteAccessInternal, shared.UserTagRecalculate),
	// POST /internal/auth/init-admin-bootstrap：内网初始化管理员账号。
	metaRoute("internal_auth", http.MethodPost, "/internal/auth/init-admin-bootstrap", RouteAccessInternal, shared.InternalInitAdminBootstrap),

	// POST /api/docs/session：创建文档访问会话。
	metaRoute("docs", http.MethodPost, "/api/docs/session", RouteAccessAuth, shared.DocsSession),
	// GET /api/docs：文档首页。
	plainRoute("docs", http.MethodGet, "/api/docs", RouteAccessDocs, "", "文档首页"),
	// GET /api/docs/:path：文档静态资源。
	plainRoute("docs", http.MethodGet, "/api/docs/:path", RouteAccessDocs, "", "文档静态资源"),
	// GET /api/docs/:path/:sub：二级文档资源。
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub", RouteAccessDocs, "", "二级文档资源"),
	// GET /api/docs/:path/:sub/:file：三级文档资源。
	plainRoute("docs", http.MethodGet, "/api/docs/:path/:sub/:file", RouteAccessDocs, "", "三级文档资源"),
}
