package bootstrap

import "testing"

// TestValidateDefaultRegistrationManifest 确保默认注册清单与真实内置注册集合保持一致。
func TestValidateDefaultRegistrationManifest(t *testing.T) {
	if err := ValidateDefaultRegistrationManifest(); err != nil {
		t.Fatalf("校验默认注册清单失败: %v", err)
	}
}

// TestDefaultRegistrationManifestHasBuiltinEntries 确保清单至少覆盖组件、路由、任务插件和运行时扩展四类内置注册。
func TestDefaultRegistrationManifestHasBuiltinEntries(t *testing.T) {
	items := DefaultRegistrationManifest()
	if len(items) == 0 {
		t.Fatal("默认注册清单不能为空")
	}

	kindSet := make(map[string]struct{}, len(items))
	for _, item := range items {
		kindSet[item.Kind] = struct{}{}
	}
	for _, kind := range []string{registrationKindComponent, registrationKindRoute, registrationKindTaskPlugin, registrationKindRuntimeRegistry} {
		if _, ok := kindSet[kind]; !ok {
			t.Fatalf("默认注册清单缺少 kind=%s", kind)
		}
	}
}

// TestValidateNameListUniqueRejectsDuplicate 确保真实注册集合出现重复名称时会被启动校验拦截。
func TestValidateNameListUniqueRejectsDuplicate(t *testing.T) {
	if err := validateNameListUnique(registrationKindRoute, []string{"task", "task"}); err == nil {
		t.Fatal("期望重复注册名称返回错误，实际为 nil")
	}
}

// TestResolveRegistrationsPreservesBuiltinOrder 确保 bootstrap 统一清单维护内置路由、任务插件和运行时扩展顺序。
func TestResolveRegistrationsPreservesBuiltinOrder(t *testing.T) {
	routes := defaultRouteModules()
	if len(routes) == 0 || routes[0].Name() != "health" || routes[len(routes)-1].Name() != "docs" {
		t.Fatalf("内置路由顺序不符合预期: %+v", routeModuleNames(routes))
	}

	plugins := defaultTaskPlugins()
	if len(plugins) == 0 || plugins[0].Name() != "core" || plugins[len(plugins)-1].Name() != "user_tag" {
		t.Fatalf("内置任务插件顺序不符合预期: %+v", pluginNames(plugins))
	}

	runtimes := defaultRuntimeRegistryNames()
	if len(runtimes) == 0 || runtimes[0] != "object_storage" || runtimes[len(runtimes)-1] != "collector_processor" {
		t.Fatalf("内置运行时扩展顺序不符合预期: %+v", runtimes)
	}
}
