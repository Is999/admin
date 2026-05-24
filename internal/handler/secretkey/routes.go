package secretkey

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册秘钥管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回秘钥管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/secret-keys", shared.SecretKeyList, ListSecretKeyHandler),
		shared.AuthRoute(http.MethodGet, "/api/secret-keys/:id", shared.SecretKeyGet, GetSecretKeyHandler),
		shared.AuthRoute(http.MethodPost, "/api/secret-keys", shared.SecretKeyAdd, AddSecretKeyHandler),
		shared.AuthRoute(http.MethodPatch, "/api/secret-keys/:id", shared.SecretKeyUpdate, UpdateSecretKeyHandler),
		shared.AuthRoute(http.MethodPatch, "/api/secret-keys/status/:id", shared.SecretKeyStatus, UpdateSecretKeyStatusHandler),
		shared.AuthRoute(http.MethodPost, "/api/secret-keys/cache/refresh/:uuid", shared.SecretKeyRenew, RenewSecretKeyHandler),
		shared.AuthRoute(http.MethodPost, "/api/secret-keys/validate", shared.SecretKeyValidate, ValidateSecretKeyHandler),
		shared.AuthRoute(http.MethodPost, "/api/secret-keys/self-check/:uuid", shared.SecretKeySelfCheck, SelfCheckSecretKeyHandler),
	}
}
