package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	componentbuiltin "admin/internal/bootstrap/components/builtin"
	"admin/internal/bootstrap/register"
	"admin/internal/handler"
)

// TestDefaultItemsCoveredByComponentDoc 确保默认注册项不会脱离组件清单文档。
func TestDefaultItemsCoveredByComponentDoc(t *testing.T) {
	docPath := filepath.Join("..", "..", "..", "docs", "site", "角色文档", "后端开发", "组件注册清单.md")
	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("读取组件注册清单失败: %v", err)
	}
	doc := string(content)
	for _, item := range Default() {
		if !strings.Contains(doc, "`"+item.Name+"`") {
			t.Fatalf("组件注册清单缺少默认项: kind=%s name=%s", item.Kind, item.Name)
		}
	}
}

// TestBuiltinRouteModulesCoverRouteContracts 确保启动默认路由模块覆盖所有内置路由契约。
func TestBuiltinRouteModulesCoverRouteContracts(t *testing.T) {
	routeNames := make(map[string]struct{})
	for _, name := range register.RouteModuleNames(handler.BuiltinRouteModules()) {
		routeNames[name] = struct{}{}
	}
	for _, contract := range handler.DefaultRouteContracts() {
		if _, ok := routeNames[contract.Module]; !ok {
			t.Fatalf("启动默认路由模块缺少契约模块: module=%s route=%s", contract.Module, contract.Key())
		}
	}
}

// TestDefaultKeepsComponentOrder 确保默认清单以启动组件顺序开头。
func TestDefaultKeepsComponentOrder(t *testing.T) {
	items := Default()
	componentSpecs := componentbuiltin.DefaultSpecs()
	if len(items) < len(componentSpecs) {
		t.Fatalf("默认注册清单长度不足: got=%d want>=%d", len(items), len(componentSpecs))
	}
	for index, spec := range componentSpecs {
		item := items[index]
		if item.Kind != register.KindComponent ||
			item.Name != spec.Name ||
			item.File != spec.File ||
			item.Method != spec.Method ||
			item.Description != spec.Description {
			t.Fatalf("组件清单顺序或字段不符合预期: index=%d item=%+v spec=%+v", index, item, spec)
		}
	}
}
