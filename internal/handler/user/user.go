package user

import (
	"net/http"

	"admin/internal/handler/shared"
	apiruntimelogic "admin/internal/logic/apiruntime"
	userlogic "admin/internal/logic/user"
	"admin/internal/svc"
	"admin/internal/types"
)

// ListHandler 查询前台用户列表。
func ListHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UserListReq](shared.UserList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.UserListReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.List(req)
	})(sCtx)
}

// GetHandler 查询前台用户详情。
func GetHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UserIDReq](shared.UserInfo, func(r *http.Request, sCtx *svc.ServiceContext, req *types.UserIDReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Get(req)
	})(sCtx)
}

// CreateHandler 新增前台用户。
func CreateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CreateUserReq](shared.UserAdd, func(r *http.Request, sCtx *svc.ServiceContext, req *types.CreateUserReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Create(req)
	})(sCtx)
}

// UpdateHandler 编辑前台用户资料。
func UpdateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UpdateUserReq](shared.UserUpdate, func(r *http.Request, sCtx *svc.ServiceContext, req *types.UpdateUserReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Update(req)
	})(sCtx)
}

// UpdateStatusHandler 修改前台用户状态。
func UpdateStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UserStatusReq](shared.UserStatusUpdate, func(r *http.Request, sCtx *svc.ServiceContext, req *types.UserStatusReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.UpdateStatus(req)
	})(sCtx)
}

// ResetPasswordHandler 重置前台用户密码。
func ResetPasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ResetUserPasswordReq](shared.UserPasswordReset, func(r *http.Request, sCtx *svc.ServiceContext, req *types.ResetUserPasswordReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.ResetPassword(req)
	})(sCtx)
}

// SyncRuntimeHandler 手动同步前台用户 API 运行态。
func SyncRuntimeHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SyncUserRuntimeReq](shared.UserRuntimeSync, func(r *http.Request, sCtx *svc.ServiceContext, req *types.SyncUserRuntimeReq) (shared.LogicObj, *types.BizResult) {
		logicObj := userlogic.NewLogic(r, sCtx)
		return logicObj, logicObj.SyncRuntime(req)
	})(sCtx)
}

// APIRuntimeReloadStatusHandler 查询 API 配置热加载状态。
func APIRuntimeReloadStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.APIRuntimeConfigReloadStatus, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := apiruntimelogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Status().WithReq(shared.ActionReq("api_runtime_config_reload_status"))
	})
}

// APIRuntimeReloadItemsHandler 查询 API 当前运行态配置项。
func APIRuntimeReloadItemsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.TaskConfigItemQueryReq](shared.APIRuntimeConfigReloadItems, func(r *http.Request, sCtx *svc.ServiceContext, req *types.TaskConfigItemQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiruntimelogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Items(req)
	})(sCtx)
}

// APIRuntimeReloadRunHandler 手动触发 API 配置热加载。
func APIRuntimeReloadRunHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.APIRuntimeConfigReloadReq](shared.APIRuntimeConfigReloadRun, func(r *http.Request, sCtx *svc.ServiceContext, req *types.APIRuntimeConfigReloadReq) (shared.LogicObj, *types.BizResult) {
		logicObj := apiruntimelogic.NewLogic(r, sCtx)
		return logicObj, logicObj.Reload(req)
	})(sCtx)
}
