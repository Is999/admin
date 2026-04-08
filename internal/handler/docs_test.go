package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"admin_cron/internal/middleware"
	"admin_cron/internal/requestctx"
)

// TestDocsSessionCookieLimitsScope 校验文档会话 cookie 只挂在文档路径下。
func TestDocsSessionCookieLimitsScope(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/docs/session", nil)
	cookie := docsSessionCookie(req, "token-1")

	if cookie.Name != middleware.DocsSessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, middleware.DocsSessionCookieName)
	}
	if cookie.Path != middleware.DocsSessionCookiePath {
		t.Fatalf("cookie path = %q, want %q", cookie.Path, middleware.DocsSessionCookiePath)
	}
	if !cookie.HttpOnly {
		t.Fatal("docs session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want %v", cookie.SameSite, http.SameSiteLaxMode)
	}
}

// TestDocsSessionCookieSecureBehindProxy 校验 HTTPS 代理头会启用 Secure 属性。
func TestDocsSessionCookieSecureBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/docs/session", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	if !docsSessionCookie(req, "token-1").Secure {
		t.Fatal("docs session cookie should be Secure when X-Forwarded-Proto=https")
	}
}

// TestDocsSessionHandlerSetsCookie 校验已鉴权请求会写入文档会话 cookie。
func TestDocsSessionHandlerSetsCookie(t *testing.T) {
	ctx, _ := requestctx.New(httptest.NewRequest(http.MethodPost, "/api/docs/session", nil).Context())
	requestctx.SetAccessToken(ctx, "token-1")
	req := httptest.NewRequest(http.MethodPost, "/api/docs/session", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	DocsSessionHandler()(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusOK)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	if cookies[0].Name != middleware.DocsSessionCookieName || cookies[0].Value != "token-1" {
		t.Fatalf("unexpected docs cookie: %+v", cookies[0])
	}
}
