package health

import (
	"net/http"

	codes "admin/common/codes"
	"admin/helper"
	"admin/internal/infra/loggerx"
	healthlogic "admin/internal/logic/health"
	"admin/internal/svc"
)

// LiveHandler 提供进程存活检查，不访问外部依赖，供 livenessProbe 使用。
func LiveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthlogic.NewHealthLogic(r.Context(), svcCtx).Liveness()
		helper.NewJSONResp(r.Context(), w).SetCode(codes.OK).Success(resp)
	}
}

// ReadyHandler 提供依赖就绪检查，供 readinessProbe 和发布流量切换使用。
func ReadyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := healthlogic.NewHealthLogic(r.Context(), svcCtx).Readiness(r.Context())
		if err != nil {
			loggerx.Errorw(r.Context(), "健康检查 依赖未就绪", err)
			helper.NewJSONResp(r.Context(), w).
				SetHTTPStatus(http.StatusServiceUnavailable).
				SetCode(codes.DependencyUnavailable).
				SetError(err).
				Fail("", resp)
			return
		}
		helper.NewJSONResp(r.Context(), w).SetCode(codes.OK).Success(resp)
	}
}
