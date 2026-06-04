package middleware

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/infra/loggerx"
	securitylogic "admin/internal/logic/security"
	"admin/internal/requestctx"
	"admin/internal/routealias"
	"admin/internal/svc"

	"github.com/Is999/go-utils"
	"github.com/zeromicro/go-zero/rest"
)

// DocsJwtMiddleware 为文档站提供轻量鉴权；开发和测试环境跳过校验，生产环境要求携带有效 JWT。
func DocsJwtMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := requestctx.New(r.Context())
			requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
			docsAlias := docsRouteAliasForPath(r.URL.Path)
			requestctx.SetRoute(ctx, string(docsAlias))
			r = r.WithContext(ctx)
			mode := ""
			if svcCtx != nil {
				mode = svcCtx.CurrentConfig().Mode
			}
			if mode == "dev" || mode == "test" {
				// 开发和测试环境下跳过认证
				next(w, r)
				return
			}

			identity, err := verifyAdminTokenFromDocsRequest(ctx, svcCtx, r, true)
			if err != nil {
				message := i18n.MsgKeyTokenInvalid
				if errors.Is(err, errMissingBearerToken) {
					message = i18n.MsgKeyUnauthorizedText
				}
				if errors.Is(err, errTokenExpired) {
					message = i18n.MsgKeyTokenExpired
				}
				helper.NewJSONResp(ctx, w).
					SetHTTPStatus(http.StatusUnauthorized).
					SetCode(codes.Unauthorized).
					Fail(message)
				return
			}

			ip := utils.ClientIP(r)
			if err = securitylogic.NewSecurityLogic(ctx, svcCtx).CheckAdminAccess(identity.UserID, string(docsAlias), ip, identity.LoginIP); err != nil {
				switch {
				case errors.Is(err, securitylogic.ErrAdminPermissionDenied):
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusForbidden).
						SetCode(codes.Forbidden).
						Fail(i18n.MsgKeyForbidden)
				case errors.Is(err, securitylogic.ErrAdminDisabled):
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						Fail(i18n.MsgKeyUserDisabled)
				case errors.Is(err, securitylogic.ErrAdminIPChanged):
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminLoginIPChanged)
				case errors.Is(err, securitylogic.ErrAdminIPNotAllowed):
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminIPNotAllowed)
				case errors.Is(err, securitylogic.ErrAdminMFARequired):
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusOK).
						SetCode(codes.CheckMFACode).
						SetError(err).
						Fail(i18n.MsgKeyCheckMFA)
				default:
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						Fail(i18n.MsgKeyTokenInvalid)
				}
				return
			}

			requestctx.SetAccessToken(ctx, identity.Token)
			requestctx.SetUser(ctx, identity.UserID, identity.UserName, utils.ClientIP(r))
			r = r.WithContext(loggerx.BindContext(ctx))
			next(w, r)
		}
	}
}

// docsRouteAliasForPath 按文档站请求路径返回目录级权限别名；未命中具体目录时使用入口权限。
func docsRouteAliasForPath(requestPath string) routealias.Alias {
	return routealias.DocsAliasForPath(requestPath)
}
