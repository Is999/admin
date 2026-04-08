package handler

import (
	"net/http"

	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
)

// UserBuildSecretVerifyAccountHandler 验证账号密码并返回 MFA 绑定信息。
func UserBuildSecretVerifyAccountHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler[types.UserCompatLoginReq](
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserCompatLoginReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.BuildSecretVerifyAccount(req)
		},
	)(sCtx)
}

// UserMineHandler 返回当前登录管理员资料。
func UserMineHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(userMineCompat, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewUserCompatLogic(r, sCtx)
		return logicObj, logicObj.Mine().WithReq(map[string]any{"action": "user_mine"})
	})
}

// UserPermissionsHandler 返回当前登录管理员角色权限。
func UserPermissionsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(userPermissionsCompat, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewUserCompatLogic(r, sCtx)
		return logicObj, logicObj.Permissions().WithReq(map[string]any{"action": "user_permissions"})
	})
}

// UserCheckSecureHandler 校验当前登录管理员密码。
func UserCheckSecureHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler[types.UserCheckSecureReq](
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserCheckSecureReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.CheckSecure(req)
		},
	)(sCtx)
}

// UserCheckMFAHandler 校验当前登录管理员 MFA 动态码。
func UserCheckMFAHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler[types.UserCheckMFAReq](
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserCheckMFAReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.CheckMFA(req)
		},
	)(sCtx)
}

// UserUpdatePasswordHandler 个人中心修改密码。
func UserUpdatePasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserUpdatePasswordReq](userUpdatePassword,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserUpdatePasswordReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdatePassword(req)
		},
	)(sCtx)
}

// UserUpdateMineHandler 个人中心修改资料。
func UserUpdateMineHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserUpdateMineReq](userUpdateMine,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserUpdateMineReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMine(req)
		},
	)(sCtx)
}

// UserUpdateMFAStatusHandler 个人中心修改 MFA 状态。
func UserUpdateMFAStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserUpdateMFAStatusReq](userUpdateMFAStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserUpdateMFAStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMFAStatus(req)
		},
	)(sCtx)
}

// UserUpdateMFASecureKeyHandler 个人中心修改 MFA 秘钥。
func UserUpdateMFASecureKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserUpdateMFASecureKeyReq](userUpdateMFASecret,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserUpdateMFASecureKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMFASecureKey(req)
		},
	)(sCtx)
}

// UserRefreshMFASecretKeyHandler 个人中心重新生成 MFA 秘钥。
func UserRefreshMFASecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserRefreshMFASecretKeyReq](userRefreshMFASecret,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserRefreshMFASecretKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.RefreshMFASecretKey(req)
		},
	)(sCtx)
}

// UserUpdateAvatarHandler 个人中心修改头像。
func UserUpdateAvatarHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UserUpdateAvatarReq](userUpdateAvatar,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UserUpdateAvatarReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdateAvatar(req)
		},
	)(sCtx)
}

// UserBuildMFASecretKeyURLHandler 生成指定管理员 MFA 绑定地址。
func UserBuildMFASecretKeyURLHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](userBuildMFAURL,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.BuildMFASecretKeyURL(req)
		},
	)(sCtx)
}

// UserAdminMFAStatusHandler 修改指定管理员 MFA 状态。
func UserAdminMFAStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMFAStatusReq](userAdminMFAStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMFAStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserCompatLogic(r, svcCtx)
			return logicObj, logicObj.UpdateAccountMFAStatus(req)
		},
	)(sCtx)
}
