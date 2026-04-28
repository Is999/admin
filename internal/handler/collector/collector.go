package collector

import (
	"admin/internal/handler/shared"
	"net/http"

	collectorlogic "admin/internal/logic/collector"
	"admin/internal/svc"
	"admin/internal/types"
)

// GetCollectorOverviewHandler 查询 Collector 概览。
func GetCollectorOverviewHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CollectorOverviewReq](shared.MethodGetCollectorOverview,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CollectorOverviewReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.Overview()
		},
	)(sCtx)
}

// ListCollectorTasksHandler 查询 Collector 任务列表。
func ListCollectorTasksHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListCollectorTasksReq](shared.MethodListCollectorTasks,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListCollectorTasksReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.ListTasks(req)
		},
	)(sCtx)
}

// RunCollectorHandler 手动触发执行一轮 Collector 任务。
func RunCollectorHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RunCollectorReq](shared.MethodRunCollector,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RunCollectorReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RunNow(req)
		},
	)(sCtx)
}

// RetryCollectorTasksHandler 对指定 Collector 任务发起人工重试/延迟重试。
func RetryCollectorTasksHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RetryCollectorTasksReq](shared.MethodRetryCollectorTasks,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RetryCollectorTasksReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RetryTasks(req)
		},
	)(sCtx)
}
