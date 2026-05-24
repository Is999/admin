package securitydebug

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册安全调试接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回安全调试路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodPost, "/api/security/debug/sign", shared.SecurityDebugSign, SecurityDebugSignHandler),
		shared.AuthRoute(http.MethodPost, "/api/security/debug/verify", shared.SecurityDebugVerify, SecurityDebugVerifyHandler),
		shared.AuthRoute(http.MethodPost, "/api/security/debug/encrypt", shared.SecurityDebugEncrypt, SecurityDebugEncryptHandler),
		shared.AuthRoute(http.MethodPost, "/api/security/debug/decrypt", shared.SecurityDebugDecrypt, SecurityDebugDecryptHandler),
	}
}
