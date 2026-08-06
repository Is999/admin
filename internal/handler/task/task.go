package task

import (
	"admin/internal/handler/shared"
	"encoding/json"
	"io"
	"net/http"

	codes "admin/common/codes"
	tasklogic "admin/internal/logic/task"
	"admin/internal/svc"
	tasklimits "admin/internal/task/limits"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

const (
	// enqueueTaskEnvelopeBytes 为任务类型、调度参数和 JSON 结构预留请求体空间。
	enqueueTaskEnvelopeBytes = 64 << 10
	// enqueueTaskBodyMaxBytes 限制手工投递请求体，防止解析层绕过任务负载硬上限。
	enqueueTaskBodyMaxBytes = tasklimits.MaxPayloadBytes + enqueueTaskEnvelopeBytes
	// triggerWorkflowBodyMaxBytes 覆盖控制字符最坏六倍 JSON 转义，解码后仍由目标总字节上限约束。
	triggerWorkflowBodyMaxBytes = tasklimits.MaxWorkflowTargetsBytes*6 + enqueueTaskEnvelopeBytes
)

// TriggerTaskWorkflowHandler 手动触发工作流执行。
func TriggerTaskWorkflowHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.TaskWorkflowTrigger, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		var req types.TriggerTaskWorkflowReq
		if err := parseTaskJSONReq(r, &req, triggerWorkflowBodyMaxBytes); err != nil {
			return nil, types.ParamErrorResult(err)
		}
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		resp := logicObj.TriggerWorkflow(&req)
		if resp == nil {
			return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
		}
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// EnqueueTaskHandler 手动投递一个已注册的通用任务。
func EnqueueTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.TaskEnqueue, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		var req types.EnqueueTaskReq
		if err := parseEnqueueTaskReq(r, &req); err != nil {
			return nil, types.ParamErrorResult(err)
		}
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
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
	return parseTaskJSONReq(r, req, enqueueTaskBodyMaxBytes)
}

// parseTaskJSONReq 有界解析单份任务 JSON，并执行请求自己的业务校验。
func parseTaskJSONReq[T interface{ Validate() error }](r *http.Request, req T, maxBytes int64) error {
	if r == nil {
		return errors.Errorf("请求不能为空")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return errors.Wrap(err, "读取任务请求失败")
	}
	if int64(len(raw)) > maxBytes {
		return errors.Errorf("任务请求体不能超过 %d 字节", maxBytes)
	}
	if err = json.Unmarshal(raw, req); err != nil {
		return errors.Wrap(err, "解析任务请求失败")
	}
	return req.Validate()
}

// GetTaskWorkflowStatusHandler 查询工作流实例状态。
func GetTaskWorkflowStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.GetTaskWorkflowReq](shared.TaskWorkflowStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.GetTaskWorkflowReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetWorkflowStatus(req)
		},
	)(sCtx)
}

// ListTaskWorkflowsHandler 查询短期工作流历史。
func ListTaskWorkflowsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListTaskWorkflowsReq](shared.TaskWorkflowStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskWorkflowsReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTaskWorkflows(req)
		},
	)(sCtx)
}

// ListTaskRunsHandler 查询全部实际任务的短期终态历史。
func ListTaskRunsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListTaskRunsReq](shared.TaskItemsList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskRunsReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTaskRuns(req)
		},
	)(sCtx)
}

// GetTaskRunHistoryHandler 查询任务终态历史详情。
func GetTaskRunHistoryHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.GetTaskRunHistoryReq](shared.TaskItemsList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.GetTaskRunHistoryReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetTaskRunHistory(req)
		},
	)(sCtx)
}

// ListTaskFailuresHandler 查询最终失败任务历史。
func ListTaskFailuresHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListTaskFailuresReq](shared.TaskItemsList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskFailuresReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTaskFailures(req)
		},
	)(sCtx)
}

