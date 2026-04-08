package middleware

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/helper"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/logic"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/svc"

	"github.com/Is999/go-utils"
	"github.com/zeromicro/go-zero/rest"
)

// DocsJwtMiddleware 为文档站提供轻量鉴权；开发和测试环境跳过校验，生产环境要求携带有效 JWT。
func DocsJwtMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := requestctx.New(r.Context())
			requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
			// 文档站统一绑定到 docs.index 权限码，前后端都复用这一条页面权限。
			requestctx.SetRoute(ctx, "docs.index")
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
				helper.NewJsonResp(ctx, w).
					SetHttpStatus(http.StatusUnauthorized).
					SetCode(codes.Unauthorized).
					Fail(message)
				return
			}

			ip := utils.ClientIP(r)
			if err = logic.NewSecurityLogic(ctx, svcCtx).CheckAdminAccess(identity.UserID, "docs.index", ip, identity.LoginIP); err != nil {
				switch {
				case errors.Is(err, logic.ErrAdminPermissionDenied):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusForbidden).
						SetCode(codes.Forbidden).
						Fail(i18n.MsgKeyForbidden)
				case errors.Is(err, logic.ErrAdminDisabled):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						Fail(i18n.MsgKeyUserDisabled)
				case errors.Is(err, logic.ErrAdminIPChanged):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminLoginIPChanged)
				case errors.Is(err, logic.ErrAdminIPNotAllowed):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminIPNotAllowed)
				case errors.Is(err, logic.ErrAdminMFARequired):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusOK).
						SetCode(codes.CheckMFACode).
						SetError(err).
						Fail(i18n.MsgKeyCheckMFA)
				default:
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
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
