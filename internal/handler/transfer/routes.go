package transfer

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册导入导出通用文件传输接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回导入导出通用文件传输路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.PlainAuthRoute(http.MethodPost, "/api/file-transfer/upload/init", middleware.Ignore, "初始化断点续传上传会话", InitFileUploadHandler),
		shared.PlainAuthRoute(http.MethodGet, "/api/file-transfer/upload/status", middleware.Ignore, "查询断点续传上传状态", GetFileUploadStatusHandler),
		shared.PlainAuthRoute(http.MethodPut, "/api/file-transfer/upload/chunk", middleware.Ignore, "上传单个文件分片", UploadFileChunkHandler),
		shared.PlainAuthRoute(http.MethodGet, "/api/file-transfer/download", middleware.Ignore, "下载当前管理员上传完成的文件", DownloadUploadedFileHandler),
		shared.PlainAuthRoute(http.MethodPost, "/api/file-transfer/upload/complete", middleware.Ignore, "完成断点续传上传会话", CompleteFileUploadHandler),
		shared.PlainPublicRoute(http.MethodGet, "/api/file-transfer/access", "公开访问允许匿名预览的上传文件", AccessUploadedFileHandler),
	}
}
