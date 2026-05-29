package bootstrap

import (
	"testing"

	"admin/internal/handler"
)

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
