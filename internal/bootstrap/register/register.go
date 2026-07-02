package register

import (
	"admin/internal/handler"
	taskruntime "admin/internal/task/runtime"

	"github.com/Is999/go-utils/errors"
)

const (
	// KindComponent 表示应用启动组件注册项。
	KindComponent = "component"
	// KindRoute 表示 HTTP 路由模块注册项。
	KindRoute = "route"
	// KindTaskPlugin 表示任务运行时插件注册项。
	KindTaskPlugin = "task_plugin"
	// KindRuntimeRegistry 表示包级运行时扩展注册表。
	KindRuntimeRegistry = "runtime_registry"
)

// Spec 描述一个默认注册规格，供启动装配和注册清单复用。
type Spec struct {
	Name        string // 注册名称，必须在同类型内唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法或构造方法
	Description string // 注册项中文说明
}

// Item 描述一个默认注册项，供文档、测试和启动装配核对。
type Item struct {
	Kind        string // 注册类型，如 component / route / task_plugin / runtime_registry
	Name        string // 注册名称，必须在同类型内保持唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法或构造方法
	Description string // 注册项中文说明
}

// NewItem 从注册规格生成统一清单项。
func NewItem(kind string, spec Spec) Item {
	return Item{
		Kind:        kind,
		Name:        spec.Name,
		File:        spec.File,
		Method:      spec.Method,
		Description: spec.Description,
	}
}

// ValidateNamesUnique 校验注册列表内部名称唯一，避免同一能力被重复装配。
func ValidateNamesUnique(kind string, names []string) error {
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

// RouteModuleNames 提取路由模块名称列表，供启动装配和测试复用。
func RouteModuleNames(items []handler.RouteModule) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}

// PluginNames 提取任务插件名称列表，供启动装配和测试复用。
func PluginNames(items []taskruntime.Plugin) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}
