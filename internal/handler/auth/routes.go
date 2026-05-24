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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回认证、会话和个人中心安全路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.PublicRoute(http.MethodGet, "/api/auth/captcha", shared.AuthCaptcha, LoginCaptchaHandler),
		shared.PublicRoute(http.MethodPost, "/api/auth/login", shared.AuthLogin, LoginHandler),
		shared.AuthRoute(http.MethodPost, "/api/auth/refresh", shared.AuthRefresh, adminhandler.RefreshAccessTokenHandler),
		shared.AuthRoute(http.MethodPost, "/api/auth/logout", shared.AuthLogout, LogoutHandler),
		shared.AuthRoute(http.MethodGet, "/api/auth/codes", shared.AuthCodes, adminhandler.GetUserPermissionCodesHandler),
		shared.AuthRoute(http.MethodGet, "/api/auth/profile", shared.AuthProfile, adminhandler.AuthProfileHandler),
		shared.PublicRoute(http.MethodPost, "/api/auth/verify-account", shared.AuthVerifyAccount, profilehandler.AuthVerifyAccountHandler),
		shared.AuthRoute(http.MethodGet, "/api/profile", shared.ProfileMine, profilehandler.ProfileMineHandler),
		shared.AuthRoute(http.MethodPost, "/api/profile/check-secure", shared.ProfileCheckSecure, profilehandler.ProfileCheckSecureHandler),
		shared.AuthRoute(http.MethodPost, "/api/profile/check-mfa", shared.ProfileCheckMFA, profilehandler.ProfileCheckMFAHandler),
	}
}

// RegisterInternalRoutes 注册仅供内网自举调用的认证相关接口。
func RegisterInternalRoutes(server *rest.Server, serverCtx *svc.ServiceContext, internalMw *middleware.InternalOnlyMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, nil, internalMw, InternalRouteSpecs())
}

// InternalRouteSpecs 返回内网认证自举路由规格。
func InternalRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.InternalRoute(http.MethodPost, "/internal/auth/init-admin-bootstrap", shared.InternalInitAdminBootstrap, InitAdminBootstrapHandler),
	}
}
