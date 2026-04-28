package docs

import (
	"net/http"

	sitedocs "admin/docs"
	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册 API 文档路由。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/docs/session",
		Handler: authMw.Handle(DocsSessionHandler(), shared.DocsSession.Alias),
	})
	// 文档路由单独挂载 JWT 鉴权，避免与主业务路由的鉴权链互相耦合。
	server.AddRoutes(
		rest.WithMiddleware(
			middleware.DocsJwtMiddleware(serverCtx),
			[]rest.Route{
				{
					Method:  http.MethodGet,
					Path:    "/api/docs", // 文档首页
					Handler: sitedocs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path", // 文档静态资源
					Handler: sitedocs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path/:sub", // 二级文档资源
					Handler: sitedocs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path/:sub/:file", // 三级文档资源
					Handler: sitedocs.Handler(),
				},
			}...,
		),
	)
}
