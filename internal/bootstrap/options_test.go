package bootstrap

import (
	"testing"

	"admin/internal/bootstrap/components"
	"admin/internal/handler"
	"admin/internal/task/runtime"
)

// TestResolveOptionsDefaults 确保未传启动选项时仍使用默认展示名。
func TestResolveOptionsDefaults(t *testing.T) {
	opts := resolveOptions(nil)
	if opts.DisplayName != defaultDisplayName {
		t.Fatalf("expected default display name %q, got %q", defaultDisplayName, opts.DisplayName)
	}
	if len(opts.TaskPlugins) != 0 {
		t.Fatalf("expected no task plugins, got %d", len(opts.TaskPlugins))
	}
	if !opts.UseDefaultComponents {
		t.Fatal("expected default components enabled")
	}
}

// TestResolveOptionsMergesPlugins 确保外部任务插件、启动组件、路由模块和展示名能够正确注入。
func TestResolveOptionsMergesPlugins(t *testing.T) {
	pluginA := taskruntime.NewPluginFunc("plugin-a", nil)
	pluginB := taskruntime.NewPluginFunc("plugin-b", nil)
	component := components.NewFunc("component-a", nil)
	routeModule := handler.NewRouteModuleFunc("route-a", nil)

	opts := resolveOptions([]Option{
		WithDisplayName("admin-enterprise"),
		WithTaskPlugins(pluginA, pluginB),
		WithComponents(component),
		WithRouteModules(routeModule),
		WithDefaultComponents(false),
	})

	if opts.DisplayName != "admin-enterprise" {
		t.Fatalf("unexpected display name: %s", opts.DisplayName)
	}
	if len(opts.TaskPlugins) != 2 {
		t.Fatalf("expected 2 task plugins, got %d", len(opts.TaskPlugins))
	}
	if opts.TaskPlugins[0].Name() != "plugin-a" || opts.TaskPlugins[1].Name() != "plugin-b" {
		t.Fatalf("unexpected plugin order: %s, %s", opts.TaskPlugins[0].Name(), opts.TaskPlugins[1].Name())
	}
	if opts.UseDefaultComponents {
		t.Fatal("expected default components disabled")
	}
	if len(opts.Components) != 1 || opts.Components[0].Name() != "component-a" {
		t.Fatalf("unexpected component options: %+v", opts.Components)
	}
	if len(opts.RouteModules) != 1 || opts.RouteModules[0].Name() != "route-a" {
		t.Fatalf("unexpected route module options: %+v", opts.RouteModules)
	}
}
