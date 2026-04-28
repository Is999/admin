package collector

import (
	"admin/internal/handler/shared"
	"net/http"

	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册通用收集器管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/collector/overview", // 查询 Collector 概览与性能观察数据
			Handler: authMw.Handle(GetCollectorOverviewHandler(serverCtx), shared.CollectorOverview.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/collector/tasks", // 查询 Collector 任务列表
			Handler: authMw.Handle(ListCollectorTasksHandler(serverCtx), shared.CollectorTaskList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/collector/run", // 手动触发执行一轮 Collector 任务
			Handler: authMw.Handle(RunCollectorHandler(serverCtx), shared.CollectorRun.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/collector/tasks/retry", // 手动重试/延迟重试 Collector 任务
			Handler: authMw.Handle(RetryCollectorTasksHandler(serverCtx), shared.CollectorRetry.Alias),
		},
	})
}
