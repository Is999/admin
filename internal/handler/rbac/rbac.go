package rbac

import (
	"admin/internal/handler/shared"
	"net/http"

	rbaclogic "admin/internal/logic/rbac"
	"admin/internal/svc"
	"admin/internal/types"
)

// ListRoleHandler 查询角色列表。
func ListRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RoleListReq](shared.RoleList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RoleListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// TreeRoleHandler 查询角色树。
func TreeRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.RoleTreeList, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := rbaclogic.NewAdminRoleLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(shared.ActionReq("tree_role"))
	})
}

// TreeRoleOptionsHandler 查询角色树下拉数据，仅要求登录态合法，不额外校验角色管理权限。
func TreeRoleOptionsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := rbaclogic.NewAdminRoleLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(shared.ActionReq("tree_role_options"))
	})
}

// AddRoleHandler 新增角色。
func AddRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CreateRoleReq](shared.RoleAdd,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreateRoleReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSaveRoleReq())
		},
	)(sCtx)
}

// UpdateRoleHandler 编辑角色。
func UpdateRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SaveRoleReq](shared.RoleUpdate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SaveRoleReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeleteRoleHandler 删除角色。
func DeleteRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.RoleDelete,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdateRoleStatusHandler 修改角色状态。
func UpdateRoleStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RoleStatusReq](shared.RoleStatusUpdate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RoleStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// GetRolePermissionHandler 查询角色权限树。
func GetRolePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RolePermissionReq](shared.RolePermissionTree,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RolePermissionReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.PermissionTree(req)
		},
	)(sCtx)
}

// UpdateRolePermissionHandler 编辑角色权限。
func UpdateRolePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RolePermissionSaveReq](shared.RolePermissionUpdate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RolePermissionSaveReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.SavePermissions(req)
		},
	)(sCtx)
}

// ListPermissionHandler 查询权限列表。
func ListPermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.PermissionListReq](shared.PermissionList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.PermissionListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// TreePermissionHandler 查询权限树。
func TreePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.PermissionTreeList, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := rbaclogic.NewAdminPermissionLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(shared.ActionReq("tree_permission"))
	})
}

// MaxPermissionUUIDHandler 查询下一个权限 UUID。
func MaxPermissionUUIDHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.PermissionMaxUUID, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := rbaclogic.NewAdminPermissionLogic(r, sCtx)
		return logicObj, logicObj.MaxUUID().WithReq(shared.ActionReq("max_permission_uuid"))
	})
}

// AddPermissionHandler 新增权限。
func AddPermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CreatePermissionReq](shared.PermissionAdd,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreatePermissionReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSavePermissionReq())
		},
	)(sCtx)
}

// UpdatePermissionHandler 编辑权限。
func UpdatePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SavePermissionReq](shared.PermissionUpdate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SavePermissionReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeletePermissionHandler 删除权限。
func DeletePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.PermissionDelete,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdatePermissionStatusHandler 修改权限状态。
func UpdatePermissionStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.PermissionStatusReq](shared.PermissionStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.PermissionStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := rbaclogic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}
