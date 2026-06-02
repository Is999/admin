package cachemanage

import (
	"admin/internal/handler/shared"
	"net/http"

	cachemanagelogic "admin/internal/logic/cachemanage"
	"admin/internal/svc"
	"admin/internal/types"
)

// ListCacheHandler 查询缓存目标列表。
func ListCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CacheListReq](shared.MethodListCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := cachemanagelogic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetCacheServerInfoHandler 查询 Redis 服务器信息。
func GetCacheServerInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodGetCacheServerInfo, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := cachemanagelogic.NewSystemCacheLogic(r, sCtx)
		return logicObj, logicObj.ServerInfo().WithReq(shared.ActionReq("cache_server_info"))
	})
}

// GetCacheKeyInfoHandler 查询 Redis key 信息。
func GetCacheKeyInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CacheKeyReq](shared.MethodGetCacheKeyInfo,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheKeyReq) (shared.LogicObj, *types.BizResult) {
			logicObj := cachemanagelogic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.KeyInfo(req)
		},
	)(sCtx)
}

// SearchCacheKeyHandler 搜索 Redis key。
func SearchCacheKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CacheKeyReq](shared.MethodSearchCacheKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheKeyReq) (shared.LogicObj, *types.BizResult) {
			logicObj := cachemanagelogic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.SearchKey(req)
		},
	)(sCtx)
}

// RenewCacheHandler 刷新指定缓存。
func RenewCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CacheRenewReq](shared.MethodRenewCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheRenewReq) (shared.LogicObj, *types.BizResult) {
			logicObj := cachemanagelogic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// RenewAllCacheHandler 刷新全部内置缓存。
func RenewAllCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodRenewAllCache, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := cachemanagelogic.NewSystemCacheLogic(r, sCtx)
		return logicObj, logicObj.RenewAll().WithReq(shared.ActionReq("renew_all_cache"))
	})
}

// WarmupCacheHandler 按模板预热缓存，解决模板 key 在 Redis 未命中时无法全量刷新的问题。
func WarmupCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CacheWarmupReq](shared.MethodWarmupCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheWarmupReq) (shared.LogicObj, *types.BizResult) {
			logicObj := cachemanagelogic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.WarmupTemplate(req)
		},
	)(sCtx)
}
