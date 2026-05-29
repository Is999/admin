package collector

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回通用收集器管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/collector/overview", // 查询Collector概览。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorOverview,
			Description: shared.CollectorOverview.Describe,
			Handler:     GetCollectorOverviewHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/collector/tasks", // 查询Collector任务。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorTaskList,
			Description: shared.CollectorTaskList.Describe,
			Handler:     ListCollectorTasksHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/collector/run", // 手动执行Collector。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorRun,
			Description: shared.CollectorRun.Describe,
			Handler:     RunCollectorHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/collector/tasks/retry", // 手动重试Collector任务。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorRetry,
			Description: shared.CollectorRetry.Describe,
			Handler:     RetryCollectorTasksHandler,
		},
	}
}
