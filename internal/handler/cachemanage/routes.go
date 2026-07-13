package cachemanage

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回缓存管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/caches", // 查询缓存列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheList,
			Description: shared.CacheList.Describe,
			Handler:     ListCacheHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/caches/server-info", // 查看缓存服务器信息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheServerInfo,
			Description: shared.CacheServerInfo.Describe,
			Handler:     GetCacheServerInfoHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/caches/metrics", // 查看当前进程表缓存运行指标。
			Access:      shared.RouteAccessAuth,
			Alias:       shared.CacheServerInfo.Alias,
			Description: "查看表缓存运行指标",
			Handler:     GetCacheMetricsHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/caches/key-info", // 查看缓存键信息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheKeyInfo,
			Description: shared.CacheKeyInfo.Describe,
			Handler:     GetCacheKeyInfoHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/caches/keys", // 搜索缓存键。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheSearch,
			Description: shared.CacheSearch.Describe,
			Handler:     SearchCacheKeyHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/caches/key-info/search", // 查看缓存键信息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheKeyInfo,
			Description: shared.CacheKeyInfo.Describe,
			Handler:     GetCacheKeyInfoHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/caches/refresh", // 刷新缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheRenew,
			Description: shared.CacheRenew.Describe,
			Handler:     RenewCacheHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/caches/refresh-all", // 刷新全部缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheRenewAll,
			Description: shared.CacheRenewAll.Describe,
			Handler:     RenewAllCacheHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/caches/warmup", // 按模板预热缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CacheWarmup,
			Description: shared.CacheWarmup.Describe,
			Handler:     WarmupCacheHandler,
		},
	}
}
