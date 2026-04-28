package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/security"
	"admin/internal/svc"
)

func TestSignatureMiddlewareRejectsRequestSignAll(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	err := middleware.verifyRequest(httptest.NewRequest(http.MethodPost, "/api/demo", nil), security.RouteSecurityPolicy{
		RequestSign: []string{security.SignFieldAll},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeMD5)
	if err == nil || !strings.Contains(err.Error(), "全量字段") {
		t.Fatalf("verifyRequest() error = %v, want full-field rejection", err)
	}
}

func TestSignatureMiddlewareRejectsOversizeRequestSignField(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	body := `{"username":"` + strings.Repeat("x", security.MaxSecurityFieldBytes+1) + `","sign":"demo"}`
	err := middleware.verifyRequest(httptest.NewRequest(http.MethodPost, "/api/demo", strings.NewReader(body)), security.RouteSecurityPolicy{
		RequestSign: []string{"username"},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeMD5)
	if err == nil || !strings.Contains(err.Error(), "长度超过上限") {
		t.Fatalf("verifyRequest() error = %v, want oversize field rejection", err)
	}
}

func TestSignatureMiddlewareRejectsResponseSignAll(t *testing.T) {
	middleware := NewSignatureMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	recorder := newBodyRecorder()
	_, _ = recorder.body.WriteString(`{"status":true,"data":{"token":"t","items":[1,2,3]}}`)
	_, err := middleware.signResponse(recorder, security.RouteSecurityPolicy{
		ResponseSign: []string{security.SignFieldAll},
	}, "demo-app", "trace", "1700000000", security.SignatureTypeMD5, httptest.NewRequest(http.MethodPost, "/api/demo", nil))
	if err == nil || !strings.Contains(err.Error(), "全量字段") {
		t.Fatalf("signResponse() error = %v, want full-field rejection", err)
	}
}

func TestDecodeAndValidateCipherParamsRejectsUndeclaredField(t *testing.T) {
	raw := security.EncodeCipherParams([]string{"profile"})
	_, err := decodeAndValidateCipherParams(raw, []string{"password"}, "请求")
	if err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("decodeAndValidateCipherParams() error = %v, want undeclared field rejection", err)
	}
}

func TestCryptoMiddlewareRejectsOversizeRequestCipherValue(t *testing.T) {
	middleware := NewCryptoMiddleware(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	req := httptest.NewRequest(http.MethodPost, "/api/demo", strings.NewReader(`{"password":"`+strings.Repeat("x", security.MaxSecurityFieldBytes+1)+`"}`))
	err := middleware.decryptRequest(req, []string{"password"}, noopCryptor{})
	if err == nil || !strings.Contains(err.Error(), "长度超过上限") {
		t.Fatalf("decryptRequest() error = %v, want oversize field rejection", err)
	}
}

type noopCryptor struct{}

func (noopCryptor) Encrypt(data string) (string, error) { return data, nil }
func (noopCryptor) Decrypt(data string) (string, error) { return data, nil }
