package admin

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回管理员管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/admins", // 查询管理员列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminList,
			Description: shared.AdminList.Describe,
			Handler:     ListAdminHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admins/exports", // 异步导出管理员列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminExportTrigger,
			Description: shared.AdminExportTrigger.Describe,
			Handler:     TriggerAdminExportHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admins/exports/status/:jobId", // 查询管理员导出进度。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminExportStatus,
			Description: shared.AdminExportStatus.Describe,
			Handler:     GetAdminExportStatusHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admins/exports/download/:jobId", // 下载管理员导出文件。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminExportDownload,
			Description: shared.AdminExportDownload.Describe,
			Handler:     DownloadAdminExportHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admins", // 新增管理员。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminAdd,
			Description: shared.AdminAdd.Describe,
			Handler:     AddAdminHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/admins/status/:id", // 修改管理员状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminStatusUpdate,
			Description: shared.AdminStatusUpdate.Describe,
			Handler:     UpdateAdminStatusHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admins/password/reset/:id", // 重置管理员密码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminPasswordReset,
			Description: shared.AdminPasswordReset.Describe,
			Handler:     ResetAdminPasswordHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admins/initial-state/reset/:id", // 重置管理员到首次登录前状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminResetInitialState,
			Description: shared.AdminResetInitialState.Describe,
			Handler:     ResetAdminInitialStateHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admins/roles/:id", // 查询管理员角色。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminRoleList,
			Description: shared.AdminRoleList.Describe,
			Handler:     ListAdminRolesHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/admins/roles/:id", // 编辑管理员角色。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminRoleUpdate,
			Description: shared.AdminRoleUpdate.Describe,
			Handler:     UpdateAdminRolesHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admins/:id", // 查询管理员详情。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminInfo,
			Description: shared.AdminInfo.Describe,
			Handler:     GetAdminHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/admins/:id", // 编辑管理员。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminUpdate,
			Description: shared.AdminUpdate.Describe,
			Handler:     UpdateAdminHandler,
		},
		{
			Method:      http.MethodDelete,
			Path:        "/api/admins/:id", // 删除管理员。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminDelete,
			Description: shared.AdminDelete.Describe,
			Handler:     DeleteAdminHandler,
		},
	}
}

// LogRouteSpecs 返回管理员审计日志路由规格。
func LogRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-logs", // 查询管理员审计日志。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminLogQuery,
			Description: shared.AdminLogQuery.Describe,
			Handler:     QueryAdminLogHandler,
		},
	}
}
