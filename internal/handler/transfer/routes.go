package transfer

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
)

// RouteSpecs 返回导入导出通用文件传输路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/api/file-transfer/upload/init", // 初始化断点续传上传会话。
			Access:      shared.RouteAccessAuth,
			Alias:       middleware.Ignore,
			Description: "初始化断点续传上传会话",
			Handler:     InitFileUploadHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/file-transfer/upload/status", // 查询断点续传上传状态。
			Access:      shared.RouteAccessAuth,
			Alias:       middleware.Ignore,
			Description: "查询断点续传上传状态",
			Handler:     GetFileUploadStatusHandler,
		},
		{
			Method:      http.MethodPut,
			Path:        "/api/file-transfer/upload/chunk", // 上传单个文件分片。
			Access:      shared.RouteAccessAuth,
			Alias:       middleware.Ignore,
			Description: "上传单个文件分片",
			Handler:     UploadFileChunkHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/file-transfer/download", // 下载当前管理员上传完成的文件。
			Access:      shared.RouteAccessAuth,
			Alias:       middleware.Ignore,
			Description: "下载当前管理员上传完成的文件",
			Handler:     DownloadUploadedFileHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/file-transfer/upload/complete", // 完成断点续传上传会话。
			Access:      shared.RouteAccessAuth,
			Alias:       middleware.Ignore,
			Description: "完成断点续传上传会话",
			Handler:     CompleteFileUploadHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/file-transfer/access", // 公开访问允许匿名预览的上传文件。
			Access:      shared.RouteAccessPublic,
			Description: "公开访问允许匿名预览的上传文件",
			Handler:     AccessUploadedFileHandler,
		},
	}
}
