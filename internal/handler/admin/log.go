package admin

import (
	"admin/internal/handler/shared"
	adminlogic "admin/internal/logic/admin"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// QueryAdminLogHandler 处理管理员审计日志分页查询请求，并返回链路化日志结果。
func QueryAdminLogHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.AdminLogQuery, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		// 解析请求参数并校验
		var req types.AdminLogQueryReq
		if err := httpx.Parse(r, &req); err != nil {
			return nil, types.ParamErrorResult(err)
		}

		// 业务逻辑处理
		logicObj := adminlogic.NewAdminLogLogic(r, sCtx)
		resp := logicObj.QueryAdminLog(&req)

		// 记录请求参数，便于日志记录。
		resp.WithReq(&req)

		return logicObj, resp
	})
}
