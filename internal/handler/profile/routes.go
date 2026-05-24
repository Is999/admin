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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回个人中心和账号安全管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodPatch, "/api/admins/mfa-status/:id", shared.AdminMFAStatus, AdminMFAStatusHandler),
		shared.AuthRoute(http.MethodGet, "/api/admins/mfa-secret-key-url/:id", shared.AdminBuildMFAURL, AdminBuildMFASecretKeyURLHandler),
		shared.AuthRoute(http.MethodPatch, "/api/profile/password", shared.ProfileUpdatePassword, ProfileUpdatePasswordHandler),
		shared.AuthRoute(http.MethodPatch, "/api/profile", shared.ProfileUpdateMine, ProfileUpdateMineHandler),
		shared.AuthRoute(http.MethodPatch, "/api/profile/mfa-status", shared.ProfileUpdateMFA, ProfileUpdateMFAStatusHandler),
		shared.AuthRoute(http.MethodPatch, "/api/profile/mfa-secret", shared.ProfileUpdateMFAKey, ProfileUpdateMFASecureKeyHandler),
		shared.AuthRoute(http.MethodPost, "/api/profile/mfa-secret/refresh", shared.ProfileRefreshMFAKey, ProfileRefreshMFASecretKeyHandler),
		shared.AuthRoute(http.MethodPatch, "/api/profile/avatar", shared.ProfileUpdateAvatar, ProfileUpdateAvatarHandler),
	}
}
