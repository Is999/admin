package docs

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sitedocs "admin/docs"
	"admin/internal/middleware"
	"admin/internal/requestctx"
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

// TestAPIDocsProxyPath 校验后台文档站 API 前缀会转换为 API 内网文档路径。
func TestAPIDocsProxyPath(t *testing.T) {
	tests := []struct {
		name     string // name 表示测试场景名称。
		path     string // path 表示请求路径。
		wantPath string // wantPath 表示期望重定向路径。
		wantOK   bool   // wantOK 表示期望是否成功。
	}{
		{name: "代理根路径", path: "/api/docs/api", wantOK: false},
		{name: "代理侧边栏", path: "/api/docs/api/_sidebar.md", wantPath: "/_sidebar.md", wantOK: true},
		{name: "接口规范", path: "/api/docs/api/接口文档/接口文档统一规范.md", wantPath: "/接口文档/接口文档统一规范.md", wantOK: true},
		{name: "编码路径", path: "/api/docs/api/%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3/%E5%89%8D%E5%8F%B0%E7%B3%BB%E7%BB%9F/%E8%AE%A4%E8%AF%81%E6%8E%A5%E5%8F%A3.md", wantPath: "/接口文档/前台系统/认证接口.md", wantOK: true},
		{name: "角色文档", path: "/api/docs/api/角色文档/后端开发/AI开发规范.md", wantPath: "/角色文档/后端开发/AI开发规范.md", wantOK: true},
		{name: "非代理路径", path: "/api/docs/接口文档/后台系统/权限管理接口.md", wantOK: false},
		{name: "路径穿越", path: "/api/docs/api/../角色文档/后端开发/AI开发规范.md", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOK := apiDocsProxyPath(tt.path)
			if gotOK != tt.wantOK || gotPath != tt.wantPath {
				t.Fatalf("apiDocsProxyPath() = (%q, %v), want (%q, %v)", gotPath, gotOK, tt.wantPath, tt.wantOK)
			}
		})
	}
}

// TestAPIDocsIndexStoredAsStaticAsset 校验前台 API 文档入口由 docs 静态资源维护。
func TestAPIDocsIndexStoredAsStaticAsset(t *testing.T) {
	content, err := fs.ReadFile(sitedocs.FS, "site/"+apiDocsIndexAssetPath)
	if err != nil {
		t.Fatalf("api docs index asset missing: %v", err)
	}
	for _, text := range []string{"前台 API 文档", "basePath: '/api/docs/api/'", "docsify.min.js"} {
		if !strings.Contains(string(content), text) {
			t.Fatalf("api docs index asset missing %q", text)
		}
	}
}

// TestAPIDocsIndexStandalone 校验前台 API 文档入口不复用 Admin 文档导航。
func TestAPIDocsIndexStandalone(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs/api", nil)
	recorder := httptest.NewRecorder()

	DocsSiteHandler(nil)(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, text := range []string{"前台 API 文档", "basePath: '/api/docs/api/'", "接口文档/前台系统/系统接口.md"} {
		if !strings.Contains(body, text) {
			t.Fatalf("api docs index missing %q", text)
		}
	}
	for _, text := range []string{"后台系统", "角色文档", "功能模块"} {
		if strings.Contains(body, text) {
			t.Fatalf("api docs index should not contain admin nav %q", text)
		}
	}
}

// TestAPIDocsSidebarRequiresAPIProxy 校验前台 API 侧边栏不再由 Admin 本地硬编码输出。
func TestAPIDocsSidebarRequiresAPIProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs/api/_sidebar.md", nil)
	recorder := httptest.NewRecorder()

	DocsSiteHandler(nil)(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}
