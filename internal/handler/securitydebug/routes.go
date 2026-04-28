package securitydebug

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册安全调试接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/sign", // 模拟请求或响应参数签名
			Handler: authMw.Handle(SecurityDebugSignHandler(serverCtx), shared.SecurityDebugSign.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/verify", // 模拟请求或响应参数验签
			Handler: authMw.Handle(SecurityDebugVerifyHandler(serverCtx), shared.SecurityDebugVerify.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/encrypt", // 模拟请求或响应参数加密
			Handler: authMw.Handle(SecurityDebugEncryptHandler(serverCtx), shared.SecurityDebugEncrypt.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/security/debug/decrypt", // 模拟请求或响应参数解密
			Handler: authMw.Handle(SecurityDebugDecryptHandler(serverCtx), shared.SecurityDebugDecrypt.Alias),
		},
	})
}
