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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回 API 文档路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodPost, "/api/docs/session", shared.DocsSession, docsSessionHandler),
		shared.DocsRoute(http.MethodGet, "/api/docs", "文档首页", docsSiteHandler),
		shared.DocsRoute(http.MethodGet, "/api/docs/:path", "文档静态资源", docsSiteHandler),
		shared.DocsRoute(http.MethodGet, "/api/docs/:path/:sub", "二级文档资源", docsSiteHandler),
		shared.DocsRoute(http.MethodGet, "/api/docs/:path/:sub/:file", "三级文档资源", docsSiteHandler),
	}
}

// docsSessionHandler 返回文档访问会话处理器。
func docsSessionHandler(*svc.ServiceContext) http.HandlerFunc {
	return DocsSessionHandler()
}

// docsSiteHandler 返回文档站静态资源处理器。
func docsSiteHandler(*svc.ServiceContext) http.HandlerFunc {
	return sitedocs.Handler()
}
