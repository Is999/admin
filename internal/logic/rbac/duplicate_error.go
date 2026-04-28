package rbac

import (
	"strings"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

var (
	// errRoleTitleAlreadyExists 表示角色名称已存在。
	errRoleTitleAlreadyExists = errors.New("角色名称已存在")
	// errPermissionUUIDAlreadyExists 表示权限 UUID 已存在。
	errPermissionUUIDAlreadyExists = errors.New("权限UUID已存在")
	// ErrRoleTitleAlreadyExists 表示角色名称已存在。
	ErrRoleTitleAlreadyExists = errRoleTitleAlreadyExists
	// ErrPermissionUUIDAlreadyExists 表示权限 UUID 已存在。
	ErrPermissionUUIDAlreadyExists = errPermissionUUIDAlreadyExists
)

// roleTitleAlreadyExistsResult 返回角色名称重复的明确业务响应。
func roleTitleAlreadyExistsResult(title string, cause error) *types.BizResult {
	title = strings.TrimSpace(title)
	if cause == nil {
		cause = errRoleTitleAlreadyExists
	}
	return types.NewBizResult(codes.AdminRoleAlreadyExists).
		SetI18nMessage(i18n.MsgKeyRoleExistsFormat, title).
		WithError(errors.Wrapf(cause, "角色名称[%s]已存在", title))
}

// RoleTitleAlreadyExistsResult 返回角色名称重复的明确业务响应。
func RoleTitleAlreadyExistsResult(title string, cause error) *types.BizResult {
	return roleTitleAlreadyExistsResult(title, cause)
}

// permissionUUIDAlreadyExistsResult 返回权限 UUID 重复的明确业务响应。
func permissionUUIDAlreadyExistsResult(uuid string, cause error) *types.BizResult {
	uuid = strings.TrimSpace(uuid)
	if cause == nil {
		cause = errPermissionUUIDAlreadyExists
	}
	return types.NewBizResult(codes.AdminPermissionAlreadyExists).
		SetI18nMessage(i18n.MsgKeyPermissionExistsFormat, uuid).
		WithError(errors.Wrapf(cause, "权限UUID[%s]已存在", uuid))
}

// PermissionUUIDAlreadyExistsResult 返回权限 UUID 重复的明确业务响应。
func PermissionUUIDAlreadyExistsResult(uuid string, cause error) *types.BizResult {
	return permissionUUIDAlreadyExistsResult(uuid, cause)
}
