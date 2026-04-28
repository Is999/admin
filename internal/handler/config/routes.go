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
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts", // 查询系统常量配置列表
			Handler: authMw.Handle(ListSysConfigHandler(serverCtx), shared.SysConfigList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts", // 新增系统常量配置
			Handler: authMw.Handle(AddSysConfigHandler(serverCtx), shared.SysConfigAdd.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts/export", // 导出系统常量配置 Excel
			Handler: authMw.Handle(ExportSysConfigExcelHandler(serverCtx), shared.SysConfigExport.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts/import", // 导入系统常量配置 Excel
			Handler: authMw.Handle(ImportSysConfigExcelHandler(serverCtx), shared.SysConfigImport.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/dicts/:id", // 编辑系统常量配置
			Handler: authMw.Handle(UpdateSysConfigHandler(serverCtx), shared.SysConfigUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/dicts/cache/:uuid", // 查看系统常量配置缓存
			Handler: authMw.Handle(GetSysConfigCacheHandler(serverCtx), shared.SysConfigCache.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/dicts/cache/refresh/:uuid", // 刷新系统常量配置缓存
			Handler: authMw.Handle(RenewSysConfigHandler(serverCtx), shared.SysConfigRenew.Alias),
		},
	})
}
