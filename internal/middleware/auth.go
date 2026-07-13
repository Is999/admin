package middleware

import (
	"context"
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
)

// RouteAlias 是路由在权限/审计体系中的稳定标识，避免直接依赖 URL。
type RouteAlias = routealias.Alias

const (
	// Ignore 表示该路由跳过业务权限校验，但仍要求 token 合法。
	Ignore = routealias.Ignore
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
		handler(w, bindPublicRequestMeta(r, alias, m.svc))
	}
}

// bindPublicRequestMeta 为公开路由预先写入请求元数据，避免最外层加密中间件读取不到 route alias。
func bindPublicRequestMeta(r *http.Request, alias RouteAlias, svcCtx *svc.ServiceContext) *http.Request {
	if r == nil {
		return r
	}
	ctx, _ := requestctx.New(r.Context())
	requestctx.SetRequest(ctx, r.Method, r.URL.Path, requestClientIP(svcCtx, r))
	if alias != "" && alias != Ignore {
		requestctx.SetRoute(ctx, string(alias))
	}
	return r.WithContext(ctx)
}

// failAuthDependency 返回不暴露内部错误的鉴权依赖故障响应。
func failAuthDependency(ctx context.Context, w http.ResponseWriter, err error) {
	code := codes.DependencyUnavailable
	message := i18n.MsgKeyDependencyUnavailable
	if errors.Is(err, cachelogic.ErrRedisUnavailable) {
		code = codes.RedisUnavailable
		message = i18n.MsgKeyRedisUnavailable
	}
	helper.NewJSONResp(ctx, w).
		SetHTTPStatus(http.StatusServiceUnavailable).
		SetCode(code).
		SetError(err).
		Fail(message)
}

// failAdminAccess 把账号状态、IP、改密、MFA 和权限错误映射为统一响应。
func failAdminAccess(ctx context.Context, w http.ResponseWriter, err error) {
	resp := helper.NewJSONResp(ctx, w).SetError(err)
	switch {
	case errors.Is(err, securitylogic.ErrAdminPermissionDenied):
		resp.SetHTTPStatus(http.StatusForbidden).SetCode(codes.Forbidden).Fail(i18n.MsgKeyForbidden)
	case errors.Is(err, securitylogic.ErrAdminDisabled):
		resp.SetHTTPStatus(http.StatusUnauthorized).SetCode(codes.Unauthorized).Fail(i18n.MsgKeyUserDisabled)
	case errors.Is(err, securitylogic.ErrAdminIPChanged):
		resp.SetHTTPStatus(http.StatusUnauthorized).SetCode(codes.Unauthorized).Fail(i18n.MsgKeyAdminLoginIPChanged)
	case errors.Is(err, securitylogic.ErrAdminIPNotAllowed):
		resp.SetHTTPStatus(http.StatusUnauthorized).SetCode(codes.Unauthorized).Fail(i18n.MsgKeyAdminIPNotAllowed)
	case errors.Is(err, securitylogic.ErrAdminPasswordResetRequired):
		resp.SetHTTPStatus(http.StatusOK).SetCode(codes.CheckPasswordReset).Fail(i18n.MsgKeyCheckPasswordReset)
	case errors.Is(err, securitylogic.ErrAdminMFABindRequired):
		resp.SetHTTPStatus(http.StatusOK).SetCode(codes.CheckMFABind).Fail(i18n.MsgKeyCheckMFABind)
	case errors.Is(err, securitylogic.ErrAdminMFARequired):
		resp.SetHTTPStatus(http.StatusOK).SetCode(codes.CheckMFACode).Fail(i18n.MsgKeyCheckMFA)
	default:
		failAuthDependency(ctx, w, err)
	}
}

// Handle 负责鉴权并补齐当前请求的操作者信息，后续 access log、logic、审计都从同一份 meta 取值。
// alias 用于写入统一路由别名，避免日志和权限系统各自维护一套路由名称。
func (m *AuthMiddleware) Handle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		clientIP := requestClientIP(m.svc, r)
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, clientIP)
		if alias != "" && alias != Ignore {
			requestctx.SetRoute(ctx, string(alias))
		}

		failUnauthorized := func(messageKey string) {
			helper.NewJSONResp(ctx, w).
				SetHTTPStatus(http.StatusUnauthorized).
				SetCode(codes.Unauthorized).
				Fail(messageKey)
		}
		identity, err := verifyAdminTokenFromRequestForRoute(ctx, m.svc, r, true, alias)
		switch {
		case errors.Is(err, errMissingBearerToken):
			failUnauthorized(i18n.MsgKeyUnauthorizedText)
			return
		case errors.Is(err, errTokenExpired):
			failUnauthorized(i18n.MsgKeyTokenExpired)
			return
		case errors.Is(err, cachelogic.ErrRedisUnavailable):
			failAuthDependency(ctx, w, err)
			return
		case errors.Is(err, cachelogic.ErrSecurityCacheSyncPending):
			failAuthDependency(ctx, w, err)
			return
		case err != nil:
			failUnauthorized(i18n.MsgKeyTokenInvalid)
			return
		}

		ip := clientIP
		if err = securitylogic.NewSecurityLogic(ctx, m.svc).CheckAdminAccess(identity.Session, string(alias), ip, identity.LoginIP); err != nil {
			failAdminAccess(ctx, w, err)
			return
		}

		// 鉴权通过后，把后续链路要用到的核心信息和已校验会话一次性写入请求上下文。
		requestctx.SetAccessToken(ctx, identity.Token)
		requestctx.SetUser(ctx, identity.UserID, identity.UserName, ip)
		ctx = cachelogic.WithAdminSession(ctx, identity.Session)
		ctx = loggerx.BindContext(ctx)

		next(w, r.WithContext(ctx))
	}
	return m.PublicHandle(handler, alias)
}
