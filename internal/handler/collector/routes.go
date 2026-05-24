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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回通用收集器管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/collector/overview", shared.CollectorOverview, GetCollectorOverviewHandler),
		shared.AuthRoute(http.MethodGet, "/api/collector/tasks", shared.CollectorTaskList, ListCollectorTasksHandler),
		shared.AuthRoute(http.MethodPost, "/api/collector/run", shared.CollectorRun, RunCollectorHandler),
		shared.AuthRoute(http.MethodPost, "/api/collector/tasks/retry", shared.CollectorRetry, RetryCollectorTasksHandler),
	}
}
