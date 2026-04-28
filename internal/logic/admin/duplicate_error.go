package admin

import (
	"strings"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

var (
	// errAdminNameAlreadyExists 表示管理员登录名已存在。
	errAdminNameAlreadyExists = errors.New("管理员账号已存在")
	// ErrAdminNameAlreadyExists 表示管理员登录名已存在。
	ErrAdminNameAlreadyExists = errAdminNameAlreadyExists
)

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

// AdminNameAlreadyExistsResult 返回管理员账号重复的明确业务响应。
func AdminNameAlreadyExistsResult(username string, cause error) *types.BizResult {
	return adminNameAlreadyExistsResult(username, cause)
}
