package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/routealias"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
)

// TestVerifyAdminTokenRequiresCachedSession 校验统一 token 校验会同时检查 JWT 与 Redis 登录态。
func TestVerifyAdminTokenRequiresCachedSession(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	secret := "test-secret"
	token := testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour))
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", JwtSecret: secret}, svc.Dependencies{Rds: client})
	if err := cachelogic.NewCacheLogic(ctx, svcCtx).SetAdminInfo(1, &types.AdminInfo{
		ID:       1,
		UserName: "admin",
		Token:    token,
	}); err != nil {
		t.Fatalf("写入管理员缓存失败: %v", err)
	}

	identity, err := verifyAdminToken(ctx, svcCtx, token, true)
	if err != nil {
		t.Fatalf("校验 token 失败: %v", err)
	}
	if identity.UserID != 1 || identity.UserName != "admin" || identity.Token != token {
		t.Fatalf("解析出的身份信息不符合预期: %+v", identity)
	}
}

// TestVerifyAdminTokenRejectsMissingCachedSession 验证 Redis 会话缺失时不会仅凭 JWT 自动恢复登录态。
func TestVerifyAdminTokenRejectsMissingCachedSession(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	secret := "test-secret"
	token := testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour))
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", JwtSecret: secret}, svc.Dependencies{Rds: client})
	if _, err := verifyAdminToken(context.Background(), svcCtx, token, true); !errors.Is(err, errInvalidToken) {
		t.Fatalf("缺失 Redis 会话时应拒绝 JWT，实际错误为 %v", err)
	}
}

// TestVerifyAdminTokenRejectsExpiredToken 校验过期 token 会返回可识别的过期错误。
func TestVerifyAdminTokenRejectsExpiredToken(t *testing.T) {
	token := testAdminToken(t, "test-secret", 1, "admin", time.Now().Add(-time.Minute))
	svcCtx := &svc.ServiceContext{}
	svcCtx.UpdateConfig(config.Config{JwtSecret: "test-secret"})
	_, err := verifyAdminToken(context.Background(), svcCtx, token, false)
	if !errors.Is(err, errTokenExpired) {
		t.Fatalf("期望返回 errTokenExpired，实际为 %v", err)
	}
}

// TestVerifyAdminTokenDoesNotDeleteCurrentSessionForExpiredOldToken 验证旧 JWT 过期时不会误删同账号已刷新出的新会话。
func TestVerifyAdminTokenDoesNotDeleteCurrentSessionForExpiredOldToken(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() { runtimecfg.Restore(prev) })
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	const secret = "test-secret"
	expiredToken := testAdminToken(t, secret, 1, "admin", time.Now().Add(-time.Minute))
	currentToken := testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour))
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", JwtSecret: secret}, svc.Dependencies{Rds: client})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(1, &types.AdminInfo{ID: 1, UserName: "admin", Token: currentToken}); err != nil {
		t.Fatalf("写入当前管理员会话失败: %v", err)
	}

	if _, err := verifyAdminToken(context.Background(), svcCtx, expiredToken, true); !errors.Is(err, errTokenExpired) {
		t.Fatalf("过期旧 token 应返回 errTokenExpired，实际为 %v", err)
	}
	if cachedToken, err := cacheLogic.GetAdminToken(1); err != nil || cachedToken != currentToken {
		t.Fatalf("校验过期旧 token 后当前会话 token = %q error = %v", cachedToken, err)
	}
}

