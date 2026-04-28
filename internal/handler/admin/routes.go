package admin

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册管理员管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admins", // 查询管理员列表
			Handler: authMw.Handle(ListAdminHandler(serverCtx), shared.AdminList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/exports", // 提交管理员列表异步导出任务
			Handler: authMw.Handle(TriggerAdminExportHandler(serverCtx), shared.AdminExportTrigger.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/exports/status/:jobId", // 查询管理员列表异步导出进度
			Handler: authMw.Handle(GetAdminExportStatusHandler(serverCtx), shared.AdminExportStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/exports/download/:jobId", // 下载管理员列表导出文件
			Handler: authMw.Handle(DownloadAdminExportHandler(serverCtx), shared.AdminExportDownload.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins", // 新增管理员
			Handler: authMw.Handle(AddAdminHandler(serverCtx), shared.AdminAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/status/:id", // 修改管理员状态
			Handler: authMw.Handle(UpdateAdminStatusHandler(serverCtx), shared.AdminStatusUpdate.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/password/reset/:id", // 重置管理员密码
			Handler: authMw.Handle(ResetAdminPasswordHandler(serverCtx), shared.AdminPasswordReset.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admins/initial-state/reset/:id", // 重置管理员到首次登录前状态
			Handler: authMw.Handle(ResetAdminInitialStateHandler(serverCtx), shared.AdminResetInitialState.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/roles/:id", // 查询管理员角色
			Handler: authMw.Handle(ListAdminRolesHandler(serverCtx), shared.AdminRoleList.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/roles/:id", // 编辑管理员角色
			Handler: authMw.Handle(UpdateAdminRolesHandler(serverCtx), shared.AdminRoleUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admins/:id", // 查询管理员详情
			Handler: authMw.Handle(GetAdminHandler(serverCtx), shared.AdminInfo.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admins/:id", // 编辑管理员
			Handler: authMw.Handle(UpdateAdminHandler(serverCtx), shared.AdminUpdate.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/admins/:id", // 删除管理员
			Handler: authMw.Handle(DeleteAdminHandler(serverCtx), shared.AdminDelete.Alias),
		},
	})
}

// RegisterLogRoutes 注册管理员审计日志接口。
func RegisterLogRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-logs", // 查询管理员审计日志
			Handler: authMw.Handle(QueryAdminLogHandler(serverCtx), shared.AdminLogQuery.Alias),
		},
	})
}
