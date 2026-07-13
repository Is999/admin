package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeromicro/go-zero/core/logx"
)

// TestSignatureFailureLogDoesNotLeakRequestHeaders 验证签名失败日志不复制认证、Cookie 或密钥请求头值。
func TestSignatureFailureLogDoesNotLeakRequestHeaders(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buffer))
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	secrets := []string{"bearer-secret", "cookie-secret", "api-key-secret"}
	request.Header.Set("Authorization", secrets[0])
	request.Header.Set("Cookie", secrets[1])
	request.Header.Set("X-Api-Key", secrets[2])
	request.Header.Set("X-Trace-Id", "trace-present")
	request.Header.Set("X-Timestamp", "123")

	NewSignatureMiddleware(nil).fail(
		httptest.NewRecorder(),
		request,
		1,
		errors.New("bad signature"),
	)
	output := buffer.String()
	for _, secret := range secrets {
		if strings.Contains(output, secret) {
			t.Fatalf("签名失败日志泄露请求头秘密 %q: %s", secret, output)
		}
	}
	if strings.Contains(output, "request_headers") {
		t.Fatalf("签名失败日志不应复制完整请求头: %s", output)
	}
}
