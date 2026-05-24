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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回管理员管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/admins", shared.AdminList, ListAdminHandler),
		shared.AuthRoute(http.MethodPost, "/api/admins/exports", shared.AdminExportTrigger, TriggerAdminExportHandler),
		shared.AuthRoute(http.MethodGet, "/api/admins/exports/status/:jobId", shared.AdminExportStatus, GetAdminExportStatusHandler),
		shared.AuthRoute(http.MethodGet, "/api/admins/exports/download/:jobId", shared.AdminExportDownload, DownloadAdminExportHandler),
		shared.AuthRoute(http.MethodPost, "/api/admins", shared.AdminAdd, AddAdminHandler),
		shared.AuthRoute(http.MethodPatch, "/api/admins/status/:id", shared.AdminStatusUpdate, UpdateAdminStatusHandler),
		shared.AuthRoute(http.MethodPost, "/api/admins/password/reset/:id", shared.AdminPasswordReset, ResetAdminPasswordHandler),
		shared.AuthRoute(http.MethodPost, "/api/admins/initial-state/reset/:id", shared.AdminResetInitialState, ResetAdminInitialStateHandler),
		shared.AuthRoute(http.MethodGet, "/api/admins/roles/:id", shared.AdminRoleList, ListAdminRolesHandler),
		shared.AuthRoute(http.MethodPatch, "/api/admins/roles/:id", shared.AdminRoleUpdate, UpdateAdminRolesHandler),
		shared.AuthRoute(http.MethodGet, "/api/admins/:id", shared.AdminInfo, GetAdminHandler),
		shared.AuthRoute(http.MethodPatch, "/api/admins/:id", shared.AdminUpdate, UpdateAdminHandler),
		shared.AuthRoute(http.MethodDelete, "/api/admins/:id", shared.AdminDelete, DeleteAdminHandler),
	}
}

// RegisterLogRoutes 注册管理员审计日志接口。
func RegisterLogRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, LogRouteSpecs())
}

// LogRouteSpecs 返回管理员审计日志路由规格。
func LogRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/admin-logs", shared.AdminLogQuery, QueryAdminLogHandler),
	}
}
