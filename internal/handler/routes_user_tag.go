package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerUserTagRoutes 注册用户标签计算接口。
func registerUserTagRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// 公网入口继续走统一鉴权中间件，供后台手工触发和兼容既有外部调用链路。
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/user-tags/workflows", // 触发用户标签计算工作流
			Handler: authMw.Handle(TriggerUserTagWorkflowHandler(serverCtx), UserTagWorkflowTrigger.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/user-tags/recalculations", // 指定标签重算
			Handler: authMw.Handle(RecalculateUserTagHandler(serverCtx), UserTagRecalculate.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/user-tags/workflow-lease/release", // 手动释放用户标签工作流互斥锁
			Handler: authMw.Handle(ReleaseUserTagWorkflowLeaseHandler(serverCtx), UserTagWorkflowLeaseRelease.Alias),
		},
	})
}

// registerInternalUserTagRoutes 注册供 admin 等内网服务调用的免鉴权入口。
// 这些入口只允许内网 IP 访问，公网请求会被直接拒绝。
func registerInternalUserTagRoutes(server *rest.Server, serverCtx *svc.ServiceContext, internalMw *middleware.InternalOnlyMiddleware) {
	// 内网工作流触发入口，供 admin 按显式 UID 定向补算或手动触发工作流时复用。
	// 多维筛选 hash 不能作为用户标签计算来源。
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/internal/user-tags/workflows", // 内网触发用户标签工作流（免鉴权，IP 白名单）
			Handler: internalMw.Handle(TriggerUserTagWorkflowHandler(serverCtx), UserTagWorkflowTrigger.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/internal/user-tags/recalculations", // 内网指定标签重算
			Handler: internalMw.Handle(RecalculateUserTagHandler(serverCtx), UserTagRecalculate.Alias),
		},
	})
}
