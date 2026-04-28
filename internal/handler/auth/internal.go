package auth

import (
	"admin/internal/handler/shared"
	"net/http"

	adminlogic "admin/internal/logic/admin"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/types"
)

// InitAdminBootstrapHandler 处理仅内网可调用的管理员自举初始化请求。
func InitAdminBootstrapHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler(func(r *http.Request, svcCtx *svc.ServiceContext, req *types.InitAdminBootstrapReq) (shared.LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(shared.InternalInitAdminBootstrap.Alias))
		logicObj := adminlogic.NewAdminBootstrapLogic(r.Context(), svcCtx)
		return logicObj, logicObj.InitAdminBootstrap(req)
	})(sCtx)
}
