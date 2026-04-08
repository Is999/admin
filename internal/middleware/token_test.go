package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"admin_cron/internal/config"
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
)

// TestVerifyAdminTokenRequiresCachedSession 校验统一 token 校验会同时检查 JWT 与 Redis 登录态。
func TestVerifyAdminTokenRequiresCachedSession(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	secret := "test-secret"
	token := testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour))
	svcCtx := &svc.ServiceContext{Rds: client}
	svcCtx.UpdateConfig(config.Config{JwtSecret: secret})
	if err := logic.NewCacheLogic(ctx, svcCtx).SetAdminInfo(1, &types.AdminInfo{
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
