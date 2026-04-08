package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerCollectorRoutes 注册通用收集器管理接口。
func registerCollectorRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/collector/overview", // 查询 Collector 概览与性能观察数据
			Handler: authMw.Handle(GetCollectorOverviewHandler(serverCtx), CollectorOverview.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/collector/tasks", // 查询 Collector 任务列表
			Handler: authMw.Handle(ListCollectorTasksHandler(serverCtx), CollectorTaskList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/collector/run", // 手动触发执行一轮 Collector 任务
			Handler: authMw.Handle(RunCollectorHandler(serverCtx), CollectorRun.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/collector/tasks/retry", // 手动重试/延迟重试 Collector 任务
			Handler: authMw.Handle(RetryCollectorTasksHandler(serverCtx), CollectorRetry.Alias),
		},
	})
}
