package manifest

import (
	componentbuiltin "admin/internal/bootstrap/components/builtin"
	"admin/internal/bootstrap/register"
	"admin/internal/handler"
	"admin/internal/infra/collectorx"
	filelogic "admin/internal/logic/file"
	taskruntime "admin/internal/task/runtime"
	"admin/pkg/storage"
)

// Default 返回项目默认注册清单。
// 该清单由各类内置注册规格派生，不包含调用方通过 Options 注入的外部能力。
func Default() []register.Item {
	items := componentItems()
	items = append(items, routeItems()...)
	items = append(items, taskPluginItems()...)
	items = append(items, runtimeItems()...)
	return items
}

// componentItems 从启动组件规格派生清单项。
func componentItems() []register.Item {
	return itemsFromSpecs(register.KindComponent, componentbuiltin.DefaultSpecs(), func(spec componentbuiltin.Spec) register.Spec {
		return spec.Spec
	})
}

// routeItems 从内置路由模块规格派生清单项。
func routeItems() []register.Item {
	return itemsFromSpecs(register.KindRoute, handler.BuiltinRouteModuleSpecs(), func(spec handler.RouteModuleSpec) register.Spec {
		return specFromFields(spec.Name, spec.File, spec.Method, spec.Description)
	})
}

// taskPluginItems 从任务插件规格派生清单项。
func taskPluginItems() []register.Item {
	return itemsFromSpecs(register.KindTaskPlugin, componentbuiltin.DefaultTaskPluginSpecs(), func(spec taskruntime.PluginSpec) register.Spec {
		return specFromFields(spec.Name, spec.File, spec.Method, spec.Description)
	})
}

// runtimeItems 从运行时扩展规格派生清单项。
func runtimeItems() []register.Item {
	storageSpecs := storage.RuntimeRegistrySpecs()
	fileSpecs := filelogic.RuntimeRegistrySpecs()
	collectorSpecs := collectorx.RuntimeRegistrySpecs()
	items := make([]register.Item, 0, len(storageSpecs)+len(fileSpecs)+len(collectorSpecs))
	items = append(items, itemsFromSpecs(register.KindRuntimeRegistry, storageSpecs, func(spec storage.RuntimeRegistrySpec) register.Spec {
		return specFromFields(spec.Name, spec.File, spec.Method, spec.Description)
	})...)
	items = append(items, itemsFromSpecs(register.KindRuntimeRegistry, fileSpecs, func(spec filelogic.RuntimeRegistrySpec) register.Spec {
		return specFromFields(spec.Name, spec.File, spec.Method, spec.Description)
	})...)
	items = append(items, itemsFromSpecs(register.KindRuntimeRegistry, collectorSpecs, func(spec collectorx.RuntimeRegistrySpec) register.Spec {
		return specFromFields(spec.Name, spec.File, spec.Method, spec.Description)
	})...)
	return items
}

// itemsFromSpecs 把不同领域的注册规格转换成统一默认清单项。
func itemsFromSpecs[T any](kind string, specs []T, toSpec func(T) register.Spec) []register.Item {
	items := make([]register.Item, 0, len(specs))
	for _, spec := range specs {
		items = append(items, register.NewItem(kind, toSpec(spec)))
	}
	return items
}

// specFromFields 把领域注册规格字段收敛为统一注册规格。
func specFromFields(name, file, method, description string) register.Spec {
	return register.Spec{
		Name:        name,
		File:        file,
		Method:      method,
		Description: description,
	}
}
