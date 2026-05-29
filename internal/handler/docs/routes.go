package docs

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/svc"
)

// RouteSpecs 返回 API 文档路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/api/docs/session", // 创建文档访问会话。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.DocsSession,
			Description: shared.DocsSession.Describe,
			Handler:     docsSessionHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/docs", // 文档首页。
			Access:      shared.RouteAccessDocs,
			Description: "文档首页",
			Handler:     docsSiteHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/docs/:path", // 文档静态资源。
			Access:      shared.RouteAccessDocs,
			Description: "文档静态资源",
			Handler:     docsSiteHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/docs/:path/:sub", // 二级文档资源。
			Access:      shared.RouteAccessDocs,
			Description: "二级文档资源",
			Handler:     docsSiteHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/docs/:path/:sub/:file", // 三级文档资源。
			Access:      shared.RouteAccessDocs,
			Description: "三级文档资源",
			Handler:     docsSiteHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/docs/:path/:sub/:group/:file", // 四级文档资源。
			Access:      shared.RouteAccessDocs,
			Description: "四级文档资源",
			Handler:     docsSiteHandler,
		},
	}
}

// docsSessionHandler 返回文档访问会话处理器。
func docsSessionHandler(*svc.ServiceContext) http.HandlerFunc {
	return DocsSessionHandler()
}

// docsSiteHandler 返回文档站资源处理器。
func docsSiteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return DocsSiteHandler(svcCtx)
}
