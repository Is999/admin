package health

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册基础健康检查路由。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext) {
	shared.AddRouteSpecs(server, serverCtx, nil, nil, RouteSpecs())
}

// RouteSpecs 返回基础健康检查路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.HealthRoute(http.MethodGet, "/api/live", "存活检查", LiveHandler),
		shared.HealthRoute(http.MethodGet, "/api/ready", "就绪检查", ReadyHandler),
		shared.HealthRoute(http.MethodGet, "/api/metrics", "Prometheus 指标抓取", metricsHandler),
	}
}

// metricsHandler 返回 Prometheus 指标处理器。
func metricsHandler(*svc.ServiceContext) http.HandlerFunc {
	return MetricsHandler()
}
