package admin

import (
	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/handler/shared"
	adminlogic "admin/internal/logic/admin"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// AddAdminHandler 处理新增管理员请求，并在写审计前对敏感字段做脱敏。
func AddAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.AdminAdd, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		// 解析请求参数
		var req types.AddAdminReq
		if err := httpx.Parse(r, &req); err != nil {
			return nil, shared.ParamErrorResult(err)
		}

		// 业务逻辑处理
		logicObj := adminlogic.NewAdminLogic(r, sCtx)
		resp := logicObj.Create(&req)

		// 脱敏处理
		req.Password = "***"
		req.MfaSecureKey = "***"

		// 记录请求参数，便于日志记录。
		resp.WithReq(&req)

		return logicObj, resp
	})
}

// AuthProfileHandler 返回当前管理员登录后的初始化信息，如菜单、角色和基础资料。
func AuthProfileHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(shared.AuthProfile.Alias))
		logicObj := adminlogic.NewAdminLogic(r, sCtx)
		// 获取当前登录管理员信息
		admin := logicObj.GetCtxAdmin()
		if admin == nil || admin.ID == 0 {
			return nil, &types.BizResult{
				Code:       codes.Unauthorized,
				MessageKey: i18n.MsgKeyNeedLogin,
				Error:      types.Nil,
			}
		}

		// 业务逻辑处理
		resp := logicObj.GetLoginAfterInfo(admin)

		return logicObj, resp
	})
}

// GetUserPermissionCodesHandler 查询当前登录管理员拥有的权限码集合。
func GetUserPermissionCodesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(shared.AuthCodes.Alias))
		logicObj := adminlogic.NewAdminLogic(r, sCtx)
		ctxAdmin := logicObj.GetCtxAdmin()
		if ctxAdmin == nil || ctxAdmin.ID == 0 {
			return nil, &types.BizResult{
				Code:       codes.Unauthorized,
				MessageKey: i18n.MsgKeyNeedLogin,
				Error:      types.Nil,
			}
		}
		resp := logicObj.GetUserPermissionCodes(ctxAdmin.ID)
		return logicObj, resp
	})
}

// RefreshAccessTokenHandler 刷新当前管理员的访问令牌。
func RefreshAccessTokenHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandlerFunc(func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(shared.AuthRefresh.Alias))
		logicObj := adminlogic.NewAdminLogic(r, sCtx)
		ctxAdmin := logicObj.GetCtxAdmin()
		if ctxAdmin == nil || ctxAdmin.ID == 0 {
			return nil, &types.BizResult{
				Code:       codes.Unauthorized,
				MessageKey: i18n.MsgKeyNeedLogin,
				Error:      types.Nil,
			}
		}
		resp := logicObj.RefreshAccessToken(ctxAdmin)
		return logicObj, resp
	})
}
