package logic

import (
	"strings"

	"admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	drivermysql "github.com/go-sql-driver/mysql"
)

const mysqlDuplicateEntryErrorNumber uint16 = 1062

var (
	// errAdminNameAlreadyExists 表示管理员登录名已存在。
	errAdminNameAlreadyExists = errors.New("管理员账号已存在")
	// errRoleTitleAlreadyExists 表示角色名称已存在。
	errRoleTitleAlreadyExists = errors.New("角色名称已存在")
	// errPermissionUUIDAlreadyExists 表示权限 UUID 已存在。
	errPermissionUUIDAlreadyExists = errors.New("权限UUID已存在")
)

// isMySQLDuplicateEntryError 判断错误链中是否包含 MySQL 唯一键冲突。
func isMySQLDuplicateEntryError(err error) bool {
	var mysqlErr *drivermysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntryErrorNumber
}

// adminNameAlreadyExistsResult 返回管理员账号重复的明确业务响应。
func adminNameAlreadyExistsResult(username string, cause error) *types.BizResult {
	username = strings.TrimSpace(username)
	if cause == nil {
		cause = errAdminNameAlreadyExists
	}
	return types.NewBizResult(codes.UserAlreadyExists).
		SetI18nMessage(i18n.MsgKeyUserExistsFormat, username).
		WithError(errors.Wrapf(cause, "管理员账号[%s]已存在", username))
}

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
