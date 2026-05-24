package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
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
	contracts := DefaultRouteContracts()
	if len(routes) != len(contracts) {
		t.Fatalf("expected %d routes, got %d", len(contracts), len(routes))
	}

	routeSet := make(map[string]struct{}, len(routes))
	for index, route := range routes {
		key := route.Method + " " + route.Path
		if _, ok := routeSet[key]; ok {
			t.Fatalf("路由重复注册: %s", key)
		}
		routeSet[key] = struct{}{}
		if want := contracts[index].Key(); key != want {
			t.Fatalf("路由注册顺序或契约不一致: index=%d got=%s want=%s", index, key, want)
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

// TestBuiltinRouteModuleSpecsValid 确保内置路由模块规格字段完整且能派生真实模块。
func TestBuiltinRouteModuleSpecsValid(t *testing.T) {
	specs := BuiltinRouteModuleSpecs()
	if len(specs) == 0 {
		t.Fatal("内置路由模块规格不能为空")
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			t.Fatalf("内置路由模块规格缺少名称: %+v", spec)
		}
		if strings.TrimSpace(spec.File) == "" || strings.TrimSpace(spec.Method) == "" || strings.TrimSpace(spec.Description) == "" {
			t.Fatalf("内置路由模块规格说明不完整: %+v", spec)
		}
		if spec.Routes == nil {
			t.Fatalf("内置路由模块规格缺少路由规格函数: %s", spec.Name)
		}
		if len(spec.Routes()) == 0 {
			t.Fatalf("内置路由模块规格缺少路由: %s", spec.Name)
		}
		if _, ok := seen[spec.Name]; ok {
			t.Fatalf("内置路由模块规格名称重复: %s", spec.Name)
		}
		seen[spec.Name] = struct{}{}
	}
}

// TestDefaultRouteContractsAreSelfConsistent 确保契约清单具备最小治理信息。
func TestDefaultRouteContractsAreSelfConsistent(t *testing.T) {
	seen := make(map[string]struct{})
	for _, contract := range DefaultRouteContracts() {
		if contract.Module == "" {
			t.Fatalf("路由契约缺少模块: %+v", contract)
		}
		if contract.Method == "" || contract.Path == "" {
			t.Fatalf("路由契约缺少 method/path: %+v", contract)
		}
		if contract.Description == "" {
			t.Fatalf("路由契约缺少描述: %s", contract.Key())
		}
		if _, ok := seen[contract.Key()]; ok {
			t.Fatalf("路由契约重复: %s", contract.Key())
		}
		seen[contract.Key()] = struct{}{}
		if strings.HasPrefix(contract.Path, "/internal/") && contract.Access != RouteAccessInternal {
			t.Fatalf("内网路由必须标记 internal: %+v", contract)
		}
		if contract.Access == RouteAccessInternal && !strings.HasPrefix(contract.Path, "/internal/") {
			t.Fatalf("internal 契约路径必须使用 /internal 前缀: %+v", contract)
		}
		if contract.Access == RouteAccessDocs && !strings.HasPrefix(contract.Path, "/api/docs") {
			t.Fatalf("docs 契约路径必须使用 /api/docs 前缀: %+v", contract)
		}
	}
}

// TestDefaultRouteContractsAreDocumented 确保真实路由路径在接口文档站中可检索。
func TestDefaultRouteContractsAreDocumented(t *testing.T) {
	docs := readDocsSiteMarkdown(t, filepath.Join("..", "..", "docs", "site"))
	for _, contract := range DefaultRouteContracts() {
		if !strings.Contains(docs, contract.Path) {
			t.Fatalf("接口文档缺少路由模板: %s", contract.Key())
		}
	}
}

func readDocsSiteMarkdown(t *testing.T, root string) string {
	t.Helper()
	var builder strings.Builder
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return errors.Tag(err)
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return errors.Tag(err)
		}
		builder.Write(body)
		builder.WriteByte('\n')
		return nil
	}); err != nil {
		t.Fatalf("读取接口文档失败: %v", err)
	}
	return builder.String()
}

// TestDefaultRouteContractsHaveAccessPolicyAliases 确保受保护路由具备明确权限或豁免边界。
func TestDefaultRouteContractsHaveAccessPolicyAliases(t *testing.T) {
	for _, contract := range DefaultRouteContracts() {
		switch contract.Access {
		case RouteAccessAuth, RouteAccessInternal:
			if strings.TrimSpace(contract.Alias) == "" {
				t.Fatalf("受保护路由缺少权限/审计别名: %+v", contract)
			}
		}
		if contract.Alias == string(middleware.Ignore) {
			continue
		}
		if strings.ContainsAny(contract.Alias, " \t\r\n") {
			t.Fatalf("路由别名不能包含空白字符: %+v", contract)
		}
	}
}

// TestDefaultRouteContractsSkipAccessLog 确保高频低价值路由才标记跳过普通访问日志。
func TestDefaultRouteContractsSkipAccessLog(t *testing.T) {
	skipRoutes := map[string]struct{}{
		http.MethodGet + " /api/live":                         {},
		http.MethodGet + " /api/ready":                        {},
		http.MethodGet + " /api/metrics":                      {},
		http.MethodGet + " /api/admin-messages/notifications": {},
	}
	seen := make(map[string]struct{}, len(skipRoutes))
	for _, contract := range DefaultRouteContracts() {
		_, wantSkip := skipRoutes[contract.Key()]
		if contract.SkipAccessLog != wantSkip {
			t.Fatalf("路由 %s skip_access_log = %v, want %v", contract.Key(), contract.SkipAccessLog, wantSkip)
		}
		if wantSkip {
			seen[contract.Key()] = struct{}{}
		}
	}
	if len(seen) != len(skipRoutes) {
		t.Fatalf("跳过访问日志路由覆盖不完整: got=%v want=%v", seen, skipRoutes)
	}
}

// TestActionLogRegistryAliasesAreContracted 确保审计动作绑定到已公开的路由契约。
func TestActionLogRegistryAliasesAreContracted(t *testing.T) {
	aliases := make(map[string]struct{}, len(DefaultRouteContracts()))
	for _, contract := range DefaultRouteContracts() {
		if contract.Alias == "" || contract.Alias == string(middleware.Ignore) {
			continue
		}
		aliases[contract.Alias] = struct{}{}
	}
	for name, meta := range shared.ActionLogRegistry() {
		if meta.Action == "" {
			continue
		}
		alias := string(meta.Alias)
		if _, ok := contractlessActionLogAliases[alias]; ok {
			continue
		}
		if _, ok := aliases[alias]; !ok {
			t.Fatalf("审计方法 %s 的路由别名未进入契约清单: %s", name, alias)
		}
	}
}

var contractlessActionLogAliases = map[string]struct{}{
	"admin.role.add":    {},
	"admin.role.delete": {},
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
