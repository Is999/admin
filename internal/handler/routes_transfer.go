package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerTransferRoutes 注册导入导出通用文件传输接口。
func registerTransferRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/file-transfer/upload/init", // 初始化断点续传上传会话
			Handler: authMw.Handle(InitFileUploadHandler(serverCtx), middleware.Ignore),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/file-transfer/upload/status", // 查询断点续传上传状态
			Handler: authMw.Handle(GetFileUploadStatusHandler(serverCtx), middleware.Ignore),
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/file-transfer/upload/chunk", // 上传单个文件分片
			Handler: authMw.Handle(UploadFileChunkHandler(serverCtx), middleware.Ignore),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/file-transfer/download", // 下载当前管理员上传完成的文件
			Handler: authMw.Handle(DownloadUploadedFileHandler(serverCtx), middleware.Ignore),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/file-transfer/upload/complete", // 完成断点续传上传会话
			Handler: authMw.Handle(CompleteFileUploadHandler(serverCtx), middleware.Ignore),
		},
	})
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/file-transfer/access", // 公开访问允许匿名预览的上传文件
			Handler: AccessUploadedFileHandler(serverCtx),
		},
	})
}
