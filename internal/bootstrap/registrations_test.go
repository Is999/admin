package bootstrap

import (
	"testing"

	"admin/internal/handler"
)

// TestDefaultRegistrationManifestValid 确保默认注册清单可供文档和排障读取。
func TestDefaultRegistrationManifestValid(t *testing.T) {
	items := DefaultRegistrationManifest()
	if len(items) == 0 {
		t.Fatal("默认注册清单不能为空")
	}
	seen := make(map[string]struct{}, len(items))
	knownKinds := map[string]struct{}{
		registrationKindComponent:       {},
		registrationKindRoute:           {},
		registrationKindTaskPlugin:      {},
		registrationKindRuntimeRegistry: {},
	}
	for _, item := range items {
		if _, ok := knownKinds[item.Kind]; !ok {
			t.Fatalf("默认注册清单存在未知 kind: %+v", item)
		}
		if item.Name == "" || item.File == "" || item.Method == "" || item.Description == "" {
			t.Fatalf("默认注册清单字段不完整: %+v", item)
		}
		key := item.Kind + ":" + item.Name
		if _, ok := seen[key]; ok {
			t.Fatalf("默认注册清单存在重复项: %s", key)
		}
		seen[key] = struct{}{}
	}
}

// TestValidateRegistrationNamesUniqueRejectsDuplicate 确保真实注册集合出现重复名称时会被启动校验拦截。
func TestValidateRegistrationNamesUniqueRejectsDuplicate(t *testing.T) {
	if err := validateRegistrationNamesUnique(registrationKindRoute, []string{"task", "task"}); err == nil {
		t.Fatal("期望重复注册名称返回错误，实际为 nil")
	}
}

// TestDefaultRouteModulesCoverRouteContracts 确保启动默认路由模块覆盖所有内置路由契约。
func TestDefaultRouteModulesCoverRouteContracts(t *testing.T) {
	routeNames := make(map[string]struct{})
	for _, name := range routeModuleNames(defaultRouteModules()) {
		routeNames[name] = struct{}{}
	}
	for _, contract := range handler.DefaultRouteContracts() {
		if _, ok := routeNames[contract.Module]; !ok {
			t.Fatalf("启动默认路由模块缺少契约模块: module=%s route=%s", contract.Module, contract.Key())
		}
	}
}
