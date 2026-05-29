package usertag

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回用户标签计算后台路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/api/user-tags/workflows", // 触发用户标签工作流。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserTagWorkflowTrigger,
			Description: shared.UserTagWorkflowTrigger.Describe,
			Handler:     TriggerUserTagWorkflowHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/user-tags/recalculations", // 指定标签重新计算。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserTagRecalculate,
			Description: shared.UserTagRecalculate.Describe,
			Handler:     RecalculateUserTagHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/user-tags/workflow-lease/release", // 释放用户标签工作流互斥锁。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserTagWorkflowLeaseRelease,
			Description: shared.UserTagWorkflowLeaseRelease.Describe,
			Handler:     ReleaseUserTagWorkflowLeaseHandler,
		},
	}
}

// InternalRouteSpecs 返回用户标签内网路由规格。
func InternalRouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/internal/user-tags/workflows", // 触发用户标签工作流。
			Access:      shared.RouteAccessInternal,
			Meta:        shared.UserTagWorkflowTrigger,
			Description: shared.UserTagWorkflowTrigger.Describe,
			Handler:     TriggerUserTagWorkflowHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/internal/user-tags/recalculations", // 指定标签重新计算。
			Access:      shared.RouteAccessInternal,
			Meta:        shared.UserTagRecalculate,
			Description: shared.UserTagRecalculate.Describe,
			Handler:     RecalculateUserTagHandler,
		},
	}
}
