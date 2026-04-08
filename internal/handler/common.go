package handler

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/helper"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// LogicObj 抽象所有 handler 依赖的最小 logic 能力，便于统一响应与审计封装。
type LogicObj interface {
	// Errorf 按项目统一日志风格输出错误日志。
	Errorf(string, ...any)

	// AddAdminLog 记录管理员操作审计日志。
	AddAdminLog(action model.AdminLogAction, route, method, describe string, data any)

	// GetCtxAdmin 返回当前请求上下文中的管理员信息。
	GetCtxAdmin() *helper.CtxAdmin
}

// handlerFunc 约定 handler 内部统一返回 logic 对象和业务响应，便于公共响应与审计逻辑复用。
type handlerFunc func(r *http.Request) (LogicObj, *types.BizResult)

// respHandler 处理普通接口响应，不附带管理员审计日志。
func respHandler(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		writeBizResponse(w, r, logicObj, resp, nil, "")
	}
}

// actionLogHandler 在统一响应之外补充管理员审计日志，避免每个 handler 重复写审计模板代码。
// fnName 是 handler 侧的方法标识，会在 actionLogRegistry 中映射为 action + route。
func actionLogHandler(fnName method, fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		logMeta := actionLogMap(fnName)
		writeBizResponse(w, r, logicObj, resp, logMeta, fnName)
	}
}

// ActionExec 定义带审计日志的标准 handler 执行函数。
// 调用方只需要关心“如何构造 logic 并执行业务”，公共层统一处理参数解析、响应写出和审计补充。
type ActionExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// RespExec 定义不带审计日志的标准 handler 执行函数。
type RespExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// ActionHandler 泛型封装，简化标准 CRUD handler 的模板代码。
func ActionHandler[Req any](
	fnName method,
	exec ActionExec[Req],
) func(*svc.ServiceContext) http.HandlerFunc {
	return func(sCtx *svc.ServiceContext) http.HandlerFunc {
		return actionLogHandler(fnName, func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, paramErrorResult(codes.ParamError, err)
			}
			logicObj, resp := exec(r, sCtx, &req)
			if resp == nil {
				return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
			}
			resp.WithReq(&req)
			return logicObj, resp
		})
	}
}

// RespHandler 泛型封装，简化普通接口（无审计日志）的 handler 模板代码。
func RespHandler[Req any](
	exec RespExec[Req],
) func(*svc.ServiceContext) http.HandlerFunc {
	return func(sCtx *svc.ServiceContext) http.HandlerFunc {
		return respHandler(func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, paramErrorResult(codes.ParamError, err)
			}
			logicObj, resp := exec(r, sCtx, &req)
			if resp == nil {
				return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
			}
			resp.WithReq(&req)
			return logicObj, resp
		})
	}
}

