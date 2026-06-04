package routealias

// Alias 是权限、审计、日志和安全策略共用的稳定路由别名。
type Alias string

const (
	// Ignore 表示已登录但不绑定业务权限码的通用接口。
	Ignore Alias = "ignore"
	// AuthLogin 表示管理员登录路由别名。
	AuthLogin Alias = "auth.login"
	// AuthRefresh 表示刷新访问令牌路由别名。
	AuthRefresh Alias = "auth.refresh"
	// AuthLogout 表示管理员退出登录路由别名。
	AuthLogout Alias = "auth.logout"
	// AuthCodes 表示获取当前用户权限码路由别名。
	AuthCodes Alias = "auth.codes"
	// AuthProfile 表示获取当前登录资料路由别名。
	AuthProfile Alias = "auth.profile"
	// AuthVerifyAccount 表示登录预校验路由别名。
	AuthVerifyAccount Alias = "auth.verify_account"
	// DocsIndex 表示后台接口文档入口页面权限路由别名。
	DocsIndex Alias = "docs.index"
	// DocsRoleOps 表示运维角色文档目录路由别名。
	DocsRoleOps Alias = "docs.role.ops"
	// DocsRoleBackend 表示后端开发角色文档目录路由别名。
	DocsRoleBackend Alias = "docs.role.backend"
	// DocsRoleFrontend 表示前端与测试角色文档目录路由别名。
	DocsRoleFrontend Alias = "docs.role.frontend"
	// DocsFeatureTask 表示任务系统功能文档目录路由别名。
	DocsFeatureTask Alias = "docs.feature.task"
	// DocsFeatureUserTag 表示用户标签功能文档目录路由别名。
	DocsFeatureUserTag Alias = "docs.feature.user_tag"
	// DocsAPIIndex 表示接口文档首页和统一规范路由别名。
	DocsAPIIndex Alias = "docs.api.index"
	// DocsAPIAdmin 表示后台系统接口文档目录路由别名。
	DocsAPIAdmin Alias = "docs.api.admin"
	// DocsAPITask 表示任务系统接口文档目录路由别名。
	DocsAPITask Alias = "docs.api.task"
	// DocsUserTag 表示用户标签接口文档目录路由别名。
	DocsUserTag Alias = "docs.user_tag"
	// DocsAPIServiceIndex 表示前台 API 文档入口页面、首页和规范路由别名。
	DocsAPIServiceIndex Alias = "docs.api_service.index"
	// DocsAPIServiceFront 表示前台 API 前台系统接口文档目录路由别名。
	DocsAPIServiceFront Alias = "docs.api_service.front"
)

const (
	// ProfileMine 表示当前管理员资料路由别名。
	ProfileMine Alias = "profile.mine"
	// ProfileCheckSecure 表示校验当前管理员密码路由别名。
	ProfileCheckSecure Alias = "profile.check_secure"
	// ProfileCheckMFA 表示校验当前管理员 MFA 动态码路由别名。
	ProfileCheckMFA Alias = "profile.check_mfa"
	// ProfileUpdatePassword 表示个人中心修改密码路由别名。
	ProfileUpdatePassword Alias = "profile.update_password"
	// ProfileUpdateMine 表示个人中心修改资料路由别名。
	ProfileUpdateMine Alias = "profile.update_mine"
	// ProfileUpdateMFAStatus 表示个人中心修改 MFA 状态路由别名。
	ProfileUpdateMFAStatus Alias = "profile.update_mfa_status"
	// ProfileUpdateMFASecret 表示个人中心修改 MFA 秘钥路由别名。
	ProfileUpdateMFASecret Alias = "profile.update_mfa_secret"
	// ProfileRefreshMFASecret 表示个人中心重新生成 MFA 秘钥路由别名。
	ProfileRefreshMFASecret Alias = "profile.refresh_mfa_secret"
	// ProfileUpdateAvatar 表示个人中心修改头像路由别名。
	ProfileUpdateAvatar Alias = "profile.update_avatar"
)

const (
	// AdminAdd 表示新增管理员路由别名。
	AdminAdd Alias = "admin.add"
	// AdminUpdate 表示编辑管理员路由别名。
	AdminUpdate Alias = "admin.update"
	// AdminDelete 表示删除管理员路由别名。
	AdminDelete Alias = "admin.delete"
	// AdminStatusUpdate 表示修改管理员状态路由别名。
	AdminStatusUpdate Alias = "admin.status.update"
	// AdminPasswordReset 表示重置管理员密码路由别名。
	AdminPasswordReset Alias = "admin.password.reset"
	// AdminResetInitialState 表示重置管理员首次状态路由别名。
	AdminResetInitialState Alias = "admin.reset.initial_state"
	// AdminRoleUpdate 表示编辑管理员角色路由别名。
	AdminRoleUpdate Alias = "admin.role.update"
	// AdminBuildMFAURL 表示生成管理员 MFA 绑定地址路由别名。
	AdminBuildMFAURL Alias = "admin.mfa_secret_url"
	// AdminMFAStatus 表示修改管理员 MFA 状态路由别名。
	AdminMFAStatus Alias = "admin.mfa_status.update"
)

