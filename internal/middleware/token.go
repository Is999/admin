package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"admin/internal/config"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/routealias"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
)

const (
	// DocsSessionCookieName 是文档站专用会话 cookie 名称，只用于浏览器访问 /api/docs。
	DocsSessionCookieName = "admin_docs_token"
	// DocsSessionCookiePath 限制文档会话 cookie 只随文档站请求发送。
	DocsSessionCookiePath = "/api/docs"
)

var (
	// errMissingBearerToken 表示请求头中没有携带 Bearer token。
	errMissingBearerToken = errors.New("缺少 Bearer Token")
	// errInvalidToken 表示 token 签名、格式、会话状态或 claims 不合法。
	errInvalidToken = errors.New("Token 无效")
	// errTokenExpired 表示 token 已过期，需要调用方返回过期提示。
	errTokenExpired = errors.New("Token 已过期")
)

// adminTokenIdentity 表示通过 JWT 和 Redis 会话校验后的管理员身份。
type adminTokenIdentity struct {
	UserID    int                 // 管理员 ID，来自 JWT sub
	UserName  string              // 管理员用户名，以当前 Redis 会话为准
	Token     string              // 当前请求携带的原始 token
	ExpiresAt int64               // token 过期时间戳，单位秒
	LoginIP   string              // 登录时写入 JWT 的 IP，用于后台 IP 变更校验
	Session   *types.AdminSession // 当前 Redis 会话，供后续安全校验复用
}

// bearerToken 从 Authorization 请求头中提取 Bearer token。
func bearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", errMissingBearerToken
	}
	token := strings.TrimSpace(header[len("Bearer "):])
	if token == "" {
		return "", errMissingBearerToken
	}
	return token, nil
}

// docsRequestToken 从文档请求中读取 token，优先使用请求头，支持浏览器 iframe 的 HttpOnly cookie。
func docsRequestToken(r *http.Request) (string, error) {
	if r == nil {
		return "", errMissingBearerToken
	}
	tokenString, err := bearerToken(r.Header.Get("Authorization"))
	if err == nil {
		return tokenString, nil
	}
	cookie, cookieErr := r.Cookie(DocsSessionCookieName)
	if cookieErr != nil {
		return "", errMissingBearerToken
	}
	tokenString = strings.TrimSpace(cookie.Value)
	if tokenString == "" {
		return "", errMissingBearerToken
	}
	return tokenString, nil
}

// verifyAdminToken 统一校验后台 JWT，并按需校验 Redis 中的当前登录会话。
func verifyAdminToken(ctx context.Context, svcCtx *svc.ServiceContext, tokenString string, requireSession bool) (*adminTokenIdentity, error) {
	return verifyAdminTokenWithSessionGrace(ctx, svcCtx, tokenString, requireSession, false)
}

// verifyAdminTokenWithSessionGrace 校验后台 JWT；allowSessionGrace 只允许会话刷新和退出路由传入 true。
func verifyAdminTokenWithSessionGrace(ctx context.Context, svcCtx *svc.ServiceContext, tokenString string, requireSession bool, allowSessionGrace bool) (*adminTokenIdentity, error) {
	cfg := config.Config{}
	if svcCtx != nil {
		cfg = svcCtx.CurrentConfig()
	}
	if svcCtx == nil || strings.TrimSpace(cfg.JwtSecret) == "" || strings.TrimSpace(tokenString) == "" {
		return nil, errInvalidToken
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.Wrap(errInvalidToken, "签名算法不匹配")
		}
		return []byte(cfg.JwtSecret), nil
	})
	if err != nil || !token.Valid {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errTokenExpired
		}
		return nil, errInvalidToken
	}

	userID, ok := claims["sub"].(float64)
	if !ok || userID <= 0 {
		return nil, errInvalidToken
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, errInvalidToken
	}
	uid := int(userID)
	loginIP := ""
	if rawIP, ok := claims["ip"]; ok {
		loginIP = fmt.Sprint(rawIP)
	}
	identity := &adminTokenIdentity{
		UserID:    uid,
		UserName:  fmt.Sprint(claims["username"]),
		Token:     tokenString,
		ExpiresAt: int64(exp),
		LoginIP:   loginIP,
	}
	if !requireSession {
		return identity, nil
	}
	if svcCtx.Rds == nil {
		return nil, cachelogic.WrapRedisUnavailable(nil, "校验管理员会话失败")
	}

	// Redis 会话只读取一次，后续账号状态、MFA 与权限校验复用同一份快照。
	cacheLogic := cachelogic.NewCacheLogic(ctx, svcCtx)
	session, err := cacheLogic.GetAdminSession(uid)
	if errors.Is(err, redis.Nil) {
		// 会话不存在表示已退出、被撤销或已过期，禁止从数据库恢复登录态。
		return nil, errInvalidToken
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	if session == nil || session.ID != uid {
		return nil, errInvalidToken
	}
	if allowSessionGrace {
		allowed, err := cacheLogic.CanUseAdminSessionToken(uid, tokenString)
		if err != nil {
			return nil, cachelogic.WrapRedisUnavailable(err, "校验管理员会话宽限期失败")
		}
		if !allowed {
			return nil, errInvalidToken
		}
	} else if session.Token != tokenString {
		return nil, errInvalidToken
	}
	identity.UserName = session.UserName
	identity.Session = session
	return identity, nil
}

// verifyAdminTokenFromRequestForRoute 仅为 auth.refresh 和 auth.logout 开启上一枚刷新 token 的短暂宽限。
func verifyAdminTokenFromRequestForRoute(ctx context.Context, svcCtx *svc.ServiceContext, r *http.Request, requireSession bool, alias RouteAlias) (*adminTokenIdentity, error) {
	tokenString, err := bearerToken(r.Header.Get("Authorization"))
	if err != nil {
		return nil, errors.Tag(err)
	}
	allowSessionGrace := alias == routealias.AuthRefresh || alias == routealias.AuthLogout
	return verifyAdminTokenWithSessionGrace(ctx, svcCtx, tokenString, requireSession, allowSessionGrace)
}

// verifyAdminTokenFromDocsRequest 校验文档站请求 token，允许通过受保护接口写入的文档 cookie 访问静态资源。
func verifyAdminTokenFromDocsRequest(ctx context.Context, svcCtx *svc.ServiceContext, r *http.Request, requireSession bool) (*adminTokenIdentity, error) {
	tokenString, err := docsRequestToken(r)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return verifyAdminToken(ctx, svcCtx, tokenString, requireSession)
}
