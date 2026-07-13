package middleware

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/infra/loggerx"
	cachelogic "admin/internal/logic/cache"
	securitylogic "admin/internal/logic/security"
	"admin/internal/requestctx"
	"admin/internal/routealias"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// DocsJwtMiddleware 为文档站提供轻量鉴权，所有运行模式都要求携带有效后台凭证。
func DocsJwtMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := requestctx.New(r.Context())
			clientIP := requestClientIP(svcCtx, r)
			requestctx.SetRequest(ctx, r.Method, r.URL.Path, clientIP)
			docsAlias := routealias.DocsEntryAliasForPath(r.URL.Path)
			requestctx.SetRoute(ctx, string(docsAlias))
			r = r.WithContext(ctx)

			identity, err := verifyAdminTokenFromDocsRequest(ctx, svcCtx, r, true)
			if err != nil {
				if errors.Is(err, cachelogic.ErrRedisUnavailable) || errors.Is(err, cachelogic.ErrSecurityCacheSyncPending) {
					failAuthDependency(ctx, w, err)
					return
				}
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

			ip := clientIP
			security := securitylogic.NewSecurityLogic(ctx, svcCtx)
			if err = security.CheckAdminAccess(identity.Session, string(docsAlias), ip, identity.LoginIP); err != nil {
				failAdminAccess(ctx, w, err)
				return
			}
			if routealias.DocsPathNeedsResourcePermission(r.URL.Path) {
				resource, ok := routealias.DocsResourceForPath(r.URL.Path)
				if !ok {
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusForbidden).
						SetCode(codes.Forbidden).
						Fail(i18n.MsgKeyForbidden)
					return
				}
				allowed, permissionErr := security.CheckDocPermission(identity.UserID, resource)
				if permissionErr != nil {
					failAuthDependency(ctx, w, permissionErr)
					return
				}
				if !allowed {
					helper.NewJSONResp(ctx, w).
						SetHTTPStatus(http.StatusForbidden).
						SetCode(codes.Forbidden).
						Fail(i18n.MsgKeyForbidden)
					return
				}
			}

			requestctx.SetAccessToken(ctx, identity.Token)
			requestctx.SetUser(ctx, identity.UserID, identity.UserName, clientIP)
			ctx = cachelogic.WithAdminSession(ctx, identity.Session)
			r = r.WithContext(loggerx.BindContext(ctx))
			next(w, r)
		}
	}
}
