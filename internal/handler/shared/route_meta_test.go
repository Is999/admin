package shared

import "testing"

// TestDefaultRouteMetasValid 确保后台内置路由元数据字段完整且别名唯一。
func TestDefaultRouteMetasValid(t *testing.T) {
	metas := DefaultRouteMetas()
	if len(metas) == 0 {
		t.Fatal("DefaultRouteMetas() must not be empty")
	}
	seen := make(map[string]struct{}, len(metas))
	for _, meta := range metas {
		if meta.Alias == "" {
			t.Fatalf("route meta has empty alias: %+v", meta)
		}
		if meta.Describe == "" {
			t.Fatalf("route meta has empty describe: %+v", meta)
		}
		alias := string(meta.Alias)
		if _, ok := seen[alias]; ok {
			t.Fatalf("duplicate route alias: %s", alias)
		}
		seen[alias] = struct{}{}
	}
}

// TestActionLogRegistryUsesRouteMetas 确保方法注册表绑定完整路由元数据，并按 Action 决定是否写审计。
func TestActionLogRegistryUsesRouteMetas(t *testing.T) {
	registry := ActionLogRegistry()
	if len(registry) == 0 {
		t.Fatal("ActionLogRegistry() must not be empty")
	}
	for method, meta := range registry {
		if method == "" {
			t.Fatalf("action log registry has empty method: %+v", meta)
		}
		if meta.Alias == "" || meta.Describe == "" {
			t.Fatalf("action log registry has incomplete meta method=%s meta=%+v", method, meta)
		}
		param := ActionLogMap(method)
		if meta.Action == "" && param != nil {
			t.Fatalf("ActionLogMap(%s) should skip non-audit route meta=%+v", method, meta)
		}
		if meta.Action != "" && param == nil {
			t.Fatalf("ActionLogMap(%s) returned nil", method)
		}
	}
}
