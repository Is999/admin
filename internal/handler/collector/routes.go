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
			Path:        "/api/collector/overview", // 查询Collector观测概览。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorOverview,
			Description: shared.CollectorOverview.Describe,
			Handler:     GetCollectorOverviewHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/collector/failures", // 查询Collector失败事件。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorFailureList,
			Description: shared.CollectorFailureList.Describe,
			Handler:     ListCollectorFailuresHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/collector/failures/run", // 手动执行Collector失败重试。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorFailureRun,
			Description: shared.CollectorFailureRun.Describe,
			Handler:     RunCollectorHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/collector/failures/retry", // 手动重试Collector失败事件。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.CollectorFailureRetry,
			Description: shared.CollectorFailureRetry.Describe,
			Handler:     RetryCollectorFailuresHandler,
		},
	}
}
