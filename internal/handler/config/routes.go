package config

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册系统常量配置接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回系统常量配置路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/dicts", shared.SysConfigList, ListSysConfigHandler),
		shared.AuthRoute(http.MethodPost, "/api/dicts", shared.SysConfigAdd, AddSysConfigHandler),
		shared.AuthRoute(http.MethodGet, "/api/dicts/export", shared.SysConfigExport, ExportSysConfigExcelHandler),
		shared.AuthRoute(http.MethodPost, "/api/dicts/import", shared.SysConfigImport, ImportSysConfigExcelHandler),
		shared.AuthRoute(http.MethodPatch, "/api/dicts/:id", shared.SysConfigUpdate, UpdateSysConfigHandler),
		shared.AuthRoute(http.MethodGet, "/api/dicts/cache/:uuid", shared.SysConfigCache, GetSysConfigCacheHandler),
		shared.AuthRoute(http.MethodPost, "/api/dicts/cache/refresh/:uuid", shared.SysConfigRenew, RenewSysConfigHandler),
	}
}
