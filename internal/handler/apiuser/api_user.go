package apiuser

import (
	"net/http"

	"admin/internal/handler/shared"
	apiruntimelogic "admin/internal/logic/apiruntime"
	apiuserlogic "admin/internal/logic/apiuser"
	"admin/internal/svc"
	"admin/internal/types"
)

// ListHandler 查询前台用户列表。
func ListHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.APIUserListReq](shared.MethodAPIUserList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.APIUserListReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.List(req)
	})(sCtx)
}

// GetHandler 查询前台用户详情。
func GetHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.APIUserIDReq](shared.MethodAPIUserInfo, func(r *http.Request, sCtx *svc.ServiceContext, req *types.APIUserIDReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Get(req)
	})(sCtx)
}

// CreateHandler 新增前台用户。
func CreateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CreateAPIUserReq](shared.MethodAPIUserAdd, func(r *http.Request, sCtx *svc.ServiceContext, req *types.CreateAPIUserReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Create(req)
	})(sCtx)
}

// UpdateHandler 编辑前台用户资料。
func UpdateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UpdateAPIUserReq](shared.MethodAPIUserUpdate, func(r *http.Request, sCtx *svc.ServiceContext, req *types.UpdateAPIUserReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Update(req)
	})(sCtx)
}

// UpdateStatusHandler 修改前台用户状态。
func UpdateStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.APIUserStatusReq](shared.MethodAPIUserStatusUpdate, func(r *http.Request, sCtx *svc.ServiceContext, req *types.APIUserStatusReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.UpdateStatus(req)
	})(sCtx)
}

// ResetPasswordHandler 重置前台用户密码。
func ResetPasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ResetAPIUserPasswordReq](shared.MethodAPIUserPasswordReset, func(r *http.Request, sCtx *svc.ServiceContext, req *types.ResetAPIUserPasswordReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.ResetPassword(req)
	})(sCtx)
}

// SyncRuntimeHandler 手动同步前台用户 API 运行态。
func SyncRuntimeHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SyncAPIUserRuntimeReq](shared.MethodAPIUserRuntimeSync, func(r *http.Request, sCtx *svc.ServiceContext, req *types.SyncAPIUserRuntimeReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiuserlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.SyncRuntime(req)
	})(sCtx)
}

// APIRuntimeReloadStatusHandler 查询 API 配置热加载状态。
func APIRuntimeReloadStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodAPIRuntimeConfigReloadStatus, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := apiruntimelogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Status().WithReq(map[string]any{"action": "api_runtime_config_reload_status"})
	})
}

// APIRuntimeReloadRunHandler 手动触发 API 配置热加载。
func APIRuntimeReloadRunHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.APIRuntimeConfigReloadReq](shared.MethodAPIRuntimeConfigReloadRun, func(r *http.Request, sCtx *svc.ServiceContext, req *types.APIRuntimeConfigReloadReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiruntimelogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Reload(req)
	})(sCtx)
}
