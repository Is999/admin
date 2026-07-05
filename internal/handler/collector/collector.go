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
	return shared.ActionHandler[types.CollectorOverviewReq](shared.CollectorOverview,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CollectorOverviewReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.Overview()
		},
	)(sCtx)
}

// ListCollectorFailuresHandler 查询 Collector 失败事件列表。
func ListCollectorFailuresHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ListCollectorFailuresReq](shared.CollectorFailureList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ListCollectorFailuresReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.ListFailures(req)
		},
	)(sCtx)
}

// RunCollectorHandler 手动触发执行一轮 Collector 失败重试。
func RunCollectorHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RunCollectorReq](shared.CollectorFailureRun,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RunCollectorReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RunNow(req)
		},
	)(sCtx)
}

// RetryCollectorFailuresHandler 对指定 Collector 失败事件发起人工重试/延迟重试。
func RetryCollectorFailuresHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RetryCollectorFailuresReq](shared.CollectorFailureRetry,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RetryCollectorFailuresReq) (shared.LogicObj, *types.BizResult) {
			logicObj := collectorlogic.NewCollectorLogic(r, svcCtx)
			return logicObj, logicObj.RetryFailures(req)
		},
	)(sCtx)
}
