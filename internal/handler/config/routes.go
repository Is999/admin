package config

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回系统常量配置路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/dicts", // 查询系统配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigList,
			Description: shared.SysConfigList.Describe,
			Handler:     ListSysConfigHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/dicts", // 新增系统配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigAdd,
			Description: shared.SysConfigAdd.Describe,
			Handler:     AddSysConfigHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/dicts/export", // 导出系统配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigExport,
			Description: shared.SysConfigExport.Describe,
			Handler:     ExportSysConfigExcelHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/dicts/import", // 导入系统配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigImport,
			Description: shared.SysConfigImport.Describe,
			Handler:     ImportSysConfigExcelHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/dicts/:id", // 编辑系统配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigUpdate,
			Description: shared.SysConfigUpdate.Describe,
			Handler:     UpdateSysConfigHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/dicts/cache/:uuid", // 查看系统配置缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigCache,
			Description: shared.SysConfigCache.Describe,
			Handler:     GetSysConfigCacheHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/dicts/cache/refresh/:uuid", // 刷新系统配置缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SysConfigRenew,
			Description: shared.SysConfigRenew.Describe,
			Handler:     RenewSysConfigHandler,
		},
	}
}
