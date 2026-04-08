package handler

import (
	"net/http"

	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerHealthRoutes 注册基础健康检查路由。
func registerHealthRoutes(server *rest.Server, serverCtx *svc.ServiceContext) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/health", // 兼容健康检查，等同于 /api/live
			Handler: HealthHandler(serverCtx),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/live", // 存活检查，不访问外部依赖
			Handler: LiveHandler(serverCtx),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/ready", // 就绪检查，探测 MySQL/Redis/任务/Collector 等关键依赖
			Handler: ReadyHandler(serverCtx),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/metrics", // Prometheus 指标抓取入口
			Handler: MetricsHandler(),
		},
	})
}
