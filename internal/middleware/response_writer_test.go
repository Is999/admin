package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"admin/internal/svc"
)

// TestCryptoMiddlewarePassesThroughRoutesWithoutCipherPolicy 校验普通和下载响应不会进入整包缓冲。
func TestCryptoMiddlewarePassesThroughRoutesWithoutCipherPolicy(t *testing.T) {
	target := httptest.NewRecorder()
	streamedInsideHandler := false
	handler := NewCryptoMiddleware(svc.NewServiceContext(securityMiddlewareConfig(1, 1), svc.Dependencies{})).Handle(
		func(w http.ResponseWriter, _ *http.Request) {
			if _, ok := w.(http.Flusher); !ok {
				t.Fatal("无加密策略时必须保留底层 ResponseWriter 的流式接口")
			}
			_, _ = w.Write([]byte("first-chunk"))
			streamedInsideHandler = target.Body.String() == "first-chunk"
		},
	)

	handler(target, httptest.NewRequest(http.MethodGet, "/api/download", nil))

	if !streamedInsideHandler {
		t.Fatal("无加密策略时响应首块必须在业务处理器返回前写入底层 ResponseWriter")
	}
}

// TestBodyRecorderKeepsFirstStatus 校验缓冲响应只接受首个 HTTP 状态码。
func TestBodyRecorderKeepsFirstStatus(t *testing.T) {
	recorder := newBodyRecorder()
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)
	_, _ = recorder.Write([]byte("ok"))

	if recorder.status != http.StatusCreated {
		t.Fatalf("期望保留首个状态码 %d，实际 %d", http.StatusCreated, recorder.status)
	}
}

// TestStatusRecorderKeepsFirstStatusAndUnwraps 校验访问日志包装器保持标准响应语义。
func TestStatusRecorderKeepsFirstStatusAndUnwraps(t *testing.T) {
	base := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: base, status: http.StatusOK}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)
	_, _ = recorder.Write([]byte("ok"))

	if recorder.status != http.StatusCreated || base.Code != http.StatusCreated {
		t.Fatalf("期望首个状态码 %d，实际 recorder=%d base=%d", http.StatusCreated, recorder.status, base.Code)
	}
	if recorder.bytes != 2 {
		t.Fatalf("期望记录 2 字节，实际 %d", recorder.bytes)
	}
	if recorder.Unwrap() != base {
		t.Fatal("期望 Unwrap 返回底层 ResponseWriter")
	}
}
