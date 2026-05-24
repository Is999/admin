package rbac

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册角色和权限管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回角色和权限管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/roles", shared.RoleList, ListRoleHandler),
		shared.AuthRoute(http.MethodGet, "/api/roles/tree", shared.RoleTreeList, TreeRoleHandler),
		shared.AuthRoute(http.MethodGet, "/api/roles/tree-options", shared.RoleTreeOptions, TreeRoleOptionsHandler),
		shared.AuthRoute(http.MethodPost, "/api/roles", shared.RoleAdd, AddRoleHandler),
		shared.AuthRoute(http.MethodPatch, "/api/roles/:id", shared.RoleUpdate, UpdateRoleHandler),
		shared.AuthRoute(http.MethodPatch, "/api/roles/status/:id", shared.RoleStatusUpdate, UpdateRoleStatusHandler),
		shared.AuthRoute(http.MethodPatch, "/api/roles/permissions/:id", shared.RolePermissionUpdate, UpdateRolePermissionHandler),
		shared.AuthRoute(http.MethodGet, "/api/roles/permissions/tree/:id/:isPid", shared.RolePermissionTree, GetRolePermissionHandler),
		shared.AuthRoute(http.MethodDelete, "/api/roles/:id", shared.RoleDelete, DeleteRoleHandler),
		shared.AuthRoute(http.MethodGet, "/api/permissions", shared.PermissionList, ListPermissionHandler),
		shared.AuthRoute(http.MethodGet, "/api/permissions/tree", shared.PermissionTreeList, TreePermissionHandler),
		shared.AuthRoute(http.MethodGet, "/api/permissions/max-uuid", shared.PermissionMaxUUID, MaxPermissionUUIDHandler),
		shared.AuthRoute(http.MethodPost, "/api/permissions", shared.PermissionAdd, AddPermissionHandler),
		shared.AuthRoute(http.MethodPatch, "/api/permissions/:id", shared.PermissionUpdate, UpdatePermissionHandler),
		shared.AuthRoute(http.MethodPatch, "/api/permissions/status/:id", shared.PermissionStatus, UpdatePermissionStatusHandler),
		shared.AuthRoute(http.MethodDelete, "/api/permissions/:id", shared.PermissionDelete, DeletePermissionHandler),
	}
}
