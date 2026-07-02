package taskruntime

import (
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
)

// PluginSpec 描述任务插件的注册规格和清单信息。
type PluginSpec struct {
	Name        string        // 插件名称，必须在同一注册集合内唯一
	File        string        // 插件实现所在文件
	Method      string        // 插件构造或注册入口
	Description string        // 插件中文说明
	Build       func() Plugin // 构造任务插件，返回 nil 会被启动校验拦截
}

// ComposePlugins 合并多组插件，保持注册顺序。
func ComposePlugins(groups ...[]Plugin) []Plugin {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	plugins := make([]Plugin, 0, total)
	for _, group := range groups {
		plugins = append(plugins, group...)
	}
	return plugins
}

// ComposePluginSpecs 合并多组插件规格，保持注册顺序。
func ComposePluginSpecs(groups ...[]PluginSpec) []PluginSpec {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	specs := make([]PluginSpec, 0, total)
	for _, group := range groups {
		specs = append(specs, group...)
	}
	return specs
}

// PluginsFromSpecs 从插件规格派生插件实例列表。
func PluginsFromSpecs(specs []PluginSpec) []Plugin {
	plugins := make([]Plugin, 0, len(specs))
	for _, spec := range specs {
		if spec.Build == nil {
			continue
		}
		plugins = append(plugins, spec.Build())
	}
	return plugins
}

// CorePluginSpecs 返回任务运行时核心插件规格。
func CorePluginSpecs() []PluginSpec {
	return []PluginSpec{
		{
			Name:        PluginNameCore,
			File:        "internal/task/runtime/core_plugin.go",
			Method:      "taskruntime.NewCorePlugin",
			Description: "注册任务核心 handler、聚合器和缓存刷新工作流",
			Build:       NewCorePlugin,
		},
	}
}

// Register 使用统一入口注册插件，并返回运行时对象，便于调用方后续补充扩展信息。
func Register(svcCtx *svc.ServiceContext, manager *taskqueue.Manager, plugins ...Plugin) (*Runtime, error) {
	if manager == nil || svcCtx == nil {
		return nil, nil
	}
	runtime := NewRuntime(svcCtx, manager)
	if err := runtime.RegisterPlugins(plugins...); err != nil {
		return nil, errors.Tag(err)
	}
	return runtime, nil
}
