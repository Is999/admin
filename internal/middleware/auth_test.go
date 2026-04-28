package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"admin/internal/requestctx"
)

// TestPublicHandleSetsRouteAliasBeforeDownstream 验证公开路由在进入下游处理前已经写入统一 route alias。
func TestPublicHandleSetsRouteAliasBeforeDownstream(t *testing.T) {
	authMw := &AuthMiddleware{}
	handler := authMw.PublicHandle(func(w http.ResponseWriter, r *http.Request) {
		meta := requestctx.FromContext(r.Context())
		if meta == nil {
			t.Fatalf("PublicHandle() request meta is nil")
		}
		if meta.Route != "auth.login" {
			t.Fatalf("PublicHandle() route = %q, want %q", meta.Route, "auth.login")
		}
		if meta.Method != http.MethodPost {
			t.Fatalf("PublicHandle() method = %q, want %q", meta.Method, http.MethodPost)
		}
		if meta.Path != "/api/auth/login" {
			t.Fatalf("PublicHandle() path = %q, want %q", meta.Path, "/api/auth/login")
		}
		w.WriteHeader(http.StatusNoContent)
	}, RouteAlias("auth.login"))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	resp := httptest.NewRecorder()
	handler(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("PublicHandle() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
}
