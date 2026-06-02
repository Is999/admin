package task

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回任务系统管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks", // 手动投递任务。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskEnqueue,
			Description: shared.TaskEnqueue.Describe,
			Handler:     EnqueueTaskHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks", // 查询任务列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskItemsList,
			Description: shared.TaskItemsList.Describe,
			Handler:     ListTaskItemsHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/overview", // 查询任务列表概览。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskItemsList,
			Description: shared.TaskItemsList.Describe,
			Handler:     ListTaskItemsOverviewHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks/workflows", // 手动触发工作流。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskWorkflowTrigger,
			Description: shared.TaskWorkflowTrigger.Describe,
			Handler:     TriggerTaskWorkflowHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/workflows/:workflowId", // 查询工作流状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskWorkflowStatus,
			Description: shared.TaskWorkflowStatus.Describe,
			Handler:     GetTaskWorkflowStatusHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/queues", // 查询任务队列概览。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskQueueList,
			Description: shared.TaskQueueList.Describe,
			Handler:     ListTaskQueuesHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/registry/task-types", // 查询已注册任务类型。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskQueueList,
			Description: shared.TaskQueueList.Describe,
			Handler:     ListTaskRegistryTypesHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/registry/workflows", // 查询已注册工作流。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskWorkflowStatus,
			Description: shared.TaskWorkflowStatus.Describe,
			Handler:     ListTaskRegistryWorkflowsHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/config-reload", // 查询配置热加载状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskConfigReload,
			Description: shared.TaskConfigReload.Describe,
			Handler:     GetConfigReloadStatusHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/config-reload/items", // 查询配置热加载配置项。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskConfigItems,
			Description: shared.TaskConfigItems.Describe,
			Handler:     GetConfigReloadItemsHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks/config-reload", // 手动触发配置热加载。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskConfigReloadRun,
			Description: shared.TaskConfigReloadRun.Describe,
			Handler:     RunConfigReloadHandler,
		},
		// 单段固定路由必须排在 :taskId 参数路由前，避免 go-zero 优先匹配参数路由。
		{
			Method:      http.MethodGet,
			Path:        "/api/tasks/:taskId", // 查询任务详情。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskInfoGet,
			Description: shared.TaskInfoGet.Describe,
			Handler:     GetTaskInfoHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks/queues/pause/:queue", // 暂停任务队列。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskQueuePause,
			Description: shared.TaskQueuePause.Describe,
			Handler:     PauseTaskQueueHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks/run/:taskId", // 立即执行任务。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskRun,
			Description: shared.TaskRun.Describe,
			Handler:     RunTaskHandler,
		},
		{
			Method:      http.MethodDelete,
			Path:        "/api/tasks/:taskId", // 删除任务。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskDelete,
			Description: shared.TaskDelete.Describe,
			Handler:     DeleteTaskHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/tasks/queues/resume/:queue", // 恢复任务队列。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.TaskQueueResume,
			Description: shared.TaskQueueResume.Describe,
			Handler:     ResumeTaskQueueHandler,
		},
	}
}
