package auth

import (
	"net/http"

	adminhandler "admin/internal/handler/admin"
	profilehandler "admin/internal/handler/profile"
	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册认证、会话和个人中心安全接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// auth 认证相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/auth/captcha", // 获取登录图形验证码
			Handler: authMw.PublicHandle(LoginCaptchaHandler(serverCtx), shared.AuthCaptcha.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/login", // 管理员登录
			Handler: authMw.PublicHandle(LoginHandler(serverCtx), shared.AuthLogin.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/refresh", // 刷新访问令牌
			Handler: authMw.Handle(adminhandler.RefreshAccessTokenHandler(serverCtx), shared.AuthRefresh.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/logout", // 管理员退出登录
			Handler: authMw.Handle(LogoutHandler(serverCtx), shared.AuthLogout.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/auth/codes", // 获取当前用户权限码
			Handler: authMw.Handle(adminhandler.GetUserPermissionCodesHandler(serverCtx), shared.AuthCodes.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/auth/profile", // 获取当前登录资料
			Handler: authMw.Handle(adminhandler.AuthProfileHandler(serverCtx), shared.AuthProfile.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/verify-account", // 验证账号并返回 MFA 绑定信息
			Handler: authMw.PublicHandle(profilehandler.AuthVerifyAccountHandler(serverCtx), shared.AuthVerifyAccount.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/profile", // 获取当前管理员资料
			Handler: authMw.Handle(profilehandler.ProfileMineHandler(serverCtx), shared.ProfileMine.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/profile/check-secure", // 校验当前管理员密码
			Handler: authMw.Handle(profilehandler.ProfileCheckSecureHandler(serverCtx), shared.ProfileCheckSecure.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/profile/check-mfa", // 校验当前管理员MFA动态码
			Handler: authMw.Handle(profilehandler.ProfileCheckMFAHandler(serverCtx), shared.ProfileCheckMFA.Alias),
		},
	})
}

// RegisterInternalRoutes 注册仅供内网自举调用的认证相关接口。
func RegisterInternalRoutes(server *rest.Server, serverCtx *svc.ServiceContext, internalMw *middleware.InternalOnlyMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/internal/auth/init-admin-bootstrap", // 内网初始化管理员账号
			Handler: internalMw.Handle(InitAdminBootstrapHandler(serverCtx), shared.InternalInitAdminBootstrap.Alias),
		},
	})
}
