package handler

import (
	"admin_cron/internal/middleware"
	"admin_cron/internal/model"
)

// RouteMeta 描述一条业务路由的统一元数据。
// 统一收口后，路由注册、审计日志、链路追踪都复用这一份定义，避免同一条路由维护多套信息。
type RouteMeta struct {
	Alias    middleware.RouteAlias // 统一路由别名，供鉴权/日志/trace 对齐
	Action   model.AdminLogAction  // 管理员审计动作；为空表示该路由不写管理员操作审计
	Describe string                // 中文业务说明，供审计日志直接复用
}

// newRouteMeta 创建普通路由元数据。
func newRouteMeta(alias middleware.RouteAlias, describe string) RouteMeta {
	return RouteMeta{
		Alias:    alias,
		Describe: describe,
	}
}

// newAuditRouteMeta 创建带管理员审计动作的路由元数据。
func newAuditRouteMeta(alias middleware.RouteAlias, action model.AdminLogAction, describe string) RouteMeta {
	return RouteMeta{
		Alias:    alias,
		Action:   action,
		Describe: describe,
	}
}

var (
	// 认证模块
	AuthCaptcha                = newRouteMeta("auth.captcha", "获取登录图形验证码")
	AuthLogin                  = newRouteMeta("auth.login", "管理员登录")
	AuthRefresh                = newRouteMeta("auth.refresh", "刷新访问令牌")
	AuthLogout                 = newRouteMeta("auth.logout", "管理员退出登录")
	AuthCodes                  = newRouteMeta("auth.codes", "获取当前用户权限码")
	AuthLoginAfterInfo         = newRouteMeta("auth.login_after_info", "获取登录后初始化信息")
	DocsSession                = newRouteMeta("docs.index", "创建文档访问会话")
	InternalInitAdminBootstrap = newRouteMeta("internal.auth.init_admin_bootstrap", "内网初始化管理员账号")
	UserBuildSecret            = newRouteMeta("user.build_secret_verify_account", "验证账号并生成MFA绑定信息")
	UserMine                   = newRouteMeta("user.mine", "获取当前管理员资料")
	UserPermissions            = newRouteMeta("user.permissions", "获取当前管理员角色权限")
	UserCheckSecure            = newRouteMeta("user.check_secure", "校验当前管理员密码")
	UserCheckMFA               = newRouteMeta("user.check_mfa_secure", "校验当前管理员MFA动态码")
	UserUpdatePassword         = newAuditRouteMeta("user.update_password", model.ActionAdminPasswordReset, "个人中心修改密码")
	UserUpdateMine             = newAuditRouteMeta("user.update_mine", model.ActionAdminUpdate, "个人中心修改资料")
	UserUpdateMFA              = newAuditRouteMeta("user.update_mfa_status", model.ActionAdminUpdate, "个人中心修改MFA状态")
	UserUpdateMFAKey           = newAuditRouteMeta("user.update_mfa_secret", model.ActionAdminUpdate, "个人中心修改MFA秘钥")
	UserRefreshMFAKey          = newAuditRouteMeta("user.refresh_mfa_secret", model.ActionAdminUpdate, "个人中心重新生成MFA秘钥")
	UserUpdateAvatar           = newAuditRouteMeta("user.update_avatar", model.ActionAdminUpdate, "个人中心修改头像")
	UserBuildMFAURL            = newAuditRouteMeta("user.build_mfa_url", model.ActionAdminUpdate, "生成管理员MFA绑定地址")
	UserAdminMFAStatus         = newAuditRouteMeta("user.admin_mfa_status", model.ActionAdminUpdate, "修改管理员MFA状态")

	// 管理员模块
	AdminLogQuery          = newAuditRouteMeta("admin.log.query", model.ActionAdminLogQuery, "查询管理员审计日志")
	AdminAdd               = newAuditRouteMeta("admin.add", model.ActionAdminAdd, "新增管理员")
	AdminList              = newAuditRouteMeta("admin.list", model.ActionAdminList, "查询管理员列表")
	AdminInfo              = newAuditRouteMeta("admin.info", model.ActionAdminInfo, "查询管理员详情")
	AdminUpdate            = newAuditRouteMeta("admin.update", model.ActionAdminUpdate, "编辑管理员")
	AdminDelete            = newAuditRouteMeta("admin.delete", model.ActionAdminDelete, "删除管理员")
	AdminStatusUpdate      = newAuditRouteMeta("admin.status.update", model.ActionAdminStatusUpdate, "修改管理员状态")
	AdminPasswordReset     = newAuditRouteMeta("admin.password.reset", model.ActionAdminPasswordReset, "重置管理员密码")
	AdminResetInitialState = newAuditRouteMeta("admin.reset.initial_state", model.ActionAdminPasswordReset, "重置管理员到首次登录前状态")
	AdminRoleList          = newAuditRouteMeta("admin.role.list", model.ActionAdminRoleList, "查询管理员角色")
	AdminRoleUpdate        = newAuditRouteMeta("admin.role.update", model.ActionAdminRoleUpdate, "编辑管理员角色")
	AdminRoleAdd           = newAuditRouteMeta("admin.role.add", model.ActionAdminRoleAdd, "添加管理员角色")
	AdminRoleDelete        = newAuditRouteMeta("admin.role.delete", model.ActionAdminRoleDelete, "解除管理员角色")
	AdminExportTrigger     = newAuditRouteMeta("admin.export", model.ActionAdminExport, "异步导出管理员列表")
	AdminExportStatus      = newAuditRouteMeta("admin.export.status", model.ActionAdminExportStatus, "查询管理员导出进度")
	AdminExportDownload    = newAuditRouteMeta("admin.export.download", model.ActionAdminExportDownload, "下载管理员导出文件")

	// 消息中心模块
	AdminMessageList          = newAuditRouteMeta("message.list", model.ActionAdminMessageList, "查询管理员消息收件箱")
	AdminMessageSentList      = newAuditRouteMeta("message.sent_list", model.ActionAdminMessageSentList, "查询管理员已发送消息")
	AdminMessageReceivers     = newAuditRouteMeta("message.receivers", model.ActionAdminMessageReceivers, "查询管理员消息收件人明细")
	AdminMessageSend          = newAuditRouteMeta("message.send", model.ActionAdminMessageSend, "发送管理员消息")
	AdminMessageMarkRead      = newAuditRouteMeta("message.mark_read", model.ActionAdminMessageMarkRead, "标记管理员消息已读")
	AdminMessageDelete        = newAuditRouteMeta("message.delete", model.ActionAdminMessageDelete, "删除管理员消息")
	AdminMessageHandle        = newAuditRouteMeta("message.handle", model.ActionAdminMessageHandle, "标记管理员消息已处理")
	AdminMessageUnreadCount   = newRouteMeta("message.unread_count", "查询管理员未读消息数量")
	AdminMessageNotifications = newRouteMeta("message.notifications", "查询管理员通知列表")
	RoleList                  = newAuditRouteMeta("role.list", model.ActionRoleList, "查询角色列表")
	RoleTreeList              = newRouteMeta("role.tree.list", "查询角色树")
	RoleTreeOptions           = newRouteMeta("role.tree.options", "查询角色树下拉")
	RoleAdd                   = newAuditRouteMeta("role.add", model.ActionRoleAdd, "新增角色")
	RoleUpdate                = newAuditRouteMeta("role.update", model.ActionRoleUpdate, "编辑角色")
	RoleDelete                = newAuditRouteMeta("role.delete", model.ActionRoleDelete, "删除角色")
	RoleStatusUpdate          = newAuditRouteMeta("role.status.update", model.ActionRoleStatusUpdate, "修改角色状态")
	RolePermissionTree        = newRouteMeta("role.permission.tree", "查询角色权限树")
	RolePermissionUpdate      = newAuditRouteMeta("role.permission.update", model.ActionRolePermissionUpdate, "编辑角色权限")
	PermissionList            = newAuditRouteMeta("permission.list", model.ActionPermissionList, "查询权限列表")
	PermissionTreeList        = newRouteMeta("permission.tree.list", "查询权限树")
	PermissionMaxUUID         = newRouteMeta("permission.max_uuid", "查询下一个权限UUID")
	PermissionAdd             = newAuditRouteMeta("permission.add", model.ActionPermissionAdd, "新增权限")
	PermissionUpdate          = newAuditRouteMeta("permission.update", model.ActionPermissionUpdate, "编辑权限")
	PermissionDelete          = newAuditRouteMeta("permission.delete", model.ActionPermissionDelete, "删除权限")
	PermissionStatus          = newAuditRouteMeta("permission.status.update", model.ActionPermissionStatus, "修改权限状态")
	SysConfigList             = newAuditRouteMeta("system.config.list", model.ActionSysConfigList, "查询系统配置")
	SysConfigAdd              = newAuditRouteMeta("system.config.add", model.ActionSysConfigAdd, "新增系统配置")
	SysConfigUpdate           = newAuditRouteMeta("system.config.update", model.ActionSysConfigUpdate, "编辑系统配置")
	SysConfigExport           = newAuditRouteMeta("system.config.export", model.ActionSysConfigExport, "导出系统配置")
	SysConfigImport           = newAuditRouteMeta("system.config.import", model.ActionSysConfigImport, "导入系统配置")
	SysConfigCache            = newAuditRouteMeta("system.config.cache", model.ActionSysConfigCache, "查看系统配置缓存")
	SysConfigRenew            = newAuditRouteMeta("system.config.renew", model.ActionSysConfigRenew, "刷新系统配置缓存")
	CacheList                 = newAuditRouteMeta("cache.list", model.ActionCacheList, "查询缓存列表")
	CacheServerInfo           = newAuditRouteMeta("cache.server.info", model.ActionCacheInfo, "查看缓存服务器信息")
	CacheKeyInfo              = newAuditRouteMeta("cache.key.info", model.ActionCacheInfo, "查看缓存键信息")
	CacheSearch               = newAuditRouteMeta("cache.search", model.ActionCacheSearch, "搜索缓存键")
	CacheRenew                = newAuditRouteMeta("cache.renew", model.ActionCacheRenew, "刷新缓存")
	CacheRenewAll             = newAuditRouteMeta("cache.renew.all", model.ActionCacheRenewAll, "刷新全部缓存")
	CacheWarmup               = newAuditRouteMeta("cache.warmup", model.ActionCacheWarmup, "按模板预热缓存")
	SecretKeyList             = newAuditRouteMeta("secretKey.index", model.ActionSecretKeyList, "查询秘钥列表")
	SecretKeyGet              = newAuditRouteMeta("secretKey.get", model.ActionSecretKeyGet, "查询秘钥详情")
	SecretKeyAdd              = newAuditRouteMeta("secretKey.add", model.ActionSecretKeyAdd, "新增秘钥")
	SecretKeyUpdate           = newAuditRouteMeta("secretKey.edit", model.ActionSecretKeyUpdate, "编辑秘钥")
	SecretKeyStatus           = newAuditRouteMeta("secretKey.editStatus", model.ActionSecretKeyStatus, "修改秘钥状态")
	SecretKeyRenew            = newAuditRouteMeta("secretKey.renew", model.ActionSecretKeyRenew, "刷新秘钥缓存")
	SecretKeyValidate         = newAuditRouteMeta("secretKey.validate", model.ActionSecretKeyValidate, "预检秘钥配置")
	SecretKeySelfCheck        = newAuditRouteMeta("secretKey.self_check", model.ActionSecretKeySelfCheck, "执行秘钥自检")
	SecurityDebugSign         = newAuditRouteMeta("security.debug.sign", model.ActionSecurityDebugSign, "安全调试签名")
	SecurityDebugVerify       = newAuditRouteMeta("security.debug.verify", model.ActionSecurityDebugVerify, "安全调试验签")
	SecurityDebugEncrypt      = newAuditRouteMeta("security.debug.encrypt", model.ActionSecurityDebugEncrypt, "安全调试加密")
	SecurityDebugDecrypt      = newAuditRouteMeta("security.debug.decrypt", model.ActionSecurityDebugDecrypt, "安全调试解密")

	// 任务系统模块
	TaskEnqueue         = newAuditRouteMeta("task.enqueue", model.ActionTaskEnqueue, "手动投递任务")
	TaskInfoGet         = newAuditRouteMeta("task.info.get", model.ActionTaskInfoGet, "查询任务详情")
	TaskItemsList       = newAuditRouteMeta("task.items.list", model.ActionTaskItemsList, "查询任务列表")
	TaskRun             = newAuditRouteMeta("task.run", model.ActionTaskRun, "立即执行任务")
	TaskDelete          = newAuditRouteMeta("task.delete", model.ActionTaskDelete, "删除任务")
	TaskWorkflowTrigger = newAuditRouteMeta("task.workflow.trigger", model.ActionTaskWorkflowTrigger, "手动触发工作流")
	TaskWorkflowStatus  = newAuditRouteMeta("task.workflow.status", model.ActionTaskWorkflowStatus, "查询工作流状态")
	TaskQueueList       = newAuditRouteMeta("task.queue.list", model.ActionTaskQueueList, "查询任务队列概览")
	TaskConfigItems     = newAuditRouteMeta("task.config.reload.items", model.ActionTaskConfigReloadItems, "查询配置热加载配置项")
	TaskConfigReload    = newAuditRouteMeta("task.config.reload.status", model.ActionTaskConfigReloadStatus, "查询配置热加载状态")
	TaskConfigReloadRun = newAuditRouteMeta("task.config.reload.run", model.ActionTaskConfigReloadRun, "手动触发配置热加载")
	TaskQueuePause      = newAuditRouteMeta("task.queue.pause", model.ActionTaskQueuePause, "暂停任务队列")
	TaskQueueResume     = newAuditRouteMeta("task.queue.resume", model.ActionTaskQueueResume, "恢复任务队列")

	// 通用收集器模块
	CollectorOverview = newAuditRouteMeta("collector.overview", model.ActionCollectorOverview, "查询Collector概览")
	CollectorTaskList = newAuditRouteMeta("collector.task.list", model.ActionCollectorTaskList, "查询Collector任务")
	CollectorRun      = newAuditRouteMeta("collector.run", model.ActionCollectorRun, "手动执行Collector")
	CollectorRetry    = newAuditRouteMeta("collector.task.retry", model.ActionCollectorRetry, "手动重试Collector任务")

	// 用户标签模块
	UserTagWorkflowTrigger      = newAuditRouteMeta("user_tag.workflow.trigger", model.ActionUserTagWorkflowTrigger, "触发用户标签工作流")
	UserTagRecalculate          = newAuditRouteMeta("user_tag.recalculate", model.ActionUserTagRecalculate, "指定标签重新计算")
	UserTagWorkflowLeaseRelease = newAuditRouteMeta("user_tag.workflow_lease.release", model.ActionUserTagWorkflowLeaseRelease, "释放用户标签工作流互斥锁")
)