// TaskObservabilityHandler 查询 Redis 实时态和历史落库健康摘要。
func TaskObservabilityHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.TaskObservability()
	})
}

// GetTaskInfoHandler 查询单个任务详情。
func GetTaskInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.GetTaskInfoReq](shared.TaskInfoGet,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.GetTaskInfoReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetTaskInfo(req)
		},
	)(sCtx)
}

// ListTaskItemsHandler 按队列和状态查询任务列表。
func ListTaskItemsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListTaskItemsReq](shared.TaskItemsList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskItemsReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTasks(req)
		},
	)(sCtx)
}

// ListTaskItemsOverviewHandler 按“总览聚合 + 按需查询”模式查询任务列表。
func ListTaskItemsOverviewHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListTaskItemsOverviewReq](shared.TaskItemsList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListTaskItemsOverviewReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ListTasksOverview(req)
		},
	)(sCtx)
}

// ListTaskQueuesHandler 查询任务队列与 worker 概览。
func ListTaskQueuesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口需要记录管理员查看任务队列的审计日志，因此走 actionLogHandler。
	return shared.ActionLogHandler(shared.TaskQueueList, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListQueues().WithReq(shared.ActionReq("list_task_queues"))
	})
}

// ListTaskRegistryTypesHandler 查询当前已注册的任务类型清单。
func ListTaskRegistryTypesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口无请求体，也不要求管理员审计日志，因此直接走 respHandler 保持最小包装。
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListRegisteredTaskTypes()
	})
}

// ListTaskRegistryWorkflowsHandler 查询当前已注册的工作流清单。
func ListTaskRegistryWorkflowsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口无请求体，也不要求管理员审计日志，因此直接走 respHandler 保持最小包装。
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.ListRegisteredWorkflows()
	})
}

// GetConfigReloadStatusHandler 查询 config.yaml 热加载运行状态。
func GetConfigReloadStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 该接口属于后台运维能力，需要保留审计日志，因此统一走 actionLogHandler。
	return shared.ActionLogHandler(shared.TaskConfigReload, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.GetConfigReloadStatus().WithReq(shared.ActionReq("get_config_reload_status"))
	})
}

// GetConfigReloadItemsHandler 查询当前运行态配置快照中的配置项。
func GetConfigReloadItemsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.TaskConfigItemQueryReq](shared.TaskConfigItems,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.TaskConfigItemQueryReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.GetConfigReloadItems(req)
		},
	)(sCtx)
}

// RunConfigReloadHandler 手动触发一次 config.yaml 重载，并返回最新运行状态。
func RunConfigReloadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	// 手动执行配置重载属于显式运维操作，需要统一落管理员审计日志。
	return shared.ActionLogHandler(shared.TaskConfigReloadRun, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := tasklogic.NewTaskLogic(r, sCtx)
		return logicObj, logicObj.RunConfigReload().WithReq(shared.ActionReq("run_config_reload"))
	})
}

// RunTaskHandler 让指定任务立即转为 pending 状态执行。
func RunTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.OperateTaskReq](shared.TaskRun,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.RunTask(req)
		},
	)(sCtx)
}

// DeleteTaskHandler 删除指定任务。
func DeleteTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.OperateTaskReq](shared.TaskDelete,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.DeleteTask(req)
		},
	)(sCtx)
}

// PauseTaskQueueHandler 暂停指定队列消费。
func PauseTaskQueueHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.OperateTaskQueueReq](shared.TaskQueuePause,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskQueueReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.PauseQueue(req)
		},
	)(sCtx)
}

// ResumeTaskQueueHandler 恢复指定队列消费。
func ResumeTaskQueueHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.OperateTaskQueueReq](shared.TaskQueueResume,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.OperateTaskQueueReq) (shared.LogicObj, *types.BizResult) {
			logicObj := tasklogic.NewTaskLogic(r, svcCtx)
			return logicObj, logicObj.ResumeQueue(req)
		},
	)(sCtx)
}
