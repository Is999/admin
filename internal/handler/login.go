package handler

import (
	"context"
	"net/http"

	i18n "admin_cron/common/i18n"
	"admin_cron/internal/audit"
	"admin_cron/internal/logic"
	"admin_cron/internal/model"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// LoginCaptchaHandler 返回登录图形验证码。
func LoginCaptchaHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return respHandler(func(r *http.Request) (LogicObj, *types.BizResult) {
		requestctx.SetRoute(r.Context(), string(AuthCaptcha.Alias))
		logicObj := logic.NewAdminLogic(r, sCtx)
		return logicObj, logicObj.BuildLoginCaptcha()
	})
}

// LoginHandler 处理管理员登录请求，并在成功后补齐当前请求的审计与用户上下文。
func LoginHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 登录接口没有经过 auth 中间件，这里手动补齐统一路由别名，方便 access log 与审计对齐。
		requestctx.SetRoute(r.Context(), string(AuthLogin.Alias))

		var req types.LoginReq
		if err := httpx.Parse(r, &req); err != nil {
			writeBizResponse(w, r, nil, paramErrorResult(0, err), nil, "")
			return
		}
		req.Ip = utils.ClientIP(r)
		logicObj := logic.NewAdminLogic(r, sCtx)
		if captchaResp := logicObj.VerifyLoginCaptcha(req.Key, req.Captcha); captchaResp.IsFailure() {
			message := captchaResp.ResolveMessage(r.Context())
			recordAuthAudit(logicObj.Audit(), r.Context(), audit.Event{
				Action:   model.ActionAdminLogin,
				Route:    string(AuthLogin.Alias),
				Method:   "LoginHandler",
				Describe: AuthLogin.Describe,
				Data:     req,
				UserName: req.Username,
				IP:       req.Ip,
			}, captchaResp, http.StatusOK, message)
			writeBizResponse(w, r, logicObj, captchaResp.WithReq(&req), nil, "")
			return
		}
		resp := logicObj.Login(&req).WithReq(&req)
		message := resp.ResolveMessage(r.Context())

		if resp.IsSuccess() {
			// 登录成功后把用户信息写回 request meta，保证本次请求的访问日志也能带上 user_id。
			if loginUser, ok := resp.Data.(*types.UserCompatLoginResp); ok && loginUser.User != nil {
				requestctx.SetUser(r.Context(), loginUser.User.ID, req.Username, req.Ip)
				// 登录成功后异步投递“管理员登录”消息，通知超级管理员与登录本人，便于安全审计与排障回溯。
				go logic.EmitAdminLoginMessage(r.Context(), sCtx, loginUser.User.ID, req.Username, req.Ip)
			}
		}
		recordAuthAudit(logicObj.Audit(), r.Context(), audit.Event{
			Action:   model.ActionAdminLogin,
			Route:    string(AuthLogin.Alias),
			Method:   "LoginHandler",
			Describe: AuthLogin.Describe,
			Data:     req,
			UserName: req.Username,
			IP:       req.Ip,
		}, resp, http.StatusOK, message)

		writeBizResponse(w, r, logicObj, resp, nil, "")
	}
}

// LogoutHandler 处理管理员登出请求，并统一记录登出审计事件。
func LogoutHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 登出接口已经过 auth 中间件，但仍显式设置 route，避免后续审计依赖 handler 名称推断。
		requestctx.SetRoute(r.Context(), string(AuthLogout.Alias))

		logicObj := logic.NewAdminLogic(r, sCtx)
		ctxAdmin := logicObj.GetCtxAdmin()
		if ctxAdmin == nil || ctxAdmin.ID == 0 {
			writeBizResponse(w, r, logicObj, types.Unauthorized(i18n.MsgKeyNeedLogin).ToBizResult(), nil, "")
			return
		}

		resp := logicObj.Logout(ctxAdmin).WithReq(map[string]any{"username": ctxAdmin.Name})
		message := resp.ResolveMessage(r.Context())
		recordAuthAudit(logicObj.Audit(), r.Context(), audit.Event{
			Action:   model.ActionAdminLogout,
			Route:    string(AuthLogout.Alias),
			Method:   "LogoutHandler",
			Describe: AuthLogout.Describe,
			Data:     map[string]any{"username": ctxAdmin.Name},
			UserID:   ctxAdmin.ID,
			UserName: ctxAdmin.Name,
			IP:       ctxAdmin.IP,
		}, resp, http.StatusOK, message)

		writeBizResponse(w, r, logicObj, resp, nil, "")
	}
}

// recordAuthAudit 统一记录登录/登出的审计事件。
// 登录链路会先落审计再写响应，因此这里把 HTTP 状态码和业务码显式补齐，避免审计丢字段。
func recordAuthAudit(recorder *audit.Recorder, ctx context.Context, event audit.Event, resp *types.BizResult, httpStatus int, errorMessage string) {
	if recorder == nil || resp == nil {
		return
	}
	success := resp.IsSuccess()
	event.Success = &success
	event.HTTPStatus = httpStatus
	event.BizCode = resp.Code
	if !success {
		event.ErrorMessage = errorMessage
	}
	_ = recorder.Record(ctx, event)
}
