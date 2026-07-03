package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"
)

// TestDocsRouteAliasForPath 验证文档站按具体文档映射权限别名。
func TestDocsRouteAliasForPath(t *testing.T) {
	cases := []struct {
		name string // name 表示测试场景名称。
		path string // path 表示请求路径。
		want string // want 表示期望结果。
	}{
		{name: "首页", path: "/api/docs", want: "docs.index"},
		{name: "后端开发文档", path: "/api/docs/角色文档/后端开发/AI开发规范.md", want: "docs.file.角色文档/后端开发/AI开发规范.md"},
		{name: "前台API接口文档", path: "/api/docs/api/接口文档/前台系统/认证接口.md", want: "docs.file.api/接口文档/前台系统/认证接口.md"},
	}
	for _, tc := range cases {
		if got := string(docsRouteAliasForPath(tc.path)); got != tc.want {
			t.Fatalf("%s docsRouteAliasForPath(%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}

// TestDocsJwtMiddlewareNonProdAnonymousRequiresAuth 校验开发和测试环境无凭证访问文档也必须鉴权。
func TestDocsJwtMiddlewareNonProdAnonymousRequiresAuth(t *testing.T) {
	for _, mode := range []string{"dev", "test"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{}
			cfg.Mode = mode
			svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{})
			req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
			recorder := httptest.NewRecorder()
			called := false

			DocsJwtMiddleware(svcCtx)(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})(recorder, req)

			if called {
				t.Fatalf("%s anonymous docs request should not reach next handler", mode)
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

// TestDocsJwtMiddlewareDevCredentialRequiresAuth 校验开发环境带文档凭证时不能绕过管理员鉴权。
func TestDocsJwtMiddlewareDevCredentialRequiresAuth(t *testing.T) {
	cfg := config.Config{JwtSecret: "test-secret"}
	cfg.Mode = "dev"
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.AddCookie(&http.Cookie{Name: DocsSessionCookieName, Value: "bad-token"})
	recorder := httptest.NewRecorder()
	called := false

	DocsJwtMiddleware(svcCtx)(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})(recorder, req)

	if called {
		t.Fatal("dev docs request with credential should not bypass auth")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
