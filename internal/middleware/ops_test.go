package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// adminTestOpsNonce 是测试使用的固定 16 字节十六进制 nonce。
const adminTestOpsNonce = "00112233445566778899aabbccddeeff"

// TestAuthenticateOpsAcceptsPrivateSource 验证私网来源和完整签名可以通过纯鉴权。
func TestAuthenticateOpsAcceptsPrivateSource(t *testing.T) {
	req := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", []byte(`{"name":"admin"}`))
	req.RemoteAddr = "10.0.0.10:12345"
	if _, err := authenticateOpsWithClientIP(req, config.OpsConfig{Token: "ops-token"}, "10.0.0.10", time.Now()); err != nil {
		t.Fatalf("authenticateOpsWithClientIP() error = %v", err)
	}
}

// TestAuthenticateOpsRejectsPublicSource 验证公网来源不能因签名正确而绕过网络边界。
func TestAuthenticateOpsRejectsPublicSource(t *testing.T) {
	req := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", nil)
	if _, err := authenticateOpsWithClientIP(req, config.OpsConfig{Token: "ops-token"}, "8.8.8.8", time.Now()); err == nil {
		t.Fatal("期望公网来源返回错误，实际为 nil")
	}
}

// TestAuthenticateOpsRejectsMissingNonce 验证缺少 nonce 的旧协议请求被拒绝。
func TestAuthenticateOpsRejectsMissingNonce(t *testing.T) {
	req := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", nil)
	req.Header.Del(HeaderOpsNonce)
	if _, err := authenticateOpsWithClientIP(req, config.OpsConfig{Token: "ops-token"}, "10.0.0.10", time.Now()); err == nil {
		t.Fatal("期望缺少 nonce 返回错误，实际为 nil")
	}
}

// TestAuthenticateOpsRestoresBody 验证验签后业务 handler 仍能读取原始请求体。
func TestAuthenticateOpsRestoresBody(t *testing.T) {
	body := []byte(`{"name":"admin"}`)
	req := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", body)
	if _, err := authenticateOpsWithClientIP(req, config.OpsConfig{Token: "ops-token"}, "10.0.0.10", time.Now()); err != nil {
		t.Fatalf("authenticateOpsWithClientIP() error = %v", err)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("恢复后的请求体=%s，期望=%s", got, body)
	}
}

// TestAuthenticateOpsNonceTTLCoversFutureTimestamp 确保未来容差内的签名也会被防重放缓存覆盖到失效。
func TestAuthenticateOpsNonceTTLCoversFutureTimestamp(t *testing.T) {
	now := time.Now()
	signedAt := now.Add(4 * time.Minute)
	req := newAdminSignedOpsRequestAt("POST", "/internal/admin/init", "ops-token", nil, signedAt)
	auth, err := authenticateOpsWithClientIP(req, config.OpsConfig{Token: "ops-token"}, "10.0.0.10", now)
	if err != nil {
		t.Fatalf("authenticateOpsWithClientIP() error = %v", err)
	}
	want := time.Unix(signedAt.Unix(), 0).Add(opsSignatureWindow).Sub(now)
	if auth.TTL != want {
		t.Fatalf("nonce TTL=%s，期望=%s", auth.TTL, want)
	}
}

// TestOpsMiddlewareRejectsReplay 验证 Redis SET NX 只接受同一 nonce 一次。
func TestOpsMiddlewareRejectsReplay(t *testing.T) {
	useAdminOpsTestAppID(t, "admin-ops-replay")
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	cfg := config.Config{
		AppID: "admin-ops-replay",
		Ops:   config.OpsConfig{Token: "ops-token"},
	}
	middleware := NewOpsMiddleware(svc.NewServiceContext(cfg, svc.Dependencies{Rds: client}))
	handler := middleware.Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}, Ignore)

	first := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", nil)
	first.RemoteAddr = "10.0.0.10:12345"
	firstRecorder := httptest.NewRecorder()
	handler(firstRecorder, first)
	if firstRecorder.Code != http.StatusNoContent {
		t.Fatalf("首次请求状态码=%d，期望=%d", firstRecorder.Code, http.StatusNoContent)
	}

	replayed := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", nil)
	replayed.RemoteAddr = "10.0.0.10:12345"
	replayRecorder := httptest.NewRecorder()
	handler(replayRecorder, replayed)
	if replayRecorder.Code != http.StatusForbidden {
		t.Fatalf("重放请求状态码=%d，期望=%d", replayRecorder.Code, http.StatusForbidden)
	}
}

// TestOpsMiddlewareFailsClosedWithoutRedis 验证防重放缓存不可用时返回 503。
func TestOpsMiddlewareFailsClosedWithoutRedis(t *testing.T) {
	cfg := config.Config{Ops: config.OpsConfig{Token: "ops-token"}}
	middleware := NewOpsMiddleware(svc.NewServiceContext(cfg, svc.Dependencies{}))
	handler := middleware.Handle(func(http.ResponseWriter, *http.Request) {
		t.Fatal("Redis 不可用时不应进入业务 handler")
	}, Ignore)
	req := newAdminSignedOpsRequest("POST", "/internal/admin/init", "ops-token", nil)
	req.RemoteAddr = "10.0.0.10:12345"
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Redis 不可用状态码=%d，期望=%d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// useAdminOpsTestAppID 为防重放 key 注入测试命名空间。
func useAdminOpsTestAppID(t *testing.T, appID string) {
	t.Helper()
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
}

// newAdminSignedOpsRequest 构造完整的新协议运维请求。
func newAdminSignedOpsRequest(method string, target string, token string, body []byte) *http.Request {
	return newAdminSignedOpsRequestAt(method, target, token, body, time.Now())
}

// newAdminSignedOpsRequestAt 构造指定签名时间的运维请求。
func newAdminSignedOpsRequestAt(method string, target string, token string, body []byte, signedAt time.Time) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	timestamp := strconv.FormatInt(signedAt.Unix(), 10)
	bodyHash := opsBodySHA256(body)
	req.Header.Set(HeaderOpsToken, token)
	req.Header.Set(HeaderOpsTimestamp, timestamp)
	req.Header.Set(HeaderOpsNonce, adminTestOpsNonce)
	req.Header.Set(HeaderOpsBodySHA256, bodyHash)
	req.Header.Set(HeaderOpsSignature, signOpsRequest(token, method, req.URL.RequestURI(), timestamp, adminTestOpsNonce, bodyHash))
	return req
}
