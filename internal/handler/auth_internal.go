package handler

import (
	"net/http"

	"admin_cron/internal/logic"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
)

// InitAdminBootstrapHandler 处理仅内网可调用的管理员自举初始化请求。
func InitAdminBootstrapHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler(func(r *http.Request, svcCtx *svc.ServiceContext, req *types.InitAdminBootstrapReq) (LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(InternalInitAdminBootstrap.Alias))
		logicObj := logic.NewAdminBootstrapLogic(r.Context(), svcCtx)
		return logicObj, logicObj.InitAdminBootstrap(req)
	})(sCtx)
}
