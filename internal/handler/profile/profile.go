package profile

import (
	"admin/internal/handler/shared"
	"net/http"

	profilelogic "admin/internal/logic/profile"
	"admin/internal/svc"
	"admin/internal/types"
)

// ProfileMineHandler 返回当前登录管理员资料。
func ProfileMineHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.ProfileMine, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := profilelogic.NewProfileLogic(r, sCtx)
		return logicObj, logicObj.Mine().WithReq(shared.ActionReq("profile_mine"))
	})
}

// ProfileCheckSecureHandler 校验当前登录管理员密码。
func ProfileCheckSecureHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler[types.ProfileCheckSecureReq](
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileCheckSecureReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.CheckSecure(req)
		},
	)(sCtx)
}

// ProfileCheckMFAHandler 校验当前登录管理员 MFA 动态码。
func ProfileCheckMFAHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler[types.ProfileCheckMFAReq](
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileCheckMFAReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.CheckMFA(req)
		},
	)(sCtx)
}

// ProfileUpdatePasswordHandler 个人中心修改密码。
func ProfileUpdatePasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfilePasswordReq](shared.ProfileUpdatePassword,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfilePasswordReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdatePassword(req)
		},
	)(sCtx)
}

// ProfileUpdateMineHandler 个人中心修改资料。
func ProfileUpdateMineHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfileUpdateReq](shared.ProfileUpdateMine,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileUpdateReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMine(req)
		},
	)(sCtx)
}

// ProfileUpdateMFAStatusHandler 个人中心修改 MFA 状态。
func ProfileUpdateMFAStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfileMFAStatusReq](shared.ProfileUpdateMFA,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileMFAStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMFAStatus(req)
		},
	)(sCtx)
}

// ProfileUpdateMFASecureKeyHandler 个人中心修改 MFA 秘钥。
func ProfileUpdateMFASecureKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfileMFASecretReq](shared.ProfileUpdateMFAKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileMFASecretReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdateMFASecureKey(req)
		},
	)(sCtx)
}

// ProfileRefreshMFASecretKeyHandler 个人中心重新生成 MFA 秘钥。
func ProfileRefreshMFASecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfileMFASecretRefreshReq](shared.ProfileRefreshMFAKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileMFASecretRefreshReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.RefreshMFASecretKey(req)
		},
	)(sCtx)
}

// ProfileUpdateAvatarHandler 个人中心修改头像。
func ProfileUpdateAvatarHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ProfileAvatarReq](shared.ProfileUpdateAvatar,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ProfileAvatarReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdateAvatar(req)
		},
	)(sCtx)
}

// AdminBuildMFASecretKeyURLHandler 生成指定管理员 MFA 绑定地址。
func AdminBuildMFASecretKeyURLHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.AdminBuildMFAURL,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.BuildMFASecretKeyURL(req)
		},
	)(sCtx)
}

// AdminMFAStatusHandler 修改指定管理员 MFA 状态。
func AdminMFAStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMFAStatusReq](shared.AdminMFAStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMFAStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := profilelogic.NewProfileLogic(r, svcCtx)
			return logicObj, logicObj.UpdateAccountMFAStatus(req)
		},
	)(sCtx)
}
