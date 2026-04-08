package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerAuthRoutes 注册认证相关接口，包括登录、登出、刷新 token 和登录后初始化信息。
func registerAuthRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// auth 认证相关接口
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/auth/captcha", // 获取登录图形验证码
			Handler: authMw.PublicHandle(LoginCaptchaHandler(serverCtx), AuthCaptcha.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/login", // 管理员登录
			Handler: authMw.PublicHandle(LoginHandler(serverCtx), AuthLogin.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/refresh", // 刷新访问令牌
			Handler: authMw.Handle(RefreshAccessTokenHandler(serverCtx), AuthRefresh.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/auth/logout", // 管理员退出登录
			Handler: authMw.Handle(LogoutHandler(serverCtx), AuthLogout.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/auth/codes", // 获取当前用户权限码
			Handler: authMw.Handle(GetUserPermissionCodesHandler(serverCtx), AuthCodes.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/login/after/info", // 获取登录后的初始化信息
			Handler: authMw.Handle(LoginAfterInfoHandler(serverCtx), AuthLoginAfterInfo.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/buildSecretVerifyAccount", // 验证账号并返回 MFA 绑定信息
			Handler: authMw.PublicHandle(UserBuildSecretVerifyAccountHandler(serverCtx), UserBuildSecret.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin/mine", // 获取当前管理员资料
			Handler: authMw.Handle(UserMineHandler(serverCtx), UserMine.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/checkSecure", // 校验当前管理员密码
			Handler: authMw.Handle(UserCheckSecureHandler(serverCtx), UserCheckSecure.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin/checkMfaSecure", // 校验当前管理员MFA动态码
			Handler: authMw.Handle(UserCheckMFAHandler(serverCtx), UserCheckMFA.Alias),
		},
	})
}

// registerInternalAuthRoutes 注册仅供内网自举调用的认证相关接口。
func registerInternalAuthRoutes(server *rest.Server, serverCtx *svc.ServiceContext, internalMw *middleware.InternalOnlyMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/internal/auth/init-admin-bootstrap", // 内网初始化管理员账号
			Handler: internalMw.Handle(InitAdminBootstrapHandler(serverCtx), InternalInitAdminBootstrap.Alias),
		},
	})
}
