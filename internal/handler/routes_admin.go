package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerAdminRoutes 注册管理员管理与管理员审计日志查询接口。
func registerAdminRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// 注册管理员管理相关接口。
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admins", // 查询管理员列表
			Handler: authMw.Handle(ListAdminHandler(serverCtx), AdminList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/exports", // 提交管理员列表异步导出任务
			Handler: authMw.Handle(TriggerAdminExportHandler(serverCtx), AdminExportTrigger.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/exports/status/:jobId", // 查询管理员列表异步导出进度
			Handler: authMw.Handle(GetAdminExportStatusHandler(serverCtx), AdminExportStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/exports/download/:jobId", // 下载管理员列表导出文件
			Handler: authMw.Handle(DownloadAdminExportHandler(serverCtx), AdminExportDownload.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins", // 新增管理员
			Handler: authMw.Handle(AddAdminHandler(serverCtx), AdminAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/status/:id", // 修改管理员状态
			Handler: authMw.Handle(UpdateAdminStatusHandler(serverCtx), AdminStatusUpdate.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/password/reset/:id", // 重置管理员密码
			Handler: authMw.Handle(ResetAdminPasswordHandler(serverCtx), AdminPasswordReset.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/initial-state/reset/:id", // 重置管理员到首次登录前状态
			Handler: authMw.Handle(ResetAdminInitialStateHandler(serverCtx), AdminResetInitialState.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/roles/:id", // 查询管理员角色
			Handler: authMw.Handle(ListAdminRolesHandler(serverCtx), AdminRoleList.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/roles/:id", // 编辑管理员角色
			Handler: authMw.Handle(UpdateAdminRolesHandler(serverCtx), AdminRoleUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/:id", // 查询管理员详情
			Handler: authMw.Handle(GetAdminHandler(serverCtx), AdminInfo.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/:id", // 编辑管理员
			Handler: authMw.Handle(UpdateAdminHandler(serverCtx), AdminUpdate.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/admins/:id", // 删除管理员
			Handler: authMw.Handle(DeleteAdminHandler(serverCtx), AdminDelete.Alias),
		},
	})

	// 管理员账号管理接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/mfa-status/:id", // 编辑账号MFA状态
			Handler: authMw.Handle(UserAdminMFAStatusHandler(serverCtx), UserAdminMFAStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/mfa-secret-key-url/:id", // 生成管理员MFA绑定地址
			Handler: authMw.Handle(UserBuildMFASecretKeyURLHandler(serverCtx), UserBuildMFAURL.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/updatePassword", // 个人中心修改密码
			Handler: authMw.Handle(UserUpdatePasswordHandler(serverCtx), UserUpdatePassword.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/updateMine", // 个人中心修改资料
			Handler: authMw.Handle(UserUpdateMineHandler(serverCtx), UserUpdateMine.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/updateMfaStatus", // 个人中心修改MFA状态
			Handler: authMw.Handle(UserUpdateMFAStatusHandler(serverCtx), UserUpdateMFA.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/updateMfaSecureKey", // 个人中心修改MFA秘钥
			Handler: authMw.Handle(UserUpdateMFASecureKeyHandler(serverCtx), UserUpdateMFAKey.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/refreshMfaSecretKey", // 个人中心重新生成MFA秘钥
			Handler: authMw.Handle(UserRefreshMFASecretKeyHandler(serverCtx), UserRefreshMFAKey.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/updateAvatar", // 个人中心修改头像
			Handler: authMw.Handle(UserUpdateAvatarHandler(serverCtx), UserUpdateAvatar.Alias),
		},
	})

	// role 角色管理相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/roles", // 查询角色列表
			Handler: authMw.Handle(ListRoleHandler(serverCtx), RoleList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/tree", // 查询角色树
			Handler: authMw.Handle(TreeRoleHandler(serverCtx), RoleTreeList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/tree-options", // 查询角色树下拉数据
			Handler: authMw.Handle(TreeRoleOptionsHandler(serverCtx), RoleTreeOptions.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/roles", // 新增角色
			Handler: authMw.Handle(AddRoleHandler(serverCtx), RoleAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/:id", // 编辑角色
			Handler: authMw.Handle(UpdateRoleHandler(serverCtx), RoleUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/status/:id", // 修改角色状态
			Handler: authMw.Handle(UpdateRoleStatusHandler(serverCtx), RoleStatusUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/permissions/:id", // 编辑角色权限
			Handler: authMw.Handle(UpdateRolePermissionHandler(serverCtx), RolePermissionUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/permissions/tree/:id/:isPid", // 查询角色权限树
			Handler: authMw.Handle(GetRolePermissionHandler(serverCtx), RolePermissionTree.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/roles/:id", // 删除角色
			Handler: authMw.Handle(DeleteRoleHandler(serverCtx), RoleDelete.Alias),
		},
	})

	// permission 权限管理相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions", // 查询权限列表
			Handler: authMw.Handle(ListPermissionHandler(serverCtx), PermissionList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions/tree", // 查询权限树
			Handler: authMw.Handle(TreePermissionHandler(serverCtx), PermissionTreeList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions/max-uuid", // 查询下一个权限 UUID
			Handler: authMw.Handle(MaxPermissionUUIDHandler(serverCtx), PermissionMaxUUID.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/permissions", // 新增权限
			Handler: authMw.Handle(AddPermissionHandler(serverCtx), PermissionAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/permissions/:id", // 编辑权限
			Handler: authMw.Handle(UpdatePermissionHandler(serverCtx), PermissionUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/permissions/status/:id", // 修改权限状态
			Handler: authMw.Handle(UpdatePermissionStatusHandler(serverCtx), PermissionStatus.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/permissions/:id", // 删除权限
			Handler: authMw.Handle(DeletePermissionHandler(serverCtx), PermissionDelete.Alias),
		},
	})

	// config 系统常量配置相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts", // 查询系统常量配置列表
			Handler: authMw.Handle(ListSysConfigHandler(serverCtx), SysConfigList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts", // 新增系统常量配置
			Handler: authMw.Handle(AddSysConfigHandler(serverCtx), SysConfigAdd.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts/export", // 导出系统常量配置 Excel
			Handler: authMw.Handle(ExportSysConfigExcelHandler(serverCtx), SysConfigExport.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts/import", // 导入系统常量配置 Excel
			Handler: authMw.Handle(ImportSysConfigExcelHandler(serverCtx), SysConfigImport.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/dicts/:id", // 编辑系统常量配置
			Handler: authMw.Handle(UpdateSysConfigHandler(serverCtx), SysConfigUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts/cache/:uuid", // 查看系统常量配置缓存
			Handler: authMw.Handle(GetSysConfigCacheHandler(serverCtx), SysConfigCache.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts/cache/refresh/:uuid", // 刷新系统常量配置缓存
			Handler: authMw.Handle(RenewSysConfigHandler(serverCtx), SysConfigRenew.Alias),
		},
	})

	// cache 缓存管理相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/caches", // 查询缓存列表
			Handler: authMw.Handle(ListCacheHandler(serverCtx), CacheList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/server-info", // 查询 Redis 服务器信息
			Handler: authMw.Handle(GetCacheServerInfoHandler(serverCtx), CacheServerInfo.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/key-info", // 查询缓存键信息
			Handler: authMw.Handle(GetCacheKeyInfoHandler(serverCtx), CacheKeyInfo.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/keys", // 搜索缓存键
			Handler: authMw.Handle(SearchCacheKeyHandler(serverCtx), CacheSearch.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/key-info/search", // 查询搜索缓存键信息
			Handler: authMw.Handle(GetCacheKeyInfoHandler(serverCtx), CacheKeyInfo.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/refresh", // 刷新指定缓存
			Handler: authMw.Handle(RenewCacheHandler(serverCtx), CacheRenew.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/refresh-all", // 刷新全部缓存
			Handler: authMw.Handle(RenewAllCacheHandler(serverCtx), CacheRenewAll.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/warmup", // 按模板预热缓存
			Handler: authMw.Handle(WarmupCacheHandler(serverCtx), CacheWarmup.Alias),
		},
	})

	// 注册管理员审计日志相关接口。
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-logs", // 查询管理员审计日志
			Handler: authMw.Handle(QueryAdminLogHandler(serverCtx), AdminLogQuery.Alias),
		},
	})

	// secretKey 秘钥管理相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/secret-keys", // 查询秘钥配置列表
			Handler: authMw.Handle(ListSecretKeyHandler(serverCtx), SecretKeyList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/secret-keys/:id", // 查询秘钥配置详情
			Handler: authMw.Handle(GetSecretKeyHandler(serverCtx), SecretKeyGet.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys", // 新增秘钥配置
			Handler: authMw.Handle(AddSecretKeyHandler(serverCtx), SecretKeyAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/secret-keys/:id", // 编辑秘钥配置
			Handler: authMw.Handle(UpdateSecretKeyHandler(serverCtx), SecretKeyUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/secret-keys/status/:id", // 修改秘钥状态
			Handler: authMw.Handle(UpdateSecretKeyStatusHandler(serverCtx), SecretKeyStatus.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/cache/refresh/:uuid", // 刷新秘钥缓存
			Handler: authMw.Handle(RenewSecretKeyHandler(serverCtx), SecretKeyRenew.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/validate", // 预检秘钥路径和材料可用性
			Handler: authMw.Handle(ValidateSecretKeyHandler(serverCtx), SecretKeyValidate.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/self-check/:uuid", // 执行秘钥运行态自检
			Handler: authMw.Handle(SelfCheckSecretKeyHandler(serverCtx), SecretKeySelfCheck.Alias),
		},
	})

	// security 调试相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/sign", // 模拟请求或响应参数签名
			Handler: authMw.Handle(SecurityDebugSignHandler(serverCtx), SecurityDebugSign.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/verify", // 模拟请求或响应参数验签
			Handler: authMw.Handle(SecurityDebugVerifyHandler(serverCtx), SecurityDebugVerify.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/encrypt", // 模拟请求或响应参数加密
			Handler: authMw.Handle(SecurityDebugEncryptHandler(serverCtx), SecurityDebugEncrypt.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/decrypt", // 模拟请求或响应参数解密
			Handler: authMw.Handle(SecurityDebugDecryptHandler(serverCtx), SecurityDebugDecrypt.Alias),
		},
	})
}
