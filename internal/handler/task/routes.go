package task

import (
	"admin/internal/handler/shared"
	"net/http"

	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册任务系统管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// 任务中心接口统一挂在 `/api/tasks` 前缀下，保持路径命名与 RESTful 风格一致。
	// 这里按“任务投递、任务查询、工作流、队列控制、配置热加载”分组注册，便于后续维护。
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回任务系统管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodPost, "/api/tasks", shared.TaskEnqueue, EnqueueTaskHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks", shared.TaskItemsList, ListTaskItemsHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/overview", shared.TaskItemsList, ListTaskItemsOverviewHandler),
		shared.AuthRoute(http.MethodPost, "/api/tasks/workflows", shared.TaskWorkflowTrigger, TriggerTaskWorkflowHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/workflows/:workflowId", shared.TaskWorkflowStatus, GetTaskWorkflowStatusHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/queues", shared.TaskQueueList, ListTaskQueuesHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/registry/task-types", shared.TaskQueueList, ListTaskRegistryTypesHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/registry/workflows", shared.TaskWorkflowStatus, ListTaskRegistryWorkflowsHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/config-reload", shared.TaskConfigReload, GetConfigReloadStatusHandler),
		shared.AuthRoute(http.MethodGet, "/api/tasks/config-reload/items", shared.TaskConfigItems, GetConfigReloadItemsHandler),
		shared.AuthRoute(http.MethodPost, "/api/tasks/config-reload", shared.TaskConfigReloadRun, RunConfigReloadHandler),
		// 固定路由必须排在 :taskId 参数路由前，避免 go-zero 优先匹配参数路由。
		shared.AuthRoute(http.MethodGet, "/api/tasks/:taskId", shared.TaskInfoGet, GetTaskInfoHandler),
		shared.AuthRoute(http.MethodPost, "/api/tasks/queues/pause/:queue", shared.TaskQueuePause, PauseTaskQueueHandler),
		shared.AuthRoute(http.MethodPost, "/api/tasks/run/:taskId", shared.TaskRun, RunTaskHandler),
		shared.AuthRoute(http.MethodDelete, "/api/tasks/:taskId", shared.TaskDelete, DeleteTaskHandler),
		shared.AuthRoute(http.MethodPost, "/api/tasks/queues/resume/:queue", shared.TaskQueueResume, ResumeTaskQueueHandler),
	}
}
