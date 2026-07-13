package auth

import (
	"net/http"

	adminhandler "admin/internal/handler/admin"
	profilehandler "admin/internal/handler/profile"
	"admin/internal/handler/shared"
)

// RouteSpecs 返回认证、会话和个人中心安全路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/auth/captcha", // 获取登录图形验证码。
			Access:      shared.RouteAccessPublic,
			Meta:        shared.AuthCaptcha,
			Description: shared.AuthCaptcha.Describe,
			Handler:     LoginCaptchaHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/auth/login", // 管理员登录。
			Access:      shared.RouteAccessPublic,
			Meta:        shared.AuthLogin,
			Description: shared.AuthLogin.Describe,
			Handler:     LoginHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/auth/refresh", // 在当前 JWT 和 Redis 会话仍有效时主动续签访问令牌。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AuthRefresh,
			Description: shared.AuthRefresh.Describe,
			Handler:     adminhandler.RefreshAccessTokenHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/auth/logout", // 管理员退出登录。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AuthLogout,
			Description: shared.AuthLogout.Describe,
			Handler:     LogoutHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/auth/codes", // 获取当前用户权限码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AuthCodes,
			Description: shared.AuthCodes.Describe,
			Handler:     adminhandler.GetUserPermissionCodesHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/auth/profile", // 获取当前登录资料。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AuthProfile,
			Description: shared.AuthProfile.Describe,
			Handler:     adminhandler.AuthProfileHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/profile", // 获取当前管理员资料。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileMine,
			Description: shared.ProfileMine.Describe,
			Handler:     profilehandler.ProfileMineHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/profile/check-secure", // 校验当前管理员密码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileCheckSecure,
			Description: shared.ProfileCheckSecure.Describe,
			Handler:     profilehandler.ProfileCheckSecureHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/profile/check-mfa", // 校验当前管理员MFA动态码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileCheckMFA,
			Description: shared.ProfileCheckMFA.Describe,
			Handler:     profilehandler.ProfileCheckMFAHandler,
		},
	}
}

// InternalRouteSpecs 返回内网认证自举路由规格。
func InternalRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/internal/auth/init-admin-bootstrap", // 内网初始化管理员账号。
			Access:      shared.RouteAccessInternal,
			Meta:        shared.InternalInitAdminBootstrap,
			Description: shared.InternalInitAdminBootstrap.Describe,
			Handler:     InitAdminBootstrapHandler,
		},
	}
}
