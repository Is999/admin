package usertag

import (
	"admin/internal/handler/shared"
	"net/http"

	usertaglogic "admin/internal/logic/usertag"
	"admin/internal/svc"
	"admin/internal/types"
)

// TriggerUserTagWorkflowHandler 触发用户标签计算工作流。
func TriggerUserTagWorkflowHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.TriggerUserTagWorkflowReq](shared.UserTagWorkflowTrigger,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.TriggerUserTagWorkflowReq) (shared.LogicObj, *types.BizResult) {
			logicObj := usertaglogic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.TriggerWorkflow(req)
		},
	)(sCtx)
}

// RecalculateUserTagHandler 后台指定标签重算接口。
func RecalculateUserTagHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RecalculateUserTagReq](shared.UserTagRecalculate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RecalculateUserTagReq) (shared.LogicObj, *types.BizResult) {
			logicObj := usertaglogic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.RecalculateByTagTypes(req)
		},
	)(sCtx)
}

// ReleaseUserTagWorkflowLeaseHandler 手动释放用户标签工作流互斥租约。
func ReleaseUserTagWorkflowLeaseHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ReleaseUserTagWorkflowLeaseReq](shared.UserTagWorkflowLeaseRelease,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ReleaseUserTagWorkflowLeaseReq) (shared.LogicObj, *types.BizResult) {
			logicObj := usertaglogic.NewUserTagLogic(r, svcCtx)
			return logicObj, logicObj.ReleaseWorkflowLease(req)
		},
	)(sCtx)
}
