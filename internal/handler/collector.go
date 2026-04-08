package handler

import (
	"net/http"

	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
)

// GetCollectorOverviewHandler 查询 Collector 概览。
func GetCollectorOverviewHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CollectorOverviewReq](getCollectorOverview,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CollectorOverviewReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.Overview()
		},
	)(sCtx)
}

// ListCollectorTasksHandler 查询 Collector 任务列表。
func ListCollectorTasksHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ListCollectorTasksReq](listCollectorTasks,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListCollectorTasksReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.ListTasks(req)
		},
	)(sCtx)
}

// RunCollectorHandler 手动触发执行一轮 Collector 任务。
func RunCollectorHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RunCollectorReq](runCollector,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RunCollectorReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RunNow(req)
		},
	)(sCtx)
}

// RetryCollectorTasksHandler 对指定 Collector 任务发起人工重试/延迟重试。
func RetryCollectorTasksHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RetryCollectorTasksReq](retryCollectorTasks,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RetryCollectorTasksReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RetryTasks(req)
		},
	)(sCtx)
}
