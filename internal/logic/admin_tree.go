package logic

import (
	"admin/internal/model"
	"admin/internal/types"
)

// BuildAdminRoleTree 把平铺角色列表转换成后台通用角色树。
func BuildAdminRoleTree(roles []model.AdminRole, permissionMap map[int][]int) []types.AdminRoleItem {
	children := make(map[int][]model.AdminRole, len(roles))
	for _, role := range roles {
		children[role.Pid] = append(children[role.Pid], role)
	}
	var walk func(pid int) []types.AdminRoleItem
	walk = func(pid int) []types.AdminRoleItem {
		nodes := children[pid]
		result := make([]types.AdminRoleItem, 0, len(nodes))
		for _, role := range nodes {
			result = append(result, AdminRoleModelToItem(role, permissionMap[role.ID], walk(role.ID)))
		}
		return result
	}
	return walk(0)
}

// AdminRoleModelToItem 把角色模型转换成后台通用角色响应节点。
func AdminRoleModelToItem(role model.AdminRole, permissionIDs []int, children []types.AdminRoleItem) types.AdminRoleItem {
	disabled := role.Status != 1 || role.IsDelete != 0
	return types.AdminRoleItem{
		ID:              role.ID,
		Title:           role.Title,
		Pid:             role.Pid,
		Pids:            role.Pids,
		Status:          role.Status,
		Description:     role.Describe,
		IsDelete:        role.IsDelete,
		Disabled:        disabled,
		DisableCheckbox: disabled,
		Selectable:      !disabled,
		Permissions:     permissionIDs,
		Children:        children,
		CreatedAt:       FormatDateTime(role.CreatedAt),
		UpdatedAt:       FormatDateTime(role.UpdatedAt),
	}
}

// BuildAdminPermissionTree 把平铺权限列表转换成后台通用权限树。
func BuildAdminPermissionTree(permissions []model.AdminPermission, checked map[int]struct{}, disabled map[int]struct{}) []types.AdminPermissionItem {
	children := make(map[int][]model.AdminPermission, len(permissions))
	for _, permission := range permissions {
		children[permission.Pid] = append(children[permission.Pid], permission)
	}
	var walk func(pid int) []types.AdminPermissionItem
	walk = func(pid int) []types.AdminPermissionItem {
		nodes := children[pid]
		result := make([]types.AdminPermissionItem, 0, len(nodes))
		for _, permission := range nodes {
			_, isChecked := checked[permission.ID]
			_, isDisabled := disabled[permission.ID]
			result = append(result, AdminPermissionModelToItem(permission, isChecked, isDisabled, walk(permission.ID)))
		}
		return result
	}
	return walk(0)
}

// AdminPermissionModelToItem 把权限模型转换成后台通用权限响应节点。
func AdminPermissionModelToItem(permission model.AdminPermission, checked bool, disabled bool, children []types.AdminPermissionItem) types.AdminPermissionItem {
	return types.AdminPermissionItem{
		ID:              permission.ID,
		UUID:            permission.UUID,
		Title:           permission.Title,
		Module:          permission.Module,
		Pid:             permission.Pid,
		Pids:            permission.Pids,
		Type:            permission.Type,
		Description:     permission.Description,
		Status:          permission.Status,
		Checked:         checked,
		Disabled:        disabled,
		DisableCheckbox: disabled,
		Selectable:      !disabled,
		Children:        children,
		CreatedAt:       FormatDateTime(permission.CreatedAt),
		UpdatedAt:       FormatDateTime(permission.UpdatedAt),
	}
}
