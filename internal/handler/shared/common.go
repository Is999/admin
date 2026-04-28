package shared

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	"admin/helper"
	"admin/internal/infra/loggerx"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

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

// HandlerFunc 约定 handler 内部统一返回 logic 对象和业务响应，便于公共响应与审计逻辑复用。
type HandlerFunc func(r *http.Request) (LogicObj, *types.BizResult)

// RespHandlerFunc 处理普通接口响应，不附带管理员审计日志。
func RespHandlerFunc(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		WriteBizResponse(w, r, logicObj, resp, nil, "")
	}
}

// ActionLogHandler 在统一响应之外补充管理员审计日志，避免每个 handler 重复写审计模板代码。
// fnName 是 handler 侧的方法标识，会在 actionLogRegistry 中映射为 action + route。
func ActionLogHandler(fnName Method, fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		logMeta := ActionLogMap(fnName)
		WriteBizResponse(w, r, logicObj, resp, logMeta, fnName)
	}
}

// ActionExec 定义带审计日志的标准 handler 执行函数。
// 调用方只需要关心“如何构造 logic 并执行业务”，公共层统一处理参数解析、响应写出和审计补充。
type ActionExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// RespExec 定义不带审计日志的标准 handler 执行函数。
type RespExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// ActionHandler 泛型封装，简化标准 CRUD handler 的模板代码。
func ActionHandler[Req any](
	fnName Method,
	exec ActionExec[Req],
) func(*svc.ServiceContext) http.HandlerFunc {
	return func(sCtx *svc.ServiceContext) http.HandlerFunc {
		return ActionLogHandler(fnName, func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, ParamErrorResult(codes.ParamError, err)
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
		return RespHandlerFunc(func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, ParamErrorResult(codes.ParamError, err)
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

// ParamErrorResult 统一封装参数解析失败响应，强制走国际化模板，避免各 handler 重复拼接文案。
func ParamErrorResult(code int, err error) *types.BizResult {
	return types.ParamErrorResultWithCode(code, err)
}

// WriteBizResponse 把标准业务响应写出，并按需补充审计日志，保证成功/失败处理路径一致。
func WriteBizResponse(w http.ResponseWriter, r *http.Request, logicObj LogicObj, resp *types.BizResult, logMeta *ActionLogParam, fnName Method) {
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
		logicObj.AddAdminLog(logMeta.Action, logMeta.Route, string(fnName), logMeta.Describe, resp.Req)
	}
}

// ActionLogParam 表示记录操作日志所需的参数。
type ActionLogParam struct {
	Action   model.AdminLogAction // 操作行为枚举
	Route    string               // 路由别名
	Describe string               // 中文业务说明
}

// Method 定义为字符串类型，用作内部方法标识。
type Method string

type method = Method

// 统一维护各 Handler 的方法名常量，确保审计映射和调用点使用同一标识。
const (
	// 管理员模块
	authVerifyAccount           method = "authVerifyAccount"           // 验证账号并返回MFA绑定信息
	profileMine                 method = "profileMine"                 // 获取当前管理员资料
	profilePermissions          method = "profilePermissions"          // 获取当前管理员角色权限
	profileCheckSecure          method = "profileCheckSecure"          // 校验当前管理员密码
	profileCheckMFA             method = "profileCheckMFA"             // 校验当前管理员MFA动态码
	profileUpdatePassword       method = "profileUpdatePassword"       // 个人中心修改密码
	profileUpdateMine           method = "profileUpdateMine"           // 个人中心修改资料
	profileUpdateMFAStatus      method = "profileUpdateMFAStatus"      // 个人中心修改MFA状态
	profileUpdateMFASecret      method = "profileUpdateMFASecret"      // 个人中心修改MFA秘钥
	profileRefreshMFASecret     method = "profileRefreshMFASecret"     // 个人中心重新生成MFA秘钥
	profileUpdateAvatar         method = "profileUpdateAvatar"         // 个人中心修改头像
	adminBuildMFAURL            method = "adminBuildMFAURL"            // 生成管理员MFA绑定地址
	adminMFAStatus              method = "adminMFAStatus"              // 修改管理员MFA状态
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

const (
	MethodAuthVerifyAccount           Method = authVerifyAccount           // 验证账号并返回MFA绑定信息
	MethodProfileMine                 Method = profileMine                 // 获取当前管理员资料
	MethodProfilePermissions          Method = profilePermissions          // 获取当前管理员角色权限
	MethodProfileCheckSecure          Method = profileCheckSecure          // 校验当前管理员密码
	MethodProfileCheckMFA             Method = profileCheckMFA             // 校验当前管理员MFA动态码
	MethodProfileUpdatePassword       Method = profileUpdatePassword       // 个人中心修改密码
	MethodProfileUpdateMine           Method = profileUpdateMine           // 个人中心修改资料
	MethodProfileUpdateMFAStatus      Method = profileUpdateMFAStatus      // 个人中心修改MFA状态
	MethodProfileUpdateMFASecret      Method = profileUpdateMFASecret      // 个人中心修改MFA秘钥
	MethodProfileRefreshMFASecret     Method = profileRefreshMFASecret     // 个人中心重新生成MFA秘钥
	MethodProfileUpdateAvatar         Method = profileUpdateAvatar         // 个人中心修改头像
	MethodAdminBuildMFAURL            Method = adminBuildMFAURL            // 生成管理员MFA绑定地址
	MethodAdminMFAStatus              Method = adminMFAStatus              // 修改管理员MFA状态
	MethodAddAdmin                    Method = addAdmin                    // 新增管理员
	MethodListAdmin                   Method = listAdmin                   // 查询管理员列表
	MethodGetAdmin                    Method = getAdmin                    // 查询管理员详情
	MethodUpdateAdmin                 Method = updateAdmin                 // 编辑管理员
	MethodDeleteAdmin                 Method = deleteAdmin                 // 删除管理员
	MethodUpdateAdminStatus           Method = updateAdminStatus           // 修改管理员状态
	MethodResetAdminPassword          Method = resetAdminPassword          // 重置管理员密码
	MethodResetAdminInitialState      Method = resetAdminInitialState      // 重置管理员到首次登录前状态
	MethodInitAdminBootstrap          Method = initAdminBootstrap          // 内网初始化管理员账号
	MethodListAdminRoles              Method = listAdminRoles              // 查询管理员角色
	MethodUpdateAdminRoles            Method = updateAdminRoles            // 编辑管理员角色
	MethodAddAdminRole                Method = addAdminRole                // 添加管理员角色
	MethodDeleteAdminRole             Method = deleteAdminRole             // 解除管理员角色
	MethodTriggerAdminExport          Method = triggerAdminExport          // 提交管理员异步导出任务
	MethodGetAdminExportStatus        Method = getAdminExportStatus        // 查询管理员异步导出进度
	MethodDownloadAdminExport         Method = downloadAdminExport         // 下载管理员异步导出文件
	MethodListRole                    Method = listRole                    // 查询角色列表
	MethodTreeRole                    Method = treeRole                    // 查询角色树
	MethodAddRole                     Method = addRole                     // 新增角色
	MethodUpdateRole                  Method = updateRole                  // 编辑角色
	MethodDeleteRole                  Method = deleteRole                  // 删除角色
	MethodUpdateRoleStatus            Method = updateRoleStatus            // 修改角色状态
	MethodGetRolePermission           Method = getRolePermission           // 查询角色权限树
	MethodUpdateRolePermission        Method = updateRolePermission        // 编辑角色权限
	MethodListPermission              Method = listPermission              // 查询权限列表
	MethodTreePermission              Method = treePermission              // 查询权限树
	MethodMaxPermissionUUID           Method = maxPermissionUUID           // 查询下一个权限 UUID
	MethodAddPermission               Method = addPermission               // 新增权限
	MethodUpdatePermission            Method = updatePermission            // 编辑权限
	MethodDeletePermission            Method = deletePermission            // 删除权限
	MethodUpdatePermissionStatus      Method = updatePermissionStatus      // 修改权限状态
	MethodListSysConfig               Method = listSysConfig               // 查询系统配置
	MethodAddSysConfig                Method = addSysConfig                // 新增系统配置
	MethodUpdateSysConfig             Method = updateSysConfig             // 编辑系统配置
	MethodExportSysConfigExcel        Method = exportSysConfigExcel        // 导出系统配置 Excel
	MethodImportSysConfigExcel        Method = importSysConfigExcel        // 导入系统配置 Excel
	MethodGetSysConfigCache           Method = getSysConfigCache           // 查看系统配置缓存
	MethodRenewSysConfig              Method = renewSysConfig              // 刷新系统配置缓存
	MethodListCache                   Method = listCache                   // 查询缓存列表
	MethodGetCacheServerInfo          Method = getCacheServerInfo          // 查看缓存服务器信息
	MethodGetCacheKeyInfo             Method = getCacheKeyInfo             // 查看缓存键信息
	MethodSearchCacheKey              Method = searchCacheKey              // 搜索缓存键
	MethodRenewCache                  Method = renewCache                  // 刷新缓存
	MethodRenewAllCache               Method = renewAllCache               // 刷新全部缓存
	MethodWarmupCache                 Method = warmupCache                 // 按模板预热缓存
	MethodGetCollectorOverview        Method = getCollectorOverview        // 查询Collector概览
	MethodListCollectorTasks          Method = listCollectorTasks          // 查询Collector任务列表
	MethodRunCollector                Method = runCollector                // 手动执行Collector任务
	MethodRetryCollectorTasks         Method = retryCollectorTasks         // 手动重试Collector任务
	MethodListSecretKey               Method = listSecretKey               // 查询秘钥列表
	MethodGetSecretKey                Method = getSecretKey                // 查询秘钥详情
	MethodAddSecretKey                Method = addSecretKey                // 新增秘钥
	MethodUpdateSecretKey             Method = updateSecretKey             // 编辑秘钥
	MethodUpdateSecretKeyStatus       Method = updateSecretKeyStatus       // 修改秘钥状态
	MethodRenewSecretKey              Method = renewSecretKey              // 刷新秘钥缓存
	MethodValidateSecretKey           Method = validateSecretKey           // 预检秘钥配置
	MethodSelfCheckSecretKey          Method = selfCheckSecretKey          // 执行秘钥自检
	MethodSecurityDebugSign           Method = securityDebugSign           // 安全调试签名
	MethodSecurityDebugVerify         Method = securityDebugVerify         // 安全调试验签
	MethodSecurityDebugEncrypt        Method = securityDebugEncrypt        // 安全调试加密
	MethodSecurityDebugDecrypt        Method = securityDebugDecrypt        // 安全调试解密
	MethodListAdminMessage            Method = listAdminMessage            // 查询管理员消息收件箱
	MethodListAdminMessageSent        Method = listAdminMessageSent        // 查询管理员已发送消息
	MethodListAdminMessageReceivers   Method = listAdminMessageReceivers   // 查询管理员消息收件人明细
	MethodMarkAdminMessageRead        Method = markAdminMessageRead        // 标记管理员消息已读
	MethodDeleteAdminMessage          Method = deleteAdminMessage          // 删除管理员消息
	MethodSendAdminMessage            Method = sendAdminMessage            // 发送管理员消息
	MethodHandleAdminMessage          Method = handleAdminMessage          // 标记管理员消息已处理
	MethodQueryAdminLog               Method = queryAdminLog               // 查询管理员操作日志
	MethodEnqueueTask                 Method = enqueueTask                 // 手动投递通用任务
	MethodGetTaskInfo                 Method = getTaskInfo                 // 查询任务详情
	MethodListTaskItems               Method = listTaskItems               // 查询任务列表
	MethodTriggerTaskWorkflow         Method = triggerTaskWorkflow         // 手动触发工作流
	MethodGetTaskWorkflowStatus       Method = getTaskWorkflowStatus       // 查询工作流状态
	MethodListTaskQueues              Method = listTaskQueues              // 查询任务队列概览
	MethodGetConfigReloadItems        Method = getConfigReloadItems        // 查询配置热加载配置项
	MethodGetConfigReloadStatus       Method = getConfigReloadStatus       // 查询配置热加载状态
	MethodRunConfigReload             Method = runConfigReload             // 手动触发配置热加载
	MethodPauseTaskQueue              Method = pauseTaskQueue              // 暂停任务队列
	MethodRunTask                     Method = runTask                     // 立即执行任务
	MethodDeleteTask                  Method = deleteTask                  // 删除任务
	MethodResumeTaskQueue             Method = resumeTaskQueue             // 恢复任务队列
	MethodTriggerUserTagWorkflow      Method = triggerUserTagWorkflow      // 触发用户标签工作流
	MethodRecalculateUserTag          Method = recalculateUserTag          // 指定标签重新计算
	MethodReleaseUserTagWorkflowLease Method = releaseUserTagWorkflowLease // 释放用户标签工作流互斥锁
)

// actionLogRegistry 维护底层 Handler 业务处理方法（method）与顶层路由元数据（RouteMeta）的关联关系。
// 在处理每个请求时，审计中间件或公共处理函数会通过这个映射表，
// 将当前执行的业务方法自动转化为标准的审计动作（Action）、路由别名（Route）以及中文描述。
// 所有的审计动作、路由别名、中文说明都统一从 route_meta.go 中取值，保持核心常量定义的唯一性。
var actionLogRegistry = map[method]RouteMeta{
	// 管理员模块
	authVerifyAccount:           AuthVerifyAccount,           // 验证账号并返回MFA绑定信息
	profileMine:                 ProfileMine,                 // 获取当前管理员资料
	profilePermissions:          ProfilePermissions,          // 获取当前管理员角色权限
	profileCheckSecure:          ProfileCheckSecure,          // 校验当前管理员密码
	profileCheckMFA:             ProfileCheckMFA,             // 校验当前管理员MFA动态码
	profileUpdatePassword:       ProfileUpdatePassword,       // 个人中心修改密码
	profileUpdateMine:           ProfileUpdateMine,           // 个人中心修改资料
	profileUpdateMFAStatus:      ProfileUpdateMFA,            // 个人中心修改MFA状态
	profileUpdateMFASecret:      ProfileUpdateMFAKey,         // 个人中心修改MFA秘钥
	profileRefreshMFASecret:     ProfileRefreshMFAKey,        // 个人中心重新生成MFA秘钥
	profileUpdateAvatar:         ProfileUpdateAvatar,         // 个人中心修改头像
	adminBuildMFAURL:            AdminBuildMFAURL,            // 生成管理员MFA绑定地址
	adminMFAStatus:              AdminMFAStatus,              // 修改管理员MFA状态
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

// ActionLogRegistry 返回审计方法与路由元数据的映射副本。
func ActionLogRegistry() map[Method]RouteMeta {
	out := make(map[Method]RouteMeta, len(actionLogRegistry))
	for name, meta := range actionLogRegistry {
		out[name] = meta
	}
	return out
}

// ActionLogMap 从中心化注册表里取路由审计元数据，避免 handler 和路由定义出现双份映射。
func ActionLogMap(name Method) *ActionLogParam {
	meta, ok := actionLogRegistry[name]
	if !ok || meta.Action == "" {
		return nil
	}
	return &ActionLogParam{
		Action:   meta.Action,
		Route:    string(meta.Alias),
		Describe: meta.Describe,
	}
}
