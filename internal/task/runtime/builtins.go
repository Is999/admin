package taskruntime

import (
	archivetask "admin/internal/jobs/archive/task"
	exportjob "admin/internal/jobs/export"
	usertagtask "admin/internal/jobs/usertag/task"
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
)

// BuiltinPluginSpec 描述内置任务插件的注册规格和清单信息。
type BuiltinPluginSpec struct {
	Name        string        // 插件名称，必须在内置插件中唯一
	File        string        // 插件实现所在文件
	Method      string        // 插件构造或注册入口
	Description string        // 插件中文说明
	Build       func() Plugin // 构造任务插件，返回 nil 会被启动校验拦截
}

// ComposePlugins 合并多组插件，保持注册顺序，便于在内置插件后追加业务插件。
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

// builtinPluginSpecs 是内置任务插件的单一规格源，顺序即启动注册顺序。
var builtinPluginSpecs = []BuiltinPluginSpec{
	{
		Name:        PluginNameCore,
		File:        "internal/task/runtime/core_plugin.go",
		Method:      "taskruntime.NewCorePlugin",
		Description: "注册任务核心 handler、聚合器和缓存刷新工作流",
		Build:       NewCorePlugin,
	},
	{
		Name:        archivetask.PluginName,
		File:        "internal/jobs/archive/task/plugin.go",
		Method:      "taskruntime.NewPluginFunc / archivetask.Setup",
		Description: "注册通用归档任务 handler 和工作流",
		Build: func() Plugin {
			return NewPluginFunc(archivetask.PluginName, func(runtime *Runtime) error {
				return archivetask.Setup(runtime)
			})
		},
	},
	{
		Name:        exportjob.PluginName,
		File:        "internal/jobs/export/plugin.go",
		Method:      "taskruntime.NewPluginFunc / exportjob.Setup",
		Description: "注册管理员列表异步导出 handler",
		Build: func() Plugin {
			return NewPluginFunc(exportjob.PluginName, func(runtime *Runtime) error {
				return exportjob.Setup(runtime)
			})
		},
	},
	{
		Name:        usertagtask.PluginName,
		File:        "internal/jobs/usertag/task/plugin.go",
		Method:      "taskruntime.NewPluginFunc / usertagtask.Setup",
		Description: "注册用户标签任务 handler、周期维护任务和工作流",
		Build: func() Plugin {
			return NewPluginFunc(usertagtask.PluginName, func(runtime *Runtime) error {
				return usertagtask.Setup(runtime)
			})
		},
	},
}

// BuiltinPluginSpecs 返回内置任务插件规格快照。
func BuiltinPluginSpecs() []BuiltinPluginSpec {
	return append([]BuiltinPluginSpec(nil), builtinPluginSpecs...)
}

// BuiltinPlugins 返回当前进程默认启用的任务插件集合。
// 该方法由内置插件规格派生，供启动装配、测试和显式装配入口复用。
func BuiltinPlugins() []Plugin {
	specs := BuiltinPluginSpecs()
	plugins := make([]Plugin, 0, len(specs))
	for _, spec := range specs {
		if spec.Build == nil {
			continue
		}
		plugins = append(plugins, spec.Build())
	}
	return plugins
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

// RegisterBuiltins 注册内置任务处理器、工作流定义以及业务插件。
// 应用启动路径统一通过 bootstrap 注册清单装配；测试可直接调用该入口。
func RegisterBuiltins(svcCtx *svc.ServiceContext, manager *taskqueue.Manager) error {
	_, err := Register(svcCtx, manager, BuiltinPlugins()...)
	return errors.Tag(err)
}