const (
	// UserList 表示查询前台用户列表路由别名。
	UserList Alias = "user.list"
	// UserInfo 表示查询前台用户详情路由别名。
	UserInfo Alias = "user.info"
	// UserAdd 表示新增前台用户路由别名。
	UserAdd Alias = "user.add"
	// UserUpdate 表示编辑前台用户资料路由别名。
	UserUpdate Alias = "user.update"
	// UserStatusUpdate 表示修改前台用户状态路由别名。
	UserStatusUpdate Alias = "user.status.update"
	// UserPasswordReset 表示重置前台用户密码路由别名。
	UserPasswordReset Alias = "user.password.reset"
	// UserRuntimeSync 表示同步前台用户 API 运行态路由别名。
	UserRuntimeSync Alias = "user.runtime.sync"
	// APIRuntimeConfigReloadStatus 表示查询 API 配置热加载状态路由别名。
	APIRuntimeConfigReloadStatus Alias = "api_runtime.config_reload.status"
	// APIRuntimeConfigReloadItems 表示查询 API 运行态配置项路由别名。
	APIRuntimeConfigReloadItems Alias = "api_runtime.config_reload.items"
	// APIRuntimeConfigReloadRun 表示触发 API 配置热加载路由别名。
	APIRuntimeConfigReloadRun Alias = "api_runtime.config_reload.run"
)

const (
	// RoleTreeOptions 表示角色树下拉路由别名。
	RoleTreeOptions Alias = "role.tree.options"
	// PermissionMaxUUID 表示权限 UUID 预览路由别名。
	PermissionMaxUUID Alias = "permission.max_uuid"
)

const (
	// AdminMessageList 表示管理员消息收件箱路由别名。
	AdminMessageList Alias = "message.list"
	// AdminMessageSentList 表示管理员已发送消息路由别名。
	AdminMessageSentList Alias = "message.sent_list"
	// AdminMessageReceivers 表示管理员消息收件人明细路由别名。
	AdminMessageReceivers Alias = "message.receivers"
	// AdminMessageUnreadCount 表示管理员未读消息数量路由别名。
	AdminMessageUnreadCount Alias = "message.unread_count"
	// AdminMessageNotifications 表示管理员通知列表路由别名。
	AdminMessageNotifications Alias = "message.notifications"
	// AdminMessageMarkRead 表示标记管理员消息已读路由别名。
	AdminMessageMarkRead Alias = "message.mark_read"
	// AdminMessageDelete 表示删除管理员消息路由别名。
	AdminMessageDelete Alias = "message.delete"
	// AdminMessageSend 表示发送管理员消息路由别名。
	AdminMessageSend Alias = "message.send"
	// AdminMessageHandle 表示标记管理员消息已处理路由别名。
	AdminMessageHandle Alias = "message.handle"
)

const (
	// SecretKeyGet 表示查询秘钥详情路由别名。
	SecretKeyGet Alias = "secretKey.get"
	// SecretKeyAdd 表示新增秘钥路由别名。
	SecretKeyAdd Alias = "secretKey.add"
	// SecretKeyUpdate 表示编辑秘钥路由别名。
	SecretKeyUpdate Alias = "secretKey.edit"
	// SecretKeyStatus 表示修改秘钥状态路由别名。
	SecretKeyStatus Alias = "secretKey.editStatus"
	// SecretKeyRenew 表示刷新秘钥缓存路由别名。
	SecretKeyRenew Alias = "secretKey.renew"
	// SecretKeyValidate 表示预检秘钥配置路由别名。
	SecretKeyValidate Alias = "secretKey.validate"
	// SecretKeySelfCheck 表示执行秘钥自检路由别名。
	SecretKeySelfCheck Alias = "secretKey.self_check"
)

const (
	// SecurityDebugSign 表示安全调试签名路由别名。
	SecurityDebugSign Alias = "security.debug.sign"
	// SecurityDebugVerify 表示安全调试验签路由别名。
	SecurityDebugVerify Alias = "security.debug.verify"
	// SecurityDebugEncrypt 表示安全调试加密路由别名。
	SecurityDebugEncrypt Alias = "security.debug.encrypt"
	// SecurityDebugDecrypt 表示安全调试解密路由别名。
	SecurityDebugDecrypt Alias = "security.debug.decrypt"
)

const (
	// UserTagWorkflowLeaseRelease 表示释放用户标签工作流互斥锁路由别名。
	UserTagWorkflowLeaseRelease Alias = "user_tag.workflow_lease.release"
)
