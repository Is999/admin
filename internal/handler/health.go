package handler

import (
	"net/http"

	codes "admin_cron/common/codes"
	"admin_cron/helper"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
)

// HealthHandler 提供兼容健康检查接口，行为等同于 LiveHandler。
func HealthHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return LiveHandler(svcCtx)
}

// LiveHandler 提供进程存活检查，不访问外部依赖，供 livenessProbe 使用。
func LiveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := logic.NewHealthLogic(r.Context(), svcCtx).Liveness()
		helper.NewJsonResp(r.Context(), w).SetCode(codes.OK).Success(resp)
	}
}

// ReadyHandler 提供依赖就绪检查，供 readinessProbe 和发布流量切换使用。
func ReadyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := logic.NewHealthLogic(r.Context(), svcCtx).Readiness(r.Context())
		if err != nil {
			loggerx.Errorw(r.Context(), "健康检查 依赖未就绪", err)
			helper.NewJsonResp(r.Context(), w).
				SetHttpStatus(http.StatusServiceUnavailable).
				SetCode(codes.DependencyUnavailable).
				SetError(err).
				Fail("", resp)
			return
		}
		helper.NewJsonResp(r.Context(), w).SetCode(codes.OK).Success(resp)
	}
}
