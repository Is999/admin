package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerTaskRoutes 注册任务系统管理接口。
func registerTaskRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	// 任务中心接口统一挂在 `/api/tasks` 前缀下，保持路径命名与 RESTful 风格一致。
	// 这里按“任务投递、任务查询、工作流、队列控制、配置热加载”分组注册，便于后续维护。
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks", // 手动投递通用任务
			Handler: authMw.Handle(EnqueueTaskHandler(serverCtx), TaskEnqueue.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks", // 按状态查询任务列表
			Handler: authMw.Handle(ListTaskItemsHandler(serverCtx), TaskItemsList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/overview", // 任务总览聚合查询
			Handler: authMw.Handle(ListTaskItemsOverviewHandler(serverCtx), TaskItemsList.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks/workflows", // 手动触发工作流
			Handler: authMw.Handle(TriggerTaskWorkflowHandler(serverCtx), TaskWorkflowTrigger.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/workflows/:workflowId", // 查询工作流实例状态
			Handler: authMw.Handle(GetTaskWorkflowStatusHandler(serverCtx), TaskWorkflowStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/queues", // 查询任务队列与 worker 概览
			Handler: authMw.Handle(ListTaskQueuesHandler(serverCtx), TaskQueueList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/registry/task-types", // 查询已注册任务类型
			Handler: authMw.Handle(ListTaskRegistryTypesHandler(serverCtx), TaskQueueList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/registry/workflows", // 查询已注册工作流
			Handler: authMw.Handle(ListTaskRegistryWorkflowsHandler(serverCtx), TaskWorkflowStatus.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/config-reload", // 查询配置热加载运行状态
			Handler: authMw.Handle(GetConfigReloadStatusHandler(serverCtx), TaskConfigReload.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/config-reload/items", // 查询当前运行态配置项，返回值已脱敏
			Handler: authMw.Handle(GetConfigReloadItemsHandler(serverCtx), TaskConfigItems.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks/config-reload", // 手动触发配置热加载
			Handler: authMw.Handle(RunConfigReloadHandler(serverCtx), TaskConfigReloadRun.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/tasks/:taskId", // 查询单个任务详情；参数路由必须排在 overview/queues/registry/config-reload 等固定路由之后
			Handler: authMw.Handle(GetTaskInfoHandler(serverCtx), TaskInfoGet.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks/queues/pause/:queue", // 暂停队列消费
			Handler: authMw.Handle(PauseTaskQueueHandler(serverCtx), TaskQueuePause.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks/run/:taskId", // 让任务立即执行
			Handler: authMw.Handle(RunTaskHandler(serverCtx), TaskRun.Alias),
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/tasks/:taskId", // 删除任务
			Handler: authMw.Handle(DeleteTaskHandler(serverCtx), TaskDelete.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/tasks/queues/resume/:queue", // 恢复队列消费
			Handler: authMw.Handle(ResumeTaskQueueHandler(serverCtx), TaskQueueResume.Alias),
		},
	})
}
