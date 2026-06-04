package middleware

import (
	"net/http"
	"runtime/debug"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// RecoverMiddleware 在最外层兜底捕获 panic，防止单个请求直接打崩服务进程。
type RecoverMiddleware struct{}

// NewRecoverMiddleware 创建 panic 保护中间件。
func NewRecoverMiddleware() *RecoverMiddleware {
	return &RecoverMiddleware{}
}

// Handle 在最外层兜底捕获 panic，保证请求不会直接崩溃，同时把异常信息写入统一日志与响应。
func (m *RecoverMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				ctx := r.Context()
				panicErr := errors.Errorf("请求 panic: %v", err)
				message := i18n.MessageByKey(i18n.MsgKeyInternalError, i18n.LocaleZHCN)
				requestctx.SetErrorResponse(ctx, http.StatusInternalServerError, codes.InternalError, message, panicErr, panicErr.Error())

				loggerx.Errorw(ctx, "请求 发生异常", panicErr, logx.Field("stacktrace", string(debug.Stack())))

				helper.NewJSONResp(ctx, w).
					SetHTTPStatus(http.StatusInternalServerError).
					SetCode(codes.InternalError).
					Fail(i18n.MsgKeyInternalError)
			}
		}()
		next(w, r)
	}
}
