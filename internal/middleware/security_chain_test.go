package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	codes "admin/common/codes"
	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/requestctx"
	"admin/internal/routealias"
	"admin/internal/security"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestSignatureMiddlewareRejectsRequestSignAll 验证对应场景符合预期。
func TestSignatureMiddlewareRejectsRequestSignAll(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	err := middleware.verifyRequest(httptest.NewRequest(http.MethodPost, "/api/demo", nil), security.RouteSecurityPolicy{
		RequestSign: []string{security.SignFieldAll},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeAES)
	if err == nil || !strings.Contains(err.Error(), "全量字段") {
		t.Fatalf("verifyRequest() error = %v, want full-field rejection", err)
	}
}

// TestSignatureMiddlewareRejectsOversizeRequestSignField 验证对应场景符合预期。
func TestSignatureMiddlewareRejectsOversizeRequestSignField(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	body := `{"username":"` + strings.Repeat("x", security.MaxSecurityFieldBytes+1) + `","sign":"demo"}`
	err := middleware.verifyRequest(httptest.NewRequest(http.MethodPost, "/api/demo", strings.NewReader(body)), security.RouteSecurityPolicy{
		RequestSign: []string{"username"},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeAES)
	if err == nil || !strings.Contains(err.Error(), "长度超过上限") {
		t.Fatalf("verifyRequest() error = %v, want oversize field rejection", err)
	}
}

// TestSignatureMiddlewareRejectsResponseSignAll 验证对应场景符合预期。
func TestSignatureMiddlewareRejectsResponseSignAll(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	recorder := newBodyRecorder()
	_, _ = recorder.body.WriteString(`{"status":true,"data":{"token":"t","items":[1,2,3]}}`)
	_, err := middleware.signResponse(recorder, security.RouteSecurityPolicy{
		ResponseSign: []string{security.SignFieldAll},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeAES, httptest.NewRequest(http.MethodPost, "/api/demo", nil))
	if err == nil || !strings.Contains(err.Error(), "全量字段") {
		t.Fatalf("signResponse() error = %v, want full-field rejection", err)
	}
}

// TestSignatureMiddlewareRejectsRemovedMD5Type 验证旧 MD5 签名不会静默降级。
func TestSignatureMiddlewareRejectsRemovedMD5Type(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	_, _, err := middleware.signer(httptest.NewRequest(http.MethodPost, "/api/demo", nil), "demo-app", security.NormalizeSignatureType("MD5"), true)
	if err == nil || !strings.Contains(err.Error(), "签名方式不合法") {
		t.Fatalf("signer(MD5) error = %v, want invalid signature type", err)
	}
}

// TestSignatureMiddlewareMarkRequestVerifiedFailsClosedOnAppIDMismatch 验证对应场景符合预期。
func TestSignatureMiddlewareMarkRequestVerifiedFailsClosedOnAppIDMismatch(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-b"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client}))
	err := middleware.markRequestVerified(httptest.NewRequest(http.MethodPost, "/api/demo", nil), "site-a", "trace-1")
	if err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("markRequestVerified() error = %v, want app_id mismatch", err)
	}
	if server.Exists("app:site-a:signature:request:trace-1") || server.Exists("app:site-b:signature:request:trace-1") {
		t.Fatal("app_id 不一致时不应写入签名防重放缓存")
	}
}

// TestDecodeAndValidateCipherParamsRejectsUndeclaredField 验证对应场景符合预期。
func TestDecodeAndValidateCipherParamsRejectsUndeclaredField(t *testing.T) {
	raw := security.EncodeCipherParams([]string{"profile"})
	_, err := decodeAndValidateCipherParams(raw, []string{"password"}, "请求")
	if err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("decodeAndValidateCipherParams() error = %v, want undeclared field rejection", err)
	}
}

// TestCryptoMiddlewareRejectsOversizeRequestCipherValue 验证对应场景符合预期。
func TestCryptoMiddlewareRejectsOversizeRequestCipherValue(t *testing.T) {
	middleware := NewCryptoMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	req := httptest.NewRequest(http.MethodPost, "/api/demo", strings.NewReader(`{"password":"`+strings.Repeat("x", security.MaxSecurityFieldBytes+1)+`"}`))
	err := middleware.decryptRequest(req, []string{"password"}, noopCryptor{})
	if err == nil || !strings.Contains(err.Error(), "长度超过上限") {
		t.Fatalf("decryptRequest() error = %v, want oversize field rejection", err)
	}
}

// TestSignatureMiddlewareDisabledSkipsTraceHeaders 确保仅开启加密时，签名中间件不要求 trace_id 和 timestamp。
func TestSignatureMiddlewareDisabledSkipsTraceHeaders(t *testing.T) {
	svcCtx := svc.NewServiceContext(securityMiddlewareConfig(0, 1), svc.Dependencies{})
	called := false
	handler := NewSignatureMiddleware(svcCtx).Handle(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}, routealias.AuthLogin)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.Header.Set("X-App-Id", base64.StdEncoding.EncodeToString([]byte("site-a")))
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if !called {
		t.Fatal("签名关闭时请求应进入业务处理器")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

// TestCryptoMiddlewareDisabledSkipsCipherPolicy 确保仅开启签名时，加密中间件不加工响应。
func TestCryptoMiddlewareDisabledSkipsCipherPolicy(t *testing.T) {
	svcCtx := svc.NewServiceContext(securityMiddlewareConfig(1, 0), svc.Dependencies{})
	called := false
	handler := NewCryptoMiddleware(svcCtx).Handle(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.Header.Set("X-App-Id", base64.StdEncoding.EncodeToString([]byte("site-a")))
	request = bindPublicRequestMeta(request, routealias.AuthLogin, svcCtx)
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if !called {
		t.Fatal("加密关闭时请求应进入业务处理器")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if recorder.Header().Get("X-Cipher") != "" || recorder.Header().Get("X-Crypto") != "" {
		t.Fatal("加密关闭时响应不应包含加密协议头")
	}
}

// TestSecurityMiddlewareFailuresUseCodeHTTPStatus 确保安全失败响应遵循统一业务码 HTTP 契约。
func TestSecurityMiddlewareFailuresUseCodeHTTPStatus(t *testing.T) {
	writers := []struct {
		name string                                        // name 表示测试场景。
		fail func(http.ResponseWriter, *http.Request, int) // fail 写出待校验安全失败响应。
	}{
		{
			name: "signature",
			fail: func(w http.ResponseWriter, r *http.Request, code int) {
				NewSignatureMiddleware(nil).fail(w, r, code, errors.New("signature failure"))
			},
		},
		{
			name: "crypto",
			fail: func(w http.ResponseWriter, r *http.Request, code int) {
				NewCryptoMiddleware(nil).fail(w, r, code, errors.New("crypto failure"))
			},
		},
	}
	codeCases := []struct {
		name string // name 表示业务码场景。
		code int    // code 表示待映射业务码。
		want int    // want 表示期望 HTTP 状态码。
	}{
		{name: "param", code: codes.ParamError, want: http.StatusBadRequest},
		{name: "auth", code: codes.AuthFailed, want: http.StatusUnauthorized},
		{name: "internal", code: codes.InternalError, want: http.StatusInternalServerError},
	}
	for _, writer := range writers {
		for _, codeCase := range codeCases {
			t.Run(writer.name+"_"+codeCase.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
				recorder := httptest.NewRecorder()
				setStaleSecurityHeaders(recorder.Header())
				writer.fail(recorder, request, codeCase.code)
				if recorder.Code != codeCase.want {
					t.Fatalf("status = %d, want %d", recorder.Code, codeCase.want)
				}
				assertSecurityHeadersEmpty(t, recorder.Header())
			})
		}
	}
}

// TestSecurityValidationFailuresHaveSameExternalResponse 确保内层验签失败不会再被外层加密加工。
func TestSecurityValidationFailuresHaveSameExternalResponse(t *testing.T) {
	svcCtx := svc.NewServiceContext(securityMiddlewareConfig(1, 1), svc.Dependencies{})
	handler := NewAuthMiddleware(svcCtx).PublicHandle(func(http.ResponseWriter, *http.Request) {
		t.Fatal("安全校验失败后不应进入业务处理器")
	}, routealias.AuthLogin)
	cipherObj, err := security.NewAESCipher("1234567890123456", "abcdefghijklmnop")
	if err != nil {
		t.Fatalf("NewAESCipher() error = %v", err)
	}
	encryptedPassword, err := cipherObj.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt(password) error = %v", err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	decryptFailure := performAdminSecurityRequest(handler, timestamp, "invalid-ciphertext")
	signatureFailure := performAdminSecurityRequest(handler, timestamp, encryptedPassword)

	if decryptFailure.Code != http.StatusUnauthorized || signatureFailure.Code != http.StatusUnauthorized {
		t.Fatalf("status = decrypt:%d signature:%d, want both %d", decryptFailure.Code, signatureFailure.Code, http.StatusUnauthorized)
	}
	if decryptFailure.Body.String() != signatureFailure.Body.String() {
		t.Fatalf("body differs:\ndecrypt=%s\nsignature=%s", decryptFailure.Body.String(), signatureFailure.Body.String())
	}
	if !reflect.DeepEqual(decryptFailure.Header(), signatureFailure.Header()) {
		t.Fatalf("headers differ:\ndecrypt=%v\nsignature=%v", decryptFailure.Header(), signatureFailure.Header())
	}
	var response struct {
		Code int `json:"code"` // Code 保存对外业务码。
	}
	if err := json.Unmarshal(signatureFailure.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if response.Code != codes.AuthFailed {
		t.Fatalf("response code = %d, want %d", response.Code, codes.AuthFailed)
	}
	assertSecurityHeadersEmpty(t, decryptFailure.Header())
	assertSecurityHeadersEmpty(t, signatureFailure.Header())
}

// securityMiddlewareConfig 返回安全中间件测试使用的配置文件秘钥。
func securityMiddlewareConfig(signStatus int, cryptoStatus int) config.Config {
	return config.Config{
		AppID: "site-a",
		Security: config.SecurityConfig{SecretKey: config.SecuritySecretKeyConfig{
			KeyVersion:   "v1",
			AESKey:       "1234567890123456",
			AESIV:        "abcdefghijklmnop",
			SignStatus:   signStatus,
			CryptoStatus: cryptoStatus,
		}},
	}
}

// performAdminSecurityRequest 执行登录安全校验失败请求。
func performAdminSecurityRequest(handler http.HandlerFunc, timestamp string, password string) *httptest.ResponseRecorder {
	body := `{"username":"demo","password":"` + password + `","sign":"invalid-signature"}`
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-App-Id", base64.StdEncoding.EncodeToString([]byte("site-a")))
	request.Header.Set("X-Cipher", security.EncodeCipherParams([]string{"password"}))
	request.Header.Set("X-Crypto", security.CryptoTypeAES)
	request.Header.Set("X-Signature", security.SignatureTypeAES)
	request.Header.Set(requestctx.HeaderTraceID, "security-failure-trace")
	request.Header.Set(requestctx.HeaderTimestamp, timestamp)
	recorder := httptest.NewRecorder()
	setStaleSecurityHeaders(recorder.Header())
	handler(recorder, request)
	return recorder
}

// setStaleSecurityHeaders 写入用于验证清理行为的旧安全响应头。
func setStaleSecurityHeaders(header http.Header) {
	for _, name := range []string{"X-Cipher", "X-Crypto", secretKeyVersionHeader, "X-Signature"} {
		header.Set(name, "stale-security-value")
	}
}

// assertSecurityHeadersEmpty 确保失败响应未残留签名或加密响应头。
func assertSecurityHeadersEmpty(t *testing.T, header http.Header) {
	t.Helper()
	for _, name := range []string{"X-Cipher", "X-Crypto", secretKeyVersionHeader, "X-Signature"} {
		if value := header.Get(name); value != "" {
			t.Fatalf("header %s = %q, want empty", name, value)
		}
	}
}

// noopCryptor 表示测试使用的辅助结构。
type noopCryptor struct{}

// Encrypt 表示测试辅助逻辑。
func (noopCryptor) Encrypt(data string) (string, error) { return data, nil }

// Decrypt 表示测试辅助逻辑。
func (noopCryptor) Decrypt(data string) (string, error) { return data, nil }
