package handler

import (
	"net/http"
	"testing"

	"admin_cron/internal/config"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// TestRegisterHandlersRegistersExpectedRoutes 确认裁剪业务模块后，主体框架路由仍然完整注册。
func TestRegisterHandlersRegistersExpectedRoutes(t *testing.T) {
	server := rest.MustNewServer(rest.RestConf{
		Host: "127.0.0.1",
		Port: 0,
	})
	defer server.Stop()

	RegisterHandlers(server, svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	routes := server.Routes()
	if len(routes) != 125 {
		t.Fatalf("expected 125 routes, got %d", len(routes))
	}

	expected := []string{
		http.MethodGet + " /api/health",
		http.MethodGet + " /api/live",
		http.MethodGet + " /api/ready",
		http.MethodGet + " /api/metrics",
		http.MethodPost + " /api/docs/session",
		http.MethodGet + " /api/docs",
		http.MethodGet + " /api/docs/:path",
		http.MethodGet + " /api/docs/:path/:sub",
		http.MethodGet + " /api/docs/:path/:sub/:file",
		http.MethodGet + " /api/auth/captcha",
		http.MethodPost + " /api/auth/login",
		http.MethodGet + " /api/admin/mine",
		http.MethodPost + " /api/admin/checkMfaSecure",
		http.MethodGet + " /api/admins",
		http.MethodPost + " /api/admins",
		http.MethodPost + " /api/admins/exports",
		http.MethodGet + " /api/admins/exports/status/:jobId",
		http.MethodGet + " /api/admins/exports/download/:jobId",
		http.MethodPost + " /api/file-transfer/upload/init",
		http.MethodGet + " /api/file-transfer/upload/status",
		http.MethodPut + " /api/file-transfer/upload/chunk",
		http.MethodGet + " /api/file-transfer/download",
		http.MethodPost + " /api/file-transfer/upload/complete",
		http.MethodGet + " /api/file-transfer/access",
		http.MethodGet + " /api/admins/:id",
		http.MethodPatch + " /api/admins/:id",
		http.MethodDelete + " /api/admins/:id",
		http.MethodPatch + " /api/admins/status/:id",
		http.MethodPost + " /api/admins/password/reset/:id",
		http.MethodPost + " /api/admins/initial-state/reset/:id",
		http.MethodGet + " /api/admins/roles/:id",
		http.MethodPatch + " /api/admins/roles/:id",
		http.MethodPatch + " /api/admins/mfa-status/:id",
		http.MethodGet + " /api/admins/mfa-secret-key-url/:id",
		http.MethodPost + " /api/admin/updatePassword",
		http.MethodPost + " /api/admin/refreshMfaSecretKey",
		http.MethodGet + " /api/roles/tree",
		http.MethodGet + " /api/roles/tree-options",
		http.MethodPatch + " /api/roles/permissions/:id",
		http.MethodGet + " /api/permissions/max-uuid",
		http.MethodPatch + " /api/permissions/:id",
		http.MethodGet + " /api/dicts/export",
		http.MethodPost + " /api/dicts/import",
		http.MethodPost + " /api/dicts/cache/refresh/:uuid",
		http.MethodPost + " /api/caches/refresh-all",
		http.MethodPost + " /api/caches/warmup",
		http.MethodGet + " /api/admin-logs",
		http.MethodGet + " /api/admin-messages",
		http.MethodGet + " /api/admin-messages/sent",
		http.MethodGet + " /api/admin-messages/:id/receivers",
		http.MethodGet + " /api/admin-messages/unread-count",
		http.MethodGet + " /api/admin-messages/notifications",
		http.MethodPatch + " /api/admin-messages/read",
		http.MethodPost + " /api/admin-messages/delete",
		http.MethodPost + " /api/admin-messages/send",
		http.MethodPost + " /api/admin-messages/handle",
		http.MethodGet + " /api/secret-keys/:id",
		http.MethodPost + " /api/secret-keys/validate",
		http.MethodPost + " /api/secret-keys/self-check/:uuid",
		http.MethodPost + " /api/security/debug/sign",
		http.MethodPost + " /api/security/debug/verify",
		http.MethodPost + " /api/security/debug/encrypt",
		http.MethodPost + " /api/security/debug/decrypt",
		http.MethodPost + " /api/tasks",
		http.MethodGet + " /api/tasks/overview",
		http.MethodPost + " /api/tasks/workflows",
		http.MethodGet + " /api/tasks/workflows/:workflowId",
		http.MethodGet + " /api/tasks/queues",
		http.MethodGet + " /api/tasks/registry/task-types",
		http.MethodGet + " /api/tasks/registry/workflows",
		http.MethodGet + " /api/tasks/config-reload",
		http.MethodGet + " /api/tasks/config-reload/items",
		http.MethodPost + " /api/tasks/config-reload",
		http.MethodGet + " /api/tasks/:taskId",
		http.MethodPost + " /api/tasks/queues/pause/:queue",
		http.MethodPost + " /api/tasks/run/:taskId",
		http.MethodDelete + " /api/tasks/:taskId",
		http.MethodPost + " /api/tasks/queues/resume/:queue",
		http.MethodGet + " /api/collector/overview",
		http.MethodGet + " /api/collector/tasks",
		http.MethodPost + " /api/collector/run",
		http.MethodPost + " /api/collector/tasks/retry",
		http.MethodPost + " /api/user-tags/workflows",
		http.MethodPost + " /api/user-tags/recalculations",
		http.MethodPost + " /api/user-tags/workflow-lease/release",
		http.MethodPost + " /internal/auth/init-admin-bootstrap",
		http.MethodPost + " /internal/user-tags/workflows",
		http.MethodPost + " /internal/user-tags/recalculations",
	}

	routeSet := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, ok := routeSet[key]; ok {
			t.Fatalf("路由重复注册: %s", key)
		}
		routeSet[key] = struct{}{}
	}

	// 重点校验登录、审计、任务和用户标签关键路由，防止裁剪时路径漂移。
	for _, item := range expected {
		if _, ok := routeSet[item]; !ok {
			t.Fatalf("missing route %s", item)
		}
	}

	taskDetailRoute := http.MethodGet + " /api/tasks/:taskId"
	for _, fixedRoute := range []string{
		http.MethodGet + " /api/tasks/overview",
		http.MethodGet + " /api/tasks/queues",
		http.MethodGet + " /api/tasks/registry/task-types",
		http.MethodGet + " /api/tasks/registry/workflows",
		http.MethodGet + " /api/tasks/config-reload",
		http.MethodGet + " /api/tasks/config-reload/items",
	} {
		if routeIndex(routes, fixedRoute) > routeIndex(routes, taskDetailRoute) {
			t.Fatalf("固定任务路由 %s 必须排在参数路由 %s 之前", fixedRoute, taskDetailRoute)
		}
	}
}

// routeIndex 返回路由在 go-zero 注册顺序中的下标，用于保护固定路由优先于参数路由。
func routeIndex(routes []rest.Route, routeKey string) int {
	for index, route := range routes {
		if route.Method+" "+route.Path == routeKey {
			return index
		}
	}
	return len(routes) + 1
}

// TestRegisterHandlersAppendsRouteModules 确保外部路由模块可以通过统一入口追加注册。
func TestRegisterHandlersAppendsRouteModules(t *testing.T) {
	server := rest.MustNewServer(rest.RestConf{
		Host: "127.0.0.1",
		Port: 0,
	})
	defer server.Stop()

	module := NewRouteModuleFunc("custom", func(scope *RouteScope) {
		scope.Server.AddRoute(rest.Route{
			Method: http.MethodGet,
			Path:   "/api/custom",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r
				w.WriteHeader(http.StatusNoContent)
			}),
		})
	})
	RegisterHandlers(server, svc.NewServiceContext(config.Config{}, svc.Dependencies{}), module)

	routeSet := make(map[string]struct{}, len(server.Routes()))
	for _, route := range server.Routes() {
		routeSet[route.Method+" "+route.Path] = struct{}{}
	}
	if _, ok := routeSet[http.MethodGet+" /api/custom"]; !ok {
		t.Fatal("期望外部路由模块已注册")
	}
}