// TestRefreshTokenGraceIsLimitedToSessionFinalizationRoutes 验证上一枚 token 只允许刷新和退出，普通路由仍拒绝。
func TestRefreshTokenGraceIsLimitedToSessionFinalizationRoutes(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() { runtimecfg.Restore(prev) })
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	const secret = "test-secret"
	oldToken := testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour))
	newToken := testAdminToken(t, secret, 1, "admin", time.Now().Add(2*time.Hour))
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", JwtSecret: secret, JwtExpiresIn: 3600}, svc.Dependencies{Rds: client})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(1, &types.AdminInfo{ID: 1, UserName: "admin", Token: oldToken}); err != nil {
		t.Fatalf("写入管理员会话失败: %v", err)
	}
	if activeToken, err := cacheLogic.RotateAdminToken(1, oldToken, newToken); err != nil || activeToken != newToken {
		t.Fatalf("轮换管理员 token 失败，activeToken = %q error = %v", activeToken, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+oldToken)

	if _, err := verifyAdminTokenFromRequestForRoute(context.Background(), svcCtx, req, true, routealias.AuthProfile); !errors.Is(err, errInvalidToken) {
		t.Fatalf("普通鉴权路由应拒绝上一枚 token，实际错误为 %v", err)
	}
	identity, err := verifyAdminTokenFromRequestForRoute(context.Background(), svcCtx, req, true, routealias.AuthRefresh)
	if err != nil {
		t.Fatalf("auth.refresh 应允许宽限期内上一枚 token，实际错误为 %v", err)
	}
	if identity == nil || identity.Token != oldToken || identity.UserID != 1 {
		t.Fatalf("刷新路由解析身份异常: %+v", identity)
	}
	identity, err = verifyAdminTokenFromRequestForRoute(context.Background(), svcCtx, req, true, routealias.AuthLogout)
	if err != nil {
		t.Fatalf("auth.logout 应允许宽限期内上一枚刷新 token，实际错误为 %v", err)
	}
	if identity == nil || identity.Token != oldToken || identity.UserID != 1 {
		t.Fatalf("退出路由解析身份异常: %+v", identity)
	}
}

// TestVerifyAdminTokenRejectsOtherHMACAlgorithms 校验服务端只接受约定的 HS256 算法。
func TestVerifyAdminTokenRejectsOtherHMACAlgorithms(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"sub":      1,
		"username": "admin",
		"exp":      time.Now().Add(time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("签发 HS384 token 失败: %v", err)
	}
	svcCtx := &svc.ServiceContext{}
	svcCtx.UpdateConfig(config.Config{JwtSecret: "test-secret"})
	if _, err = verifyAdminToken(context.Background(), svcCtx, signed, false); !errors.Is(err, errInvalidToken) {
		t.Fatalf("期望拒绝 HS384 token，实际为 %v", err)
	}
}

// TestVerifyAdminTokenRejectsMissingRedisSessionStore 校验需要会话态时，未注入 Redis 会返回无效 token 而不是 panic。
func TestVerifyAdminTokenRejectsMissingRedisSessionStore(t *testing.T) {
	token := testAdminToken(t, "test-secret", 1, "admin", time.Now().Add(time.Hour))
	svcCtx := &svc.ServiceContext{}
	svcCtx.UpdateConfig(config.Config{JwtSecret: "test-secret"})
	_, err := verifyAdminToken(context.Background(), svcCtx, token, true)
	if !errors.Is(err, errInvalidToken) {
		t.Fatalf("期望返回 errInvalidToken，实际为 %v", err)
	}
}

// TestBearerTokenRejectsMissingHeader 校验 Bearer 解析会拒绝空鉴权头。
func TestBearerTokenRejectsMissingHeader(t *testing.T) {
	_, err := bearerToken("")
	if !errors.Is(err, errMissingBearerToken) {
		t.Fatalf("期望返回 errMissingBearerToken，实际为 %v", err)
	}
}

// TestDocsRequestTokenSupportsCookie 校验文档请求可从受限 cookie 读取 token。
func TestDocsRequestTokenSupportsCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.AddCookie(&http.Cookie{Name: DocsSessionCookieName, Value: "cookie-token"})

	token, err := docsRequestToken(req)
	if err != nil {
		t.Fatalf("读取文档 cookie token 失败: %v", err)
	}
	if token != "cookie-token" {
		t.Fatalf("token = %q, want cookie-token", token)
	}
}

// TestDocsRequestTokenPrefersAuthorization 校验请求头 token 优先级高于文档 cookie。
func TestDocsRequestTokenPrefersAuthorization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	req.AddCookie(&http.Cookie{Name: DocsSessionCookieName, Value: "cookie-token"})

	token, err := docsRequestToken(req)
	if err != nil {
		t.Fatalf("读取文档请求 token 失败: %v", err)
	}
	if token != "header-token" {
		t.Fatalf("token = %q, want header-token", token)
	}
}

// testAdminToken 生成测试用管理员 JWT。
func testAdminToken(t *testing.T, secret string, userID int, userName string, expiresAt time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      userID,
		"username": userName,
		"exp":      expiresAt.Unix(),
		"iat":      time.Now().Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return signed
}
