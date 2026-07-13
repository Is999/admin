package auth

import (
	"net/http/httptest"
	"testing"
)

// TestRouteSpecsExposeNoPasswordVerificationBypass 锁定公开认证面只保留验证码登录，不再暴露可签发普通会话的预校验入口。
func TestRouteSpecsExposeNoPasswordVerificationBypass(t *testing.T) {
	for _, spec := range RouteSpecs() {
		if spec.Path == "/api/auth/verify-account" {
			t.Fatal("公开认证路由不得注册可绕过验证码的 verify-account 入口")
		}
	}
}

// TestSetCaptchaNoStoreHeaders 验证一次性验证码不会被浏览器或中间代理缓存。
func TestSetCaptchaNoStoreHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	setCaptchaNoStoreHeaders(recorder)

	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := recorder.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}
