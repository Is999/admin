package middleware

import (
	"net/http"

	"admin/internal/requestctx"
	"admin/internal/svc"

	utils "github.com/Is999/go-utils"
)

// requestClientIP 按启动期可信代理白名单解析客户端 IP。
func requestClientIP(svcCtx *svc.ServiceContext, r *http.Request) string {
	if svcCtx != nil {
		return svcCtx.ClientIP(r)
	}
	return requestctx.NormalizeClientIP(utils.ClientIP(r))
}
