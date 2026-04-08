package types

import (
	"strings"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// adminPasswordMinLength 表示后台管理员密码允许的最小长度。
	adminPasswordMinLength uint8 = 8
	// adminPasswordMaxLength 表示后台管理员密码允许的最大长度。
	adminPasswordMaxLength uint8 = 12
)

// validateAdminPasswordRequired 校验必填的后台管理员强密码。
func validateAdminPasswordRequired(password string, fieldName string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return errors.Errorf("%s不能为空", fieldName)
	}
	return validateAdminPasswordStrength(password, fieldName)
}

// validateAdminPasswordOptional 校验可选的后台管理员强密码；为空时表示不修改。
func validateAdminPasswordOptional(password string, fieldName string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return nil
	}
	return validateAdminPasswordStrength(password, fieldName)
}

// validateAdminPasswordStrength 使用 go-utils 的 PassWord3 规则统一校验密码强度。
func validateAdminPasswordStrength(password string, fieldName string) error {
	if err := utils.PassWord3(password, adminPasswordMinLength, adminPasswordMaxLength); err != nil {
		return errors.Errorf("%s必须为8-12位，且包含大小写字母和数字，可使用特殊字符", fieldName)
	}
	return nil
}