// paramErrorResult 统一封装参数解析失败响应，强制走国际化模板，避免各 handler 重复拼接文案。
func paramErrorResult(code int, err error) *types.BizResult {
	if code <= 0 {
		code = codes.ParamError
	}
	if err == nil {
		return types.NewBizResult(code).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	return types.NewBizResult(code).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
}

// writeBizResponse 把标准业务响应写出，并按需补充审计日志，保证成功/失败处理路径一致。
func writeBizResponse(w http.ResponseWriter, r *http.Request, logicObj LogicObj, resp *types.BizResult, logMeta *actionLogParam, fnName method) {
	// 兜住空响应，避免 handler 意外返回 nil 时 panic。
	if resp == nil {
		resp = types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
	}

	message := resp.ResolveMessage(r.Context())
	if resp.IsFailure() {
		if logicObj != nil && resp.Error != nil && !errors.Is(resp.Error, types.Nil) {
			admin := logicObj.GetCtxAdmin()
			if admin != nil && admin.Name != "" {
				logicObj.Errorf("%s %s", admin.Name, loggerx.ErrorChain(resp.Error))
			} else {
				logicObj.Errorf("%s", loggerx.ErrorChain(resp.Error))
			}
		}
		jsonResp := helper.NewJsonResp(r.Context(), w).SetCode(resp.Code)
		if resp.Error != nil && !errors.Is(resp.Error, types.Nil) {
			jsonResp = jsonResp.SetError(resp.Error)
		}
		jsonResp.Fail(message)
	} else {
		helper.NewJsonResp(r.Context(), w).SetCode(resp.Code).SetMessage(message).Success(resp.Data)
	}

	// 统一在响应写出后补充审计日志，成功和失败都保留，便于链路回溯。
	if logicObj != nil && logMeta != nil {
		logicObj.AddAdminLog(logMeta.action, logMeta.route, string(fnName), logMeta.describe, resp.Req)
	}
}

// actionLogParam 表示记录操作日志所需的参数。
type actionLogParam struct {
	action   model.AdminLogAction // 操作行为枚举
	route    string               // 路由别名
	describe string               // 中文业务说明
}

// method 定义为字符串类型，用作内部方法标识。
type method string

// 统一维护各 Handler 的方法名常量，确保审计映射和调用点使用同一标识。
const (
	// 管理员模块
	userBuildSecretCompat       method = "userBuildSecretCompat"       // 验证账号并返回MFA绑定信息
	userMineCompat              method = "userMineCompat"              // 获取当前管理员资料
	userPermissionsCompat       method = "userPermissionsCompat"       // 获取当前管理员角色权限
	userCheckSecureCompat       method = "userCheckSecureCompat"       // 校验当前管理员密码
	userCheckMFACompat          method = "userCheckMFACompat"          // 校验当前管理员MFA动态码
	userUpdatePassword          method = "userUpdatePassword"          // 个人中心修改密码
	userUpdateMine              method = "userUpdateMine"              // 个人中心修改资料
	userUpdateMFAStatus         method = "userUpdateMFAStatus"         // 个人中心修改MFA状态
	userUpdateMFASecret         method = "userUpdateMFASecret"         // 个人中心修改MFA秘钥
	userRefreshMFASecret        method = "userRefreshMFASecret"        // 个人中心重新生成MFA秘钥
	userUpdateAvatar            method = "userUpdateAvatar"            // 个人中心修改头像
	userBuildMFAURL             method = "userBuildMFAURL"             // 生成管理员MFA绑定地址
	userAdminMFAStatus          method = "userAdminMFAStatus"          // 修改管理员MFA状态
	addAdmin                    method = "addAdmin"                    // 新增管理员
	listAdmin                   method = "listAdmin"                   // 查询管理员列表
	getAdmin                    method = "getAdmin"                    // 查询管理员详情
	updateAdmin                 method = "updateAdmin"                 // 编辑管理员
	deleteAdmin                 method = "deleteAdmin"                 // 删除管理员
	updateAdminStatus           method = "updateAdminStatus"           // 修改管理员状态
	resetAdminPassword          method = "resetAdminPassword"          // 重置管理员密码
	resetAdminInitialState      method = "resetAdminInitialState"      // 重置管理员到首次登录前状态
	initAdminBootstrap          method = "initAdminBootstrap"          // 内网初始化管理员账号
	listAdminRoles              method = "listAdminRoles"              // 查询管理员角色
	updateAdminRoles            method = "updateAdminRoles"            // 编辑管理员角色
	addAdminRole                method = "addAdminRole"                // 添加管理员角色
	deleteAdminRole             method = "deleteAdminRole"             // 解除管理员角色
	triggerAdminExport          method = "triggerAdminExport"          // 提交管理员异步导出任务
	getAdminExportStatus        method = "getAdminExportStatus"        // 查询管理员异步导出进度
	downloadAdminExport         method = "downloadAdminExport"         // 下载管理员异步导出文件
	listRole                    method = "listRole"                    // 查询角色列表
	treeRole                    method = "treeRole"                    // 查询角色树
	addRole                     method = "addRole"                     // 新增角色
	updateRole                  method = "updateRole"                  // 编辑角色
	deleteRole                  method = "deleteRole"                  // 删除角色
	updateRoleStatus            method = "updateRoleStatus"            // 修改角色状态
	getRolePermission           method = "getRolePermission"           // 查询角色权限树
	updateRolePermission        method = "updateRolePermission"        // 编辑角色权限
	listPermission              method = "listPermission"              // 查询权限列表
	treePermission              method = "treePermission"              // 查询权限树
	maxPermissionUUID           method = "maxPermissionUUID"           // 查询下一个权限 UUID
	addPermission               method = "addPermission"               // 新增权限
	updatePermission            method = "updatePermission"            // 编辑权限
	deletePermission            method = "deletePermission"            // 删除权限
	updatePermissionStatus      method = "updatePermissionStatus"      // 修改权限状态
	listSysConfig               method = "listSysConfig"               // 查询系统配置
	addSysConfig                method = "addSysConfig"                // 新增系统配置
	updateSysConfig             method = "updateSysConfig"             // 编辑系统配置
	exportSysConfigExcel        method = "exportSysConfigExcel"        // 导出系统配置 Excel
	importSysConfigExcel        method = "importSysConfigExcel"        // 导入系统配置 Excel
	getSysConfigCache           method = "getSysConfigCache"           // 查看系统配置缓存
	renewSysConfig              method = "renewSysConfig"              // 刷新系统配置缓存
	listCache                   method = "listCache"                   // 查询缓存列表
	getCacheServerInfo          method = "getCacheServerInfo"          // 查看缓存服务器信息
	getCacheKeyInfo             method = "getCacheKeyInfo"             // 查看缓存键信息
	searchCacheKey              method = "searchCacheKey"              // 搜索缓存键
	renewCache                  method = "renewCache"                  // 刷新缓存
	renewAllCache               method = "renewAllCache"               // 刷新全部缓存
	warmupCache                 method = "warmupCache"                 // 按模板预热缓存
	getCollectorOverview        method = "getCollectorOverview"        // 查询Collector概览
	listCollectorTasks          method = "listCollectorTasks"          // 查询Collector任务列表
	runCollector                method = "runCollector"                // 手动执行Collector任务
	retryCollectorTasks         method = "retryCollectorTasks"         // 手动重试Collector任务
	listSecretKey               method = "listSecretKey"               // 查询秘钥列表
	getSecretKey                method = "getSecretKey"                // 查询秘钥详情
	addSecretKey                method = "addSecretKey"                // 新增秘钥
	updateSecretKey             method = "updateSecretKey"             // 编辑秘钥
	updateSecretKeyStatus       method = "updateSecretKeyStatus"       // 修改秘钥状态
	renewSecretKey              method = "renewSecretKey"              // 刷新秘钥缓存
	validateSecretKey           method = "validateSecretKey"           // 预检秘钥配置
	selfCheckSecretKey          method = "selfCheckSecretKey"          // 执行秘钥自检
	securityDebugSign           method = "securityDebugSign"           // 安全调试签名
	securityDebugVerify         method = "securityDebugVerify"         // 安全调试验签
	securityDebugEncrypt        method = "securityDebugEncrypt"        // 安全调试加密
	securityDebugDecrypt        method = "securityDebugDecrypt"        // 安全调试解密
	listAdminMessage            method = "listAdminMessage"            // 查询管理员消息收件箱
	listAdminMessageSent        method = "listAdminMessageSent"        // 查询管理员已发送消息
	listAdminMessageReceivers   method = "listAdminMessageReceivers"   // 查询管理员消息收件人明细
	markAdminMessageRead        method = "markAdminMessageRead"        // 标记管理员消息已读
	deleteAdminMessage          method = "deleteAdminMessage"          // 删除管理员消息
	sendAdminMessage            method = "sendAdminMessage"            // 发送管理员消息
	handleAdminMessage          method = "handleAdminMessage"          // 标记管理员消息已处理
	queryAdminLog               method = "queryAdminLog"               // 查询管理员操作日志
	enqueueTask                 method = "enqueueTask"                 // 手动投递通用任务
	getTaskInfo                 method = "getTaskInfo"                 // 查询任务详情
	listTaskItems               method = "listTaskItems"               // 查询任务列表
	triggerTaskWorkflow         method = "triggerTaskWorkflow"         // 手动触发工作流
	getTaskWorkflowStatus       method = "getTaskWorkflowStatus"       // 查询工作流状态
	listTaskQueues              method = "listTaskQueues"              // 查询任务队列概览
	getConfigReloadItems        method = "getConfigReloadItems"        // 查询配置热加载配置项
	getConfigReloadStatus       method = "getConfigReloadStatus"       // 查询配置热加载状态
	runConfigReload             method = "runConfigReload"             // 手动触发配置热加载
	pauseTaskQueue              method = "pauseTaskQueue"              // 暂停任务队列
	runTask                     method = "runTask"                     // 立即执行任务
	deleteTask                  method = "deleteTask"                  // 删除任务
	resumeTaskQueue             method = "resumeTaskQueue"             // 恢复任务队列
	triggerUserTagWorkflow      method = "triggerUserTagWorkflow"      // 触发用户标签工作流
	recalculateUserTag          method = "recalculateUserTag"          // 指定标签重新计算
	releaseUserTagWorkflowLease method = "releaseUserTagWorkflowLease" // 释放用户标签工作流互斥锁
)

// actionLogRegistry 维护底层 Handler 业务处理方法（method）与顶层路由元数据（RouteMeta）的关联关系。
// 在处理每个请求时，审计中间件或公共处理函数会通过这个映射表，
// 将当前执行的业务方法自动转化为标准的审计动作（Action）、路由别名（Route）以及中文描述。
// 所有的审计动作、路由别名、中文说明都统一从 route_meta.go 中取值，保持核心常量定义的唯一性。
var actionLogRegistry = map[method]RouteMeta{
	// 管理员模块
	userBuildSecretCompat:       UserBuildSecret,             // 验证账号并返回MFA绑定信息
	userMineCompat:              UserMine,                    // 获取当前管理员资料
	userPermissionsCompat:       UserPermissions,             // 获取当前管理员角色权限
	userCheckSecureCompat:       UserCheckSecure,             // 校验当前管理员密码
	userCheckMFACompat:          UserCheckMFA,                // 校验当前管理员MFA动态码
	userUpdatePassword:          UserUpdatePassword,          // 个人中心修改密码
	userUpdateMine:              UserUpdateMine,              // 个人中心修改资料
	userUpdateMFAStatus:         UserUpdateMFA,               // 个人中心修改MFA状态
	userUpdateMFASecret:         UserUpdateMFAKey,            // 个人中心修改MFA秘钥
	userRefreshMFASecret:        UserRefreshMFAKey,           // 个人中心重新生成MFA秘钥
	userUpdateAvatar:            UserUpdateAvatar,            // 个人中心修改头像
	userBuildMFAURL:             UserBuildMFAURL,             // 生成管理员MFA绑定地址
	userAdminMFAStatus:          UserAdminMFAStatus,          // 修改管理员MFA状态
	queryAdminLog:               AdminLogQuery,               // 审计日志查询
	addAdmin:                    AdminAdd,                    // 添加管理员
	listAdmin:                   AdminList,                   // 查询管理员列表
	getAdmin:                    AdminInfo,                   // 查询管理员详情
	updateAdmin:                 AdminUpdate,                 // 编辑管理员
	deleteAdmin:                 AdminDelete,                 // 删除管理员
	updateAdminStatus:           AdminStatusUpdate,           // 修改管理员状态
	resetAdminPassword:          AdminPasswordReset,          // 重置管理员密码
	resetAdminInitialState:      AdminResetInitialState,      // 重置管理员到首次登录前状态
	listAdminRoles:              AdminRoleList,               // 查询管理员角色
	updateAdminRoles:            AdminRoleUpdate,             // 编辑管理员角色
	addAdminRole:                AdminRoleAdd,                // 添加管理员角色
	deleteAdminRole:             AdminRoleDelete,             // 解除管理员角色
	triggerAdminExport:          AdminExportTrigger,          // 提交管理员异步导出任务
	getAdminExportStatus:        AdminExportStatus,           // 查询管理员异步导出进度
	downloadAdminExport:         AdminExportDownload,         // 下载管理员异步导出文件
	listRole:                    RoleList,                    // 查询角色列表
	treeRole:                    RoleTreeList,                // 查询角色树
	addRole:                     RoleAdd,                     // 新增角色
	updateRole:                  RoleUpdate,                  // 编辑角色
	deleteRole:                  RoleDelete,                  // 删除角色
	updateRoleStatus:            RoleStatusUpdate,            // 修改角色状态
	getRolePermission:           RolePermissionTree,          // 查询角色权限树
	updateRolePermission:        RolePermissionUpdate,        // 编辑角色权限
	listPermission:              PermissionList,              // 查询权限列表
	treePermission:              PermissionTreeList,          // 查询权限树
	maxPermissionUUID:           PermissionMaxUUID,           // 查询下一个权限 UUID
	addPermission:               PermissionAdd,               // 新增权限
	updatePermission:            PermissionUpdate,            // 编辑权限
	deletePermission:            PermissionDelete,            // 删除权限
	updatePermissionStatus:      PermissionStatus,            // 修改权限状态
	listSysConfig:               SysConfigList,               // 查询系统配置
	addSysConfig:                SysConfigAdd,                // 新增系统配置
	updateSysConfig:             SysConfigUpdate,             // 编辑系统配置
	exportSysConfigExcel:        SysConfigExport,             // 导出系统配置 Excel
	importSysConfigExcel:        SysConfigImport,             // 导入系统配置 Excel
	getSysConfigCache:           SysConfigCache,              // 查看系统配置缓存
	renewSysConfig:              SysConfigRenew,              // 刷新系统配置缓存
	listCache:                   CacheList,                   // 查询缓存列表
	getCacheServerInfo:          CacheServerInfo,             // 查看缓存服务器信息
	getCacheKeyInfo:             CacheKeyInfo,                // 查看缓存键信息
	searchCacheKey:              CacheSearch,                 // 搜索缓存键
	renewCache:                  CacheRenew,                  // 刷新缓存
	renewAllCache:               CacheRenewAll,               // 刷新全部缓存
	warmupCache:                 CacheWarmup,                 // 按模板预热缓存
	getCollectorOverview:        CollectorOverview,           // 查询Collector概览
	listCollectorTasks:          CollectorTaskList,           // 查询Collector任务列表
	runCollector:                CollectorRun,                // 手动执行Collector任务
	retryCollectorTasks:         CollectorRetry,              // 手动重试Collector任务
	listSecretKey:               SecretKeyList,               // 查询秘钥列表
	getSecretKey:                SecretKeyGet,                // 查询秘钥详情
	addSecretKey:                SecretKeyAdd,                // 新增秘钥
	updateSecretKey:             SecretKeyUpdate,             // 编辑秘钥
	updateSecretKeyStatus:       SecretKeyStatus,             // 修改秘钥状态
	renewSecretKey:              SecretKeyRenew,              // 刷新秘钥缓存
	validateSecretKey:           SecretKeyValidate,           // 预检秘钥配置
	selfCheckSecretKey:          SecretKeySelfCheck,          // 执行秘钥自检
	securityDebugSign:           SecurityDebugSign,           // 安全调试签名
	securityDebugVerify:         SecurityDebugVerify,         // 安全调试验签
	securityDebugEncrypt:        SecurityDebugEncrypt,        // 安全调试加密
	securityDebugDecrypt:        SecurityDebugDecrypt,        // 安全调试解密
	listAdminMessage:            AdminMessageList,            // 查询管理员消息收件箱
	listAdminMessageSent:        AdminMessageSentList,        // 查询管理员已发送消息
	listAdminMessageReceivers:   AdminMessageReceivers,       // 查询管理员消息收件人明细
	markAdminMessageRead:        AdminMessageMarkRead,        // 标记管理员消息已读
	deleteAdminMessage:          AdminMessageDelete,          // 删除管理员消息
	sendAdminMessage:            AdminMessageSend,            // 发送管理员消息
	handleAdminMessage:          AdminMessageHandle,          // 标记管理员消息已处理
	enqueueTask:                 TaskEnqueue,                 // 手动投递通用任务
	getTaskInfo:                 TaskInfoGet,                 // 查询任务详情
	listTaskItems:               TaskItemsList,               // 查询任务列表
	triggerTaskWorkflow:         TaskWorkflowTrigger,         // 触发任务工作流
	getTaskWorkflowStatus:       TaskWorkflowStatus,          // 获取工作流状态
	listTaskQueues:              TaskQueueList,               // 查询任务队列列表
	getConfigReloadItems:        TaskConfigItems,             // 查询配置热加载配置项
	getConfigReloadStatus:       TaskConfigReload,            // 查询配置热加载状态
	runConfigReload:             TaskConfigReloadRun,         // 手动触发配置热加载
	pauseTaskQueue:              TaskQueuePause,              // 暂停任务队列
	runTask:                     TaskRun,                     // 立即执行任务
	deleteTask:                  TaskDelete,                  // 删除任务
	resumeTaskQueue:             TaskQueueResume,             // 恢复任务队列
	triggerUserTagWorkflow:      UserTagWorkflowTrigger,      // 触发用户标签工作流
	recalculateUserTag:          UserTagRecalculate,          // 指定标签重新计算
	releaseUserTagWorkflowLease: UserTagWorkflowLeaseRelease, // 释放用户标签工作流互斥锁
}

// actionLogMap 从中心化注册表里取路由审计元数据，避免 handler 和路由定义出现双份映射。
func actionLogMap(name method) *actionLogParam {
	meta, ok := actionLogRegistry[name]
	if !ok || meta.Action == "" {
		return nil
	}
	return &actionLogParam{
		action:   meta.Action,
		route:    string(meta.Alias),
		describe: meta.Describe,
	}
}
