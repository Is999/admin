package profile

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterManageRoutes 注册个人中心和账号安全管理接口。
func RegisterManageRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/mfa-status/:id", // 编辑账号MFA状态
			Handler: authMw.Handle(AdminMFAStatusHandler(serverCtx), shared.AdminMFAStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/mfa-secret-key-url/:id", // 生成管理员MFA绑定地址
			Handler: authMw.Handle(AdminBuildMFASecretKeyURLHandler(serverCtx), shared.AdminBuildMFAURL.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/profile/password", // 个人中心修改密码
			Handler: authMw.Handle(ProfileUpdatePasswordHandler(serverCtx), shared.ProfileUpdatePassword.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/profile", // 个人中心修改资料
			Handler: authMw.Handle(ProfileUpdateMineHandler(serverCtx), shared.ProfileUpdateMine.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/profile/mfa-status", // 个人中心修改MFA状态
			Handler: authMw.Handle(ProfileUpdateMFAStatusHandler(serverCtx), shared.ProfileUpdateMFA.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/profile/mfa-secret", // 个人中心修改MFA秘钥
			Handler: authMw.Handle(ProfileUpdateMFASecureKeyHandler(serverCtx), shared.ProfileUpdateMFAKey.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/profile/mfa-secret/refresh", // 个人中心重新生成MFA秘钥
			Handler: authMw.Handle(ProfileRefreshMFASecretKeyHandler(serverCtx), shared.ProfileRefreshMFAKey.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/profile/avatar", // 个人中心修改头像
			Handler: authMw.Handle(ProfileUpdateAvatarHandler(serverCtx), shared.ProfileUpdateAvatar.Alias),
		},
	})
}
