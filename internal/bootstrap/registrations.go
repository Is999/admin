package bootstrap

import (
	"admin/internal/handler"
	"admin/internal/infra/collectorx"
	filelogic "admin/internal/logic/file"
	"admin/internal/task/runtime"
	"admin/pkg/storage"

	"github.com/Is999/go-utils/errors"
)

const (
	// registrationKindComponent 表示应用启动组件注册项。
	registrationKindComponent = "component"
	// registrationKindRoute 表示 HTTP 路由模块注册项。
	registrationKindRoute = "route"
	// registrationKindTaskPlugin 表示任务运行时插件注册项。
	registrationKindTaskPlugin = "task_plugin"
	// registrationKindRuntimeRegistry 表示包级运行时扩展注册表。
	registrationKindRuntimeRegistry = "runtime_registry"

	// componentNameCollector 表示通用收集器启动组件名称。
	componentNameCollector = "collector"
	// componentNameTaskRuntime 表示任务运行时启动组件名称。
	componentNameTaskRuntime = "task_runtime"
	// componentNameHTTPServer 表示 HTTP 服务启动组件名称。
	componentNameHTTPServer = "http_server"
)

// RegistrationManifestItem 描述一个默认注册项，供文档、测试和启动装配核对。
type RegistrationManifestItem struct {
	Kind        string // 注册类型，如 component / route / task_plugin / runtime_registry
	Name        string // 注册名称，必须在同类型内保持唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法或构造方法
	Description string // 注册项中文说明
}

// registrationSpec 描述默认注册项的稳定说明字段。
type registrationSpec struct {
	Name        string // 注册名称，必须在同类型内保持唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法或构造方法
	Description string // 注册项中文说明
}

// componentSpec 描述一个内置启动组件及其清单说明。
type componentSpec struct {
	registrationSpec                  // 组件在默认注册清单中的说明字段
	Build            func() Component // 启动组件构造函数
}

// runtimeRegistrySpec 描述一个包级运行时扩展入口。
type runtimeRegistrySpec struct {
	registrationSpec // 运行时扩展在默认注册清单中的说明字段
}

// DefaultRegistrationManifest 返回项目默认注册清单。
// 该清单由各类内置注册规格派生，不包含调用方通过 Options 注入的外部能力。
func DefaultRegistrationManifest() []RegistrationManifestItem {
	items := componentRegistrationManifestItems()
	items = append(items, routeRegistrationManifestItems()...)
	items = append(items, taskPluginRegistrationManifestItems()...)
	items = append(items, runtimeRegistrationManifestItems()...)
	return items
}

// registrationManifestItem 将注册规格转换为统一清单项。
func registrationManifestItem(kind string, spec registrationSpec) RegistrationManifestItem {
	return RegistrationManifestItem{
		Kind:        kind,
		Name:        spec.Name,
		File:        spec.File,
		Method:      spec.Method,
		Description: spec.Description,
	}
}

// componentRegistrationManifestItems 从启动组件规格派生清单项。
func componentRegistrationManifestItems() []RegistrationManifestItem {
	specs := defaultComponentSpecs()
	items := make([]RegistrationManifestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, registrationManifestItem(registrationKindComponent, spec.registrationSpec))
	}
	return items
}

// routeRegistrationManifestItems 从内置路由模块规格派生清单项。
func routeRegistrationManifestItems() []RegistrationManifestItem {
	specs := handler.BuiltinRouteModuleSpecs()
	items := make([]RegistrationManifestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, RegistrationManifestItem{
			Kind:        registrationKindRoute,
			Name:        spec.Name,
			File:        spec.File,
			Method:      spec.Method,
			Description: spec.Description,
		})
	}
	return items
}

// taskPluginRegistrationManifestItems 从任务插件规格派生清单项。
func taskPluginRegistrationManifestItems() []RegistrationManifestItem {
	specs := defaultTaskPluginSpecs()
	items := make([]RegistrationManifestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, registrationManifestItem(registrationKindTaskPlugin, registrationSpec{
			Name:        spec.Name,
			File:        spec.File,
			Method:      spec.Method,
			Description: spec.Description,
		}))
	}
	return items
}

// runtimeRegistrationManifestItems 从运行时扩展规格派生清单项。
func runtimeRegistrationManifestItems() []RegistrationManifestItem {
	specs := defaultRuntimeRegistrySpecs()
	items := make([]RegistrationManifestItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, registrationManifestItem(registrationKindRuntimeRegistry, spec.registrationSpec))
	}
	return items
}

