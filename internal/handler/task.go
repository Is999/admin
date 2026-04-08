package handler

import (
	"encoding/json"
	"io"
	"net/http"

	codes "admin_cron/common/codes"
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
)

const (
	// enqueueTaskBodyMaxBytes 限制手工投递请求体大小，保持和 go-zero 默认 JSON 解析上限一致。
	enqueueTaskBodyMaxBytes = 8 << 20
)

// TriggerTaskWorkflowHandler 手动触发工作流执行。
func TriggerTaskWorkflowHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.TriggerTaskWorkflowReq](triggerTaskWorkflow,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.TriggerTaskWorkflowReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.TriggerWorkflow(req)
		},
	)(sCtx)
}

// EnqueueTaskHandler 手动投递一个已注册的通用任务。
func EnqueueTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(enqueueTask, func(r *http.Request) (LogicObj, *types.BizResult) {
		var req types.EnqueueTaskReq
		if err := parseEnqueueTaskReq(r, &req); err != nil {
			return nil, paramErrorResult(codes.ParamError, err)
		}
		logicObj := logic.NewTaskLogic(r, sCtx)
		resp := logicObj.EnqueueTask(&req)
		if resp == nil {
			return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
		}
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// parseEnqueueTaskReq 使用标准 JSON 解析任务负载，保留 payload 原始结构给 Asynq 处理器。
func parseEnqueueTaskReq(r *http.Request, req *types.EnqueueTaskReq) error {
	if r == nil || req == nil {
		return errors.Errorf("请求不能为空")
	}
	reader := io.LimitReader(r.Body, enqueueTaskBodyMaxBytes)
	if err := json.NewDecoder(reader).Decode(req); err != nil {
		return errors.Wrap(err, "解析任务投递请求失败")
	}
	return req.Validate()
}

// GetTaskWorkflowStatusHandler 查询工作流实例状态。
func GetTaskWorkflowStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.GetTaskWorkflowReq](getTaskWorkflowStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.GetTaskWorkflowReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetWorkflowStatus(req)
		},
	)(sCtx)
}

// GetTaskInfoHandler 查询单个任务详情。
func GetTaskInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.GetTaskInfoReq](getTaskInfo,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.GetTaskInfoReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetTaskInfo(req)
		},
	)(sCtx)
}

// ListTaskItemsHandler 按队列和状态查询任务列表。
func ListTaskItemsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ListTaskItemsReq](listTaskItems,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskItemsReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTasks(req)
		},
	)(sCtx)
}

// ListTaskItemsOverviewHandler 按“总览聚合 + 按需查询”模式查询任务列表。
func ListTaskItemsOverviewHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ListTaskItemsOverviewReq](listTaskItems,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskItemsOverviewReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTasksOverview(req)
		},
	)(sCtx)
}

// ListTaskQueuesHandler 查询任务队列与 worker 概览。
func ListTaskQueuesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口需要记录管理员查看任务队列的审计日志，因此走 actionLogHandler。
	return actionLogHandler(listTaskQueues, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListQueues().WithReq(map[string]any{"action": "list_task_queues"})
	})
}

// ListTaskRegistryTypesHandler 查询当前已注册的任务类型清单。
func ListTaskRegistryTypesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口无请求体，也不要求管理员审计日志，因此直接走 respHandler 保持最小包装。
	return respHandler(func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListRegisteredTaskTypes()
	})
}

// ListTaskRegistryWorkflowsHandler 查询当前已注册的工作流清单。
func ListTaskRegistryWorkflowsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口无请求体，也不要求管理员审计日志，因此直接走 respHandler 保持最小包装。
	return respHandler(func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListRegisteredWorkflows()
	})
}

// GetConfigReloadStatusHandler 查询 config.yaml 热加载运行状态。
func GetConfigReloadStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口属于后台运维能力，需要保留审计日志，因此统一走 actionLogHandler。
	return actionLogHandler(getConfigReloadStatus, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.GetConfigReloadStatus().WithReq(map[string]any{"action": "get_config_reload_status"})
	})
}

// GetConfigReloadItemsHandler 查询当前运行态配置快照中的配置项。
func GetConfigReloadItemsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.TaskConfigItemQueryReq](getConfigReloadItems,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.TaskConfigItemQueryReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetConfigReloadItems(req)
		},
	)(sCtx)
}

// RunConfigReloadHandler 手动触发一次 config.yaml 重载，并返回最新运行状态。
func RunConfigReloadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 手动执行配置重载属于显式运维操作，需要统一落管理员审计日志。
	return actionLogHandler(runConfigReload, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.RunConfigReload().WithReq(map[string]any{"action": "run_config_reload"})
	})
}

// RunTaskHandler 让指定任务立即转为 pending 状态执行。
func RunTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.OperateTaskReq](runTask,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.RunTask(req)
		},
	)(sCtx)
}

// DeleteTaskHandler 删除指定任务。
func DeleteTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.OperateTaskReq](deleteTask,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.DeleteTask(req)
		},
	)(sCtx)
}

// PauseTaskQueueHandler 暂停指定队列消费。
func PauseTaskQueueHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.OperateTaskQueueReq](pauseTaskQueue,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskQueueReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.PauseQueue(req)
		},
	)(sCtx)
}

// ResumeTaskQueueHandler 恢复指定队列消费。
func ResumeTaskQueueHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.OperateTaskQueueReq](resumeTaskQueue,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskQueueReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ResumeQueue(req)
		},
	)(sCtx)
}
