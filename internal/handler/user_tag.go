package handler

import (
	"net/http"

	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
)

// TriggerUserTagWorkflowHandler 触发用户标签计算工作流。
func TriggerUserTagWorkflowHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.TriggerUserTagWorkflowReq](triggerUserTagWorkflow,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.TriggerUserTagWorkflowReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.TriggerWorkflow(req)
		},
	)(sCtx)
}

// RecalculateUserTagHandler 兼容 admin 指定标签重算接口。
func RecalculateUserTagHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RecalculateUserTagReq](recalculateUserTag,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RecalculateUserTagReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.RecalculateByTagTypes(req)
		},
	)(sCtx)
}

// ReleaseUserTagWorkflowLeaseHandler 手动释放用户标签工作流互斥租约。
func ReleaseUserTagWorkflowLeaseHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ReleaseUserTagWorkflowLeaseReq](releaseUserTagWorkflowLease,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ReleaseUserTagWorkflowLeaseReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.ReleaseWorkflowLease(req)
		},
	)(sCtx)
}