// defaultComponentSpecs 返回项目内置启动组件规格，顺序即启动装配顺序。
func defaultComponentSpecs() []componentSpec {
	return []componentSpec{
		{
			registrationSpec: registrationSpec{
				Name:        componentNameCollector,
				File:        "internal/bootstrap/component_builtin.go",
				Method:      "newCollectorComponent",
				Description: "注册通用收集器启动组件",
			},
			Build: newCollectorComponent,
		},
		{
			registrationSpec: registrationSpec{
				Name:        componentNameTaskRuntime,
				File:        "internal/bootstrap/component_builtin.go",
				Method:      "newTaskRuntimeComponent",
				Description: "注册任务队列管理器和任务插件运行时",
			},
			Build: newTaskRuntimeComponent,
		},
		{
			registrationSpec: registrationSpec{
				Name:        componentNameHTTPServer,
				File:        "internal/bootstrap/component_builtin.go",
				Method:      "newHTTPServerComponent",
				Description: "注册 HTTP 服务和路由模块",
			},
			Build: newHTTPServerComponent,
		},
	}
}

// defaultComponents 从启动组件规格派生项目内置启动组件集合。
func defaultComponents() []Component {
	specs := defaultComponentSpecs()
	components := make([]Component, 0, len(specs))
	for _, spec := range specs {
		if spec.Build == nil {
			continue
		}
		components = append(components, spec.Build())
	}
	return components
}

// defaultRouteModules 返回项目内置 HTTP 路由模块集合。
func defaultRouteModules() []handler.RouteModule {
	return handler.BuiltinRouteModules()
}

// resolveRouteModules 合并内置路由模块与外部注入模块。
func resolveRouteModules(options Options) []handler.RouteModule {
	return handler.ComposeRouteModules(defaultRouteModules(), options.RouteModules)
}

// defaultTaskPluginSpecs 返回项目内置任务运行时插件规格，顺序即注册顺序。
func defaultTaskPluginSpecs() []taskruntime.BuiltinPluginSpec {
	return taskruntime.BuiltinPluginSpecs()
}

// defaultTaskPlugins 从任务插件规格派生项目内置任务运行时插件集合。
func defaultTaskPlugins() []taskruntime.Plugin {
	return taskruntime.BuiltinPlugins()
}

// resolveTaskPlugins 合并内置任务插件与外部注入插件。
func resolveTaskPlugins(options Options) []taskruntime.Plugin {
	return taskruntime.ComposePlugins(defaultTaskPlugins(), options.TaskPlugins)
}

// defaultRuntimeRegistrySpecs 返回清单需要覆盖的包级运行时扩展入口规格。
func defaultRuntimeRegistrySpecs() []runtimeRegistrySpec {
	storageSpecs := storageRuntimeRegistrySpecs()
	fileSpecs := fileRuntimeRegistrySpecs()
	collectorSpecs := collectorRuntimeRegistrySpecs()
	specs := make([]runtimeRegistrySpec, 0, len(storageSpecs)+len(fileSpecs)+len(collectorSpecs))
	specs = append(specs, storageSpecs...)
	specs = append(specs, fileSpecs...)
	specs = append(specs, collectorSpecs...)
	return specs
}

// runtimeRegistrySpecFromFields 构造 bootstrap 内部统一的运行时扩展规格。
func runtimeRegistrySpecFromFields(name, file, method, description string) runtimeRegistrySpec {
	return runtimeRegistrySpec{
		registrationSpec: registrationSpec{
			Name:        name,
			File:        file,
			Method:      method,
			Description: description,
		},
	}
}

// storageRuntimeRegistrySpecs 从 storage 包注册规格派生运行时清单。
func storageRuntimeRegistrySpecs() []runtimeRegistrySpec {
	items := storage.RuntimeRegistrySpecs()
	specs := make([]runtimeRegistrySpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, runtimeRegistrySpecFromFields(item.Name, item.File, item.Method, item.Description))
	}
	return specs
}

// fileRuntimeRegistrySpecs 从文件业务注册规格派生运行时清单。
func fileRuntimeRegistrySpecs() []runtimeRegistrySpec {
	items := filelogic.RuntimeRegistrySpecs()
	specs := make([]runtimeRegistrySpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, runtimeRegistrySpecFromFields(item.Name, item.File, item.Method, item.Description))
	}
	return specs
}

// collectorRuntimeRegistrySpecs 从 Collector 注册规格派生运行时清单。
func collectorRuntimeRegistrySpecs() []runtimeRegistrySpec {
	items := collectorx.RuntimeRegistrySpecs()
	specs := make([]runtimeRegistrySpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, runtimeRegistrySpecFromFields(item.Name, item.File, item.Method, item.Description))
	}
	return specs
}

// validateRegistrationNamesUnique 校验注册列表内部名称唯一，避免同一能力被重复装配。
func validateRegistrationNamesUnique(kind string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			return errors.Errorf("注册集合[%s]存在空名称", kind)
		}
		if _, ok := seen[name]; ok {
			return errors.Errorf("注册集合[%s]存在重复名称: %s", kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// routeModuleNames 提取路由模块名称列表，供启动装配和测试复用。
func routeModuleNames(items []handler.RouteModule) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}

// pluginNames 提取任务插件名称列表，供启动装配和测试复用。
func pluginNames(items []taskruntime.Plugin) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}
