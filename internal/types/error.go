//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"fmt"
	"strings"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"

	"github.com/Is999/go-utils/errors"
)

// Error 统一封装业务失败信息，兼顾业务码、国际化消息和底层错误链。
type Error struct {
	Code       int    // 业务状态码，对应 `common/codes` 中的错误语义
	MessageKey string // 国际化消息键，用于最终响应文案解析
	Args       []any  // 国际化消息模板参数
	Cause      error  // 原始错误，用于日志和 errors.Is/errors.As 判断
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("Error(code=%d, key=%s): %v", e.Code, e.MessageKey, e.Cause)
	}
	return fmt.Sprintf("Error(code=%d, key=%s)", e.Code, e.MessageKey)
}

// Unwrap 返回原始错误，支持 errors.Is 和 errors.As。
func (e *Error) Unwrap() error {
	return e.Cause
}

// ToBizResult 把业务错误转换成统一响应对象，便于 handler 直接写出标准失败响应。
func (e *Error) ToBizResult() *BizResult {
	return NewBizResult(e.Code).
		SetI18nMessage(e.MessageKey, e.Args...).
		WithError(e.Cause)
}

// NotFound 创建资源不存在错误。
func NotFound(msgKey string, cause error, args ...any) *Error {
	return &Error{
		Code:       codes.NotFound,
		MessageKey: msgKey,
		Args:       unwrapErrorArgs(msgKey, args),
		Cause:      wrapCauseWithContext(msgKey, cause, args),
	}
}

// DBError 创建数据库读写相关错误。
func DBError(msgKey string, cause error, args ...any) *Error {
	return &Error{
		Code:       codes.DBError,
		MessageKey: msgKey,
		Args:       unwrapErrorArgs(msgKey, args),
		Cause:      wrapCauseWithContext(msgKey, cause, args),
	}
}

// ParamError 创建参数校验或解析错误。
func ParamError(cause error) *Error {
	return &Error{
		Code:       codes.ParamError,
		MessageKey: i18n.MsgKeyParamError,
		Cause:      errors.Tag(cause),
	}
}

// ServerError 创建服务端内部错误。
func ServerError(msgKey string, cause error, args ...any) *Error {
	return &Error{
		Code:       codes.ServerError,
		MessageKey: msgKey,
		Args:       unwrapErrorArgs(msgKey, args),
		Cause:      wrapCauseWithContext(msgKey, cause, args),
	}
}

// Forbidden 创建权限不足错误。
func Forbidden(msgKey string, args ...any) *Error {
	return &Error{
		Code:       codes.Forbidden,
		MessageKey: msgKey,
		Args:       args,
	}
}

// Unauthorized 创建未授权错误。
func Unauthorized(msgKey string, args ...any) *Error {
	return &Error{
		Code:       codes.Unauthorized,
		MessageKey: msgKey,
		Args:       args,
	}
}

// Errorf 创建带格式化上下文的业务错误，并保留底层 cause 作为错误链尾部。
func Errorf(code int, msgKey string, cause error, format string, args ...any) *Error {
	format = strings.TrimSpace(format)
	if format != "" {
		if cause != nil {
			cause = errors.Wrapf(cause, format, args...)
		} else {
			cause = errors.Errorf(format, args...)
		}
	}
	return &Error{
		Code:       code,
		MessageKey: msgKey,
		Cause:      cause,
	}
}

// wrapCauseWithContext 在不影响对外国际化文案的前提下，为错误链补充业务上下文。
func wrapCauseWithContext(msgKey string, cause error, args []any) error {
	if cause == nil || len(args) == 0 {
		return errors.Tag(cause)
	}
	format, ok := args[0].(string)
	if !ok || strings.TrimSpace(format) == "" {
		return errors.Tag(cause)
	}
	if i18n.MessageTemplateHasArgs(msgKey) && !strings.Contains(format, "%") {
		return errors.Tag(cause)
	}
	if len(args) > 1 {
		return errors.Wrapf(cause, format, args[1:]...)
	}
	return errors.Wrap(cause, format)
}

// unwrapErrorArgs 对“仅内部错误上下文”的参数不再继续透传到对外文案，避免出现 `%!(EXTRA ...)`。
func unwrapErrorArgs(msgKey string, args []any) []any {
	if len(args) == 0 {
		return args
	}
	if i18n.MessageTemplateHasArgs(msgKey) {
		format, ok := args[0].(string)
		if !ok || strings.TrimSpace(format) == "" || !strings.Contains(format, "%") {
			return args
		}
		if len(args) == 1 {
			return []any{format}
		}
		return []any{fmt.Sprintf(format, args[1:]...)}
	}
	if format, ok := args[0].(string); ok && strings.TrimSpace(format) != "" {
		return nil
	}
	return args
}
