package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"admin/common/codes"
	"admin/common/runtimecfg"
	"admin/helper"
	"admin/internal/config"
	"admin/internal/routealias"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestDocsRequestPermissionKeys 验证文档请求同时映射入口路由与精确正文资源。
func TestDocsRequestPermissionKeys(t *testing.T) {
	cases := []struct {
		name          string                 // name 表示测试场景名称。
		path          string                 // path 表示请求路径。
		wantEntry     string                 // wantEntry 表示期望入口权限。
		wantResource  routealias.DocResource // wantResource 表示期望正文资源。
		needsResource bool                   // needsResource 表示请求是否必须进入精确资源校验。
		hasResource   bool                   // hasResource 表示是否需要精确正文权限。
	}{
		{name: "首页", path: "/api/docs", wantEntry: "docs.index"},
		{name: "共享静态资源", path: "/api/docs/vendor/docsify/vue.css", wantEntry: string(routealias.Ignore)},
		{name: "后端开发文档", path: "/api/docs/角色文档/后端开发/AI开发规范.md", wantEntry: "docs.index", wantResource: routealias.DocResource{Site: "admin", Path: "角色文档/后端开发/AI开发规范.md"}, needsResource: true, hasResource: true},
		{name: "前台API接口文档", path: "/api/docs/api/接口文档/前台系统/认证接口.md", wantEntry: "docs.api_service.index", wantResource: routealias.DocResource{Site: "api", Path: "接口文档/前台系统/认证接口.md"}, needsResource: true, hasResource: true},
		{name: "二次编码穿越", path: httptest.NewRequest(http.MethodGet, "/api/docs/%252e%252e/文档首页.md", nil).URL.Path, wantEntry: "docs.index", needsResource: true},
	}
	for _, tc := range cases {
		if got := string(routealias.DocsEntryAliasForPath(tc.path)); got != tc.wantEntry {
			t.Fatalf("%s DocsEntryAliasForPath(%q) = %q, want %q", tc.name, tc.path, got, tc.wantEntry)
		}
		if got := routealias.DocsPathNeedsResourcePermission(tc.path); got != tc.needsResource {
			t.Fatalf("%s DocsPathNeedsResourcePermission(%q) = %t, want %t", tc.name, tc.path, got, tc.needsResource)
		}
		resource, ok := routealias.DocsResourceForPath(tc.path)
		if ok != tc.hasResource || resource != tc.wantResource {
			t.Fatalf("%s DocsResourceForPath(%q) = (%+v, %t), want (%+v, %t)", tc.name, tc.path, resource, ok, tc.wantResource, tc.hasResource)
		}
	}
}

// TestDocsJwtMiddlewareReturnsServiceUnavailableWithoutRedis 验证有效 JWT 遇到 Redis 故障时返回 503，而不是误报 token 无效。
func TestDocsJwtMiddlewareReturnsServiceUnavailableWithoutRedis(t *testing.T) {
	const secret = "test-secret"
	svcCtx := svc.NewServiceContext(config.Config{JwtSecret: secret}, svc.Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.AddCookie(&http.Cookie{
		Name:  DocsSessionCookieName,
		Value: testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour)),
	})
	recorder := httptest.NewRecorder()

	DocsJwtMiddleware(svcCtx)(func(http.ResponseWriter, *http.Request) {
		t.Fatal("Redis不可用时不应进入文档处理器")
	})(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("http status=%d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var response helper.ResponseJSON
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.Code != codes.RedisUnavailable {
		t.Fatalf("business code=%d, want %d", response.Code, codes.RedisUnavailable)
	}
}

// TestDocsJwtMiddlewareTreatsMissingSessionAsUnauthorized 验证会话缺失仍按退出或撤销返回 401。
func TestDocsJwtMiddlewareTreatsMissingSessionAsUnauthorized(t *testing.T) {
	const secret = "test-secret"
	previousRuntime := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() { runtimecfg.Restore(previousRuntime) })
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", JwtSecret: secret}, svc.Dependencies{Rds: client})
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	req.AddCookie(&http.Cookie{
		Name:  DocsSessionCookieName,
		Value: testAdminToken(t, secret, 1, "admin", time.Now().Add(time.Hour)),
	})
	recorder := httptest.NewRecorder()

	DocsJwtMiddleware(svcCtx)(func(http.ResponseWriter, *http.Request) {
		t.Fatal("会话缺失时不应进入文档处理器")
	})(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("http status=%d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var response helper.ResponseJSON
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.Code != codes.Unauthorized {
		t.Fatalf("business code=%d, want %d", response.Code, codes.Unauthorized)
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
