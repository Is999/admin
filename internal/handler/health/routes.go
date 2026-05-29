package health

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/svc"
)

// RouteSpecs 返回基础健康检查路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:        http.MethodGet,
			Path:          "/api/live", // 存活检查。
			Access:        shared.RouteAccessHealth,
			Description:   "存活检查",
			SkipAccessLog: true,
			Handler:       LiveHandler,
		},
		{
			Method:        http.MethodGet,
			Path:          "/api/ready", // 就绪检查。
			Access:        shared.RouteAccessHealth,
			Description:   "就绪检查",
			SkipAccessLog: true,
			Handler:       ReadyHandler,
		},
		{
			Method:        http.MethodGet,
			Path:          "/api/metrics", // Prometheus 指标抓取。
			Access:        shared.RouteAccessHealth,
			Description:   "Prometheus 指标抓取",
			SkipAccessLog: true,
			Handler:       metricsHandler,
		},
	}
}

// metricsHandler 返回 Prometheus 指标处理器。
func metricsHandler(*svc.ServiceContext) http.HandlerFunc {
	return MetricsHandler()
}
