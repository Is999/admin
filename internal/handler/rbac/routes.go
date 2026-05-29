package rbac

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回角色和权限管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/roles", // 查询角色列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleList,
			Description: shared.RoleList.Describe,
			Handler:     ListRoleHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/roles/tree", // 查询角色树。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleTreeList,
			Description: shared.RoleTreeList.Describe,
			Handler:     TreeRoleHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/roles/tree-options", // 查询角色树下拉。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleTreeOptions,
			Description: shared.RoleTreeOptions.Describe,
			Handler:     TreeRoleOptionsHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/roles", // 新增角色。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleAdd,
			Description: shared.RoleAdd.Describe,
			Handler:     AddRoleHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/roles/:id", // 编辑角色。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleUpdate,
			Description: shared.RoleUpdate.Describe,
			Handler:     UpdateRoleHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/roles/status/:id", // 修改角色状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleStatusUpdate,
			Description: shared.RoleStatusUpdate.Describe,
			Handler:     UpdateRoleStatusHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/roles/permissions/:id", // 编辑角色权限。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RolePermissionUpdate,
			Description: shared.RolePermissionUpdate.Describe,
			Handler:     UpdateRolePermissionHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/roles/permissions/tree/:id/:isPid", // 查询角色权限树。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RolePermissionTree,
			Description: shared.RolePermissionTree.Describe,
			Handler:     GetRolePermissionHandler,
		},
		{
			Method:      http.MethodDelete,
			Path:        "/api/roles/:id", // 删除角色。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.RoleDelete,
			Description: shared.RoleDelete.Describe,
			Handler:     DeleteRoleHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/permissions", // 查询权限列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionList,
			Description: shared.PermissionList.Describe,
			Handler:     ListPermissionHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/permissions/tree", // 查询权限树。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionTreeList,
			Description: shared.PermissionTreeList.Describe,
			Handler:     TreePermissionHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/permissions/max-uuid", // 查询下一个权限UUID。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionMaxUUID,
			Description: shared.PermissionMaxUUID.Describe,
			Handler:     MaxPermissionUUIDHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/permissions", // 新增权限。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionAdd,
			Description: shared.PermissionAdd.Describe,
			Handler:     AddPermissionHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/permissions/:id", // 编辑权限。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionUpdate,
			Description: shared.PermissionUpdate.Describe,
			Handler:     UpdatePermissionHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/permissions/status/:id", // 修改权限状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionStatus,
			Description: shared.PermissionStatus.Describe,
			Handler:     UpdatePermissionStatusHandler,
		},
		{
			Method:      http.MethodDelete,
			Path:        "/api/permissions/:id", // 删除权限。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.PermissionDelete,
			Description: shared.PermissionDelete.Describe,
			Handler:     DeletePermissionHandler,
		},
	}
}
