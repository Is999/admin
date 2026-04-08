package handler

import (
	codes "admin_cron/common/codes"
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// QueryAdminLogHandler 处理管理员审计日志分页查询请求，并返回链路化日志结果。
func QueryAdminLogHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(queryAdminLog, func(r *http.Request) (LogicObj, *types.BizResult) {
		// 解析请求参数并校验
		var req types.AdminLogQueryReq
		if err := httpx.Parse(r, &req); err != nil {
			return nil, paramErrorResult(codes.ParamError, err)
		}

		// 业务逻辑处理
		logicObj := logic.NewAdminLogLogic(r, sCtx)
		resp := logicObj.QueryAdminLog(&req)

		// 记录请求参数，便于日志记录。
		resp.WithReq(&req)

		return logicObj, resp
	})
}
