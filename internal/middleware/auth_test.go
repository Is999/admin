package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"admin/common/codes"
	securitylogic "admin/internal/logic/security"
	"admin/internal/requestctx"
	"admin/internal/routealias"
)

// TestFailAdminAccessMapsPasswordReset 验证普通接口与文档接口共用强制改密响应映射。
func TestFailAdminAccessMapsPasswordReset(t *testing.T) {
	resp := httptest.NewRecorder()
	failAdminAccess(context.Background(), resp, securitylogic.ErrAdminPasswordResetRequired)
	if resp.Code != http.StatusOK {
		t.Fatalf("failAdminAccess() status = %d, want %d", resp.Code, http.StatusOK)
	}
	if !strings.Contains(resp.Body.String(), `"code":`+strconv.Itoa(codes.CheckPasswordReset)) {
		t.Fatalf("failAdminAccess() body = %s", resp.Body.String())
	}
}

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
	}, routealias.AuthLogin)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	resp := httptest.NewRecorder()
	handler(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("PublicHandle() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
}
