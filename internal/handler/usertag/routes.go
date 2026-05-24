package usertag

import (
	"admin/internal/handler/shared"
	"net/http"

	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册用户标签计算接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// 公网入口继续走统一鉴权中间件，供后台手工触发和外部调用链路。
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回用户标签计算后台路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodPost, "/api/user-tags/workflows", shared.UserTagWorkflowTrigger, TriggerUserTagWorkflowHandler),
		shared.AuthRoute(http.MethodPost, "/api/user-tags/recalculations", shared.UserTagRecalculate, RecalculateUserTagHandler),
		shared.AuthRoute(http.MethodPost, "/api/user-tags/workflow-lease/release", shared.UserTagWorkflowLeaseRelease, ReleaseUserTagWorkflowLeaseHandler),
	}
}

// RegisterInternalRoutes 注册供 admin 等内网服务调用的免鉴权入口。
// 这些入口只允许内网 IP 访问，公网请求会被直接拒绝。
func RegisterInternalRoutes(server *rest.Server, serverCtx *svc.ServiceContext, internalMw *middleware.InternalOnlyMiddleware) {
	// 内网工作流触发入口，供 admin 按显式 UID 定向补算或手动触发工作流时复用。
	// 多维筛选 hash 不能作为用户标签计算来源。
	shared.AddRouteSpecs(server, serverCtx, nil, internalMw, InternalRouteSpecs())
}

// InternalRouteSpecs 返回用户标签内网路由规格。
func InternalRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.InternalRoute(http.MethodPost, "/internal/user-tags/workflows", shared.UserTagWorkflowTrigger, TriggerUserTagWorkflowHandler),
		shared.InternalRoute(http.MethodPost, "/internal/user-tags/recalculations", shared.UserTagRecalculate, RecalculateUserTagHandler),
	}
}
