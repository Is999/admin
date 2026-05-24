package cachemanage

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册缓存管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回缓存管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/caches", shared.CacheList, ListCacheHandler),
		shared.AuthRoute(http.MethodGet, "/api/caches/server-info", shared.CacheServerInfo, GetCacheServerInfoHandler),
		shared.AuthRoute(http.MethodGet, "/api/caches/key-info", shared.CacheKeyInfo, GetCacheKeyInfoHandler),
		shared.AuthRoute(http.MethodGet, "/api/caches/keys", shared.CacheSearch, SearchCacheKeyHandler),
		shared.AuthRoute(http.MethodGet, "/api/caches/key-info/search", shared.CacheKeyInfo, GetCacheKeyInfoHandler),
		shared.AuthRoute(http.MethodPost, "/api/caches/refresh", shared.CacheRenew, RenewCacheHandler),
		shared.AuthRoute(http.MethodPost, "/api/caches/refresh-all", shared.CacheRenewAll, RenewAllCacheHandler),
		shared.AuthRoute(http.MethodPost, "/api/caches/warmup", shared.CacheWarmup, WarmupCacheHandler),
	}
}
