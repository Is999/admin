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
)

// RouteAlias 是路由在权限/审计体系中的稳定标识，避免直接依赖 URL。
type RouteAlias string

const (
	// Ignore 表示该路由跳过业务权限校验，但仍要求 token 合法。
	Ignore RouteAlias = "ignore"
)

// AuthMiddleware 负责 JWT 鉴权、Redis token 校验以及请求元数据中的用户信息补全。
type AuthMiddleware struct {
	svc       *svc.ServiceContext  // 鉴权过程中依赖的配置、缓存与公共服务集合
	crypto    *CryptoMiddleware    // 请求解密与响应加密中间件
	signature *SignatureMiddleware // 请求验签与响应签名中间件
}

// NewAuthMiddleware 创建鉴权中间件实例。
func NewAuthMiddleware(svcCtx *svc.ServiceContext) *AuthMiddleware {
	return &AuthMiddleware{
		svc:       svcCtx,
		crypto:    NewCryptoMiddleware(svcCtx),
		signature: NewSignatureMiddleware(svcCtx),
	}
}

// PublicHandle 为登录等未登录接口挂载加密与签名中间件，但不执行 JWT 鉴权。
func (m *AuthMiddleware) PublicHandle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	handler := next
	if m.signature != nil {
		handler = m.signature.Handle(handler, alias)
	}
	if m.crypto != nil {
		handler = m.crypto.Handle(handler)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// 公开路由必须在进入最外层加密/签名中间件前写入 route alias，否则登录等接口的响应策略无法命中。
		handler(w, bindPublicRequestMeta(r, alias))
	}
}

// bindPublicRequestMeta 为公开路由预先写入请求元数据，避免最外层加密中间件读取不到 route alias。
func bindPublicRequestMeta(r *http.Request, alias RouteAlias) *http.Request {
	if r == nil {
		return r
	}
	ctx, _ := requestctx.New(r.Context())
	requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
	if alias != "" && alias != Ignore {
		requestctx.SetRoute(ctx, string(alias))
	}
	return r.WithContext(ctx)
}

// Handle 负责鉴权并补齐当前请求的操作者信息，后续 access log、logic、审计都从同一份 meta 取值。
// alias 用于写入统一路由别名，避免日志和权限系统各自维护一套路由名称。
func (m *AuthMiddleware) Handle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
		if alias != "" && alias != Ignore {
			requestctx.SetRoute(ctx, string(alias))
		}

		failUnauthorized := func(messageKey string) {
			helper.NewJsonResp(ctx, w).
				SetHttpStatus(http.StatusUnauthorized).
				SetCode(codes.Unauthorized).
				Fail(messageKey)
		}
		failForbidden := func(messageKey string) {
			helper.NewJsonResp(ctx, w).
				SetHttpStatus(http.StatusForbidden).
				SetCode(codes.Forbidden).
				Fail(messageKey)
		}

		identity, err := verifyAdminTokenFromRequest(ctx, m.svc, r, true)
		switch {
		case errors.Is(err, errMissingBearerToken):
			failUnauthorized(i18n.MsgKeyUnauthorizedText)
			return
		case errors.Is(err, errTokenExpired):
			failUnauthorized(i18n.MsgKeyTokenExpired)
			return
		case err != nil:
			failUnauthorized(i18n.MsgKeyTokenInvalid)
			return
		}

		ip := utils.ClientIP(r)
		if err = logic.NewSecurityLogic(ctx, m.svc).CheckAdminAccess(identity.UserID, string(alias), ip, identity.LoginIP); err != nil {
			switch {
			case errors.Is(err, logic.ErrAdminPermissionDenied):
				failForbidden(i18n.MsgKeyForbidden)
			case errors.Is(err, logic.ErrAdminDisabled):
				failUnauthorized(i18n.MsgKeyUserDisabled)
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
			case errors.Is(err, logic.ErrAdminPasswordResetRequired):
				helper.NewJsonResp(ctx, w).
					SetHttpStatus(http.StatusOK).
					SetCode(codes.CheckPasswordReset).
					SetError(err).
					Fail(i18n.MsgKeyCheckPasswordReset)
			case errors.Is(err, logic.ErrAdminMFABindRequired):
				helper.NewJsonResp(ctx, w).
					SetHttpStatus(http.StatusOK).
					SetCode(codes.CheckMFABind).
					SetError(err).
					Fail(i18n.MsgKeyCheckMFABind)
			case errors.Is(err, logic.ErrAdminMFARequired):
				helper.NewJsonResp(ctx, w).
					SetHttpStatus(http.StatusOK).
					SetCode(codes.CheckMFACode).
					SetError(err).
					Fail(i18n.MsgKeyCheckMFA)
			default:
				failUnauthorized(i18n.MsgKeyTokenInvalid)
			}
			return
		}

		// 鉴权通过后，把后续链路要用到的核心信息一次性写入请求元数据。
		requestctx.SetAccessToken(ctx, identity.Token)
		requestctx.SetUser(ctx, identity.UserID, identity.UserName, ip)
		ctx = loggerx.BindContext(ctx)

		// 活跃会话滑动续期，减少管理员持续操作期间的 Redis 回源抖动。
		_ = logic.NewCacheLogic(ctx, m.svc).TouchAdminInfo(identity.UserID)

		next(w, r.WithContext(ctx))
	}
	return m.PublicHandle(handler, alias)
}
