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
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/roles", // 查询角色列表
			Handler: authMw.Handle(ListRoleHandler(serverCtx), shared.RoleList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/tree", // 查询角色树
			Handler: authMw.Handle(TreeRoleHandler(serverCtx), shared.RoleTreeList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/tree-options", // 查询角色树下拉数据
			Handler: authMw.Handle(TreeRoleOptionsHandler(serverCtx), shared.RoleTreeOptions.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/roles", // 新增角色
			Handler: authMw.Handle(AddRoleHandler(serverCtx), shared.RoleAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/:id", // 编辑角色
			Handler: authMw.Handle(UpdateRoleHandler(serverCtx), shared.RoleUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/status/:id", // 修改角色状态
			Handler: authMw.Handle(UpdateRoleStatusHandler(serverCtx), shared.RoleStatusUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/roles/permissions/:id", // 编辑角色权限
			Handler: authMw.Handle(UpdateRolePermissionHandler(serverCtx), shared.RolePermissionUpdate.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/roles/permissions/tree/:id/:isPid", // 查询角色权限树
			Handler: authMw.Handle(GetRolePermissionHandler(serverCtx), shared.RolePermissionTree.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/roles/:id", // 删除角色
			Handler: authMw.Handle(DeleteRoleHandler(serverCtx), shared.RoleDelete.Alias),
		},
	})
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions", // 查询权限列表
			Handler: authMw.Handle(ListPermissionHandler(serverCtx), shared.PermissionList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions/tree", // 查询权限树
			Handler: authMw.Handle(TreePermissionHandler(serverCtx), shared.PermissionTreeList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/permissions/max-uuid", // 查询下一个权限 UUID
			Handler: authMw.Handle(MaxPermissionUUIDHandler(serverCtx), shared.PermissionMaxUUID.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/permissions", // 新增权限
			Handler: authMw.Handle(AddPermissionHandler(serverCtx), shared.PermissionAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/permissions/:id", // 编辑权限
			Handler: authMw.Handle(UpdatePermissionHandler(serverCtx), shared.PermissionUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/permissions/status/:id", // 修改权限状态
			Handler: authMw.Handle(UpdatePermissionStatusHandler(serverCtx), shared.PermissionStatus.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/permissions/:id", // 删除权限
			Handler: authMw.Handle(DeletePermissionHandler(serverCtx), shared.PermissionDelete.Alias),
		},
	})
}
