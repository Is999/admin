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
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/caches", // 查询缓存列表
			Handler: authMw.Handle(ListCacheHandler(serverCtx), shared.CacheList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/server-info", // 查询 Redis 服务器信息
			Handler: authMw.Handle(GetCacheServerInfoHandler(serverCtx), shared.CacheServerInfo.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/key-info", // 查询缓存键信息
			Handler: authMw.Handle(GetCacheKeyInfoHandler(serverCtx), shared.CacheKeyInfo.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/keys", // 搜索缓存键
			Handler: authMw.Handle(SearchCacheKeyHandler(serverCtx), shared.CacheSearch.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/caches/key-info/search", // 查询搜索缓存键信息
			Handler: authMw.Handle(GetCacheKeyInfoHandler(serverCtx), shared.CacheKeyInfo.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/refresh", // 刷新指定缓存
			Handler: authMw.Handle(RenewCacheHandler(serverCtx), shared.CacheRenew.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/refresh-all", // 刷新全部缓存
			Handler: authMw.Handle(RenewAllCacheHandler(serverCtx), shared.CacheRenewAll.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/caches/warmup", // 按模板预热缓存
			Handler: authMw.Handle(WarmupCacheHandler(serverCtx), shared.CacheWarmup.Alias),
		},
	})
}
