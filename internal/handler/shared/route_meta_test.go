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

// TestActionLogParamFromMeta 确保审计参数直接由 RouteMeta 派生，不再维护额外方法映射。
func TestActionLogParamFromMeta(t *testing.T) {
	for _, meta := range DefaultRouteMetas() {
		param := ActionLogParamFromMeta(meta)
		if meta.Action == "" && param != nil {
			t.Fatalf("ActionLogParamFromMeta should skip non-audit route meta=%+v", meta)
		}
		if meta.Action != "" && param == nil {
			t.Fatalf("ActionLogParamFromMeta returned nil: %+v", meta)
		}
		if param != nil && (param.Route != string(meta.Alias) || param.Method != string(meta.Alias) || param.Describe != meta.Describe) {
			t.Fatalf("ActionLogParamFromMeta returned mismatched param=%+v meta=%+v", param, meta)
		}
	}
}
