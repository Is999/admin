package taskruntime

import (
	"admin_cron/internal/svc"
	"admin_cron/internal/taskqueue"

	"github.com/Is999/go-utils/errors"
)

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

// BuiltinPlugins 返回当前进程默认启用的任务插件集合。
// 该方法仅保留给旧测试和兼容入口复用；启动默认清单以 bootstrap.defaultTaskPlugins 为唯一来源。
func BuiltinPlugins() []Plugin {
	return []Plugin{
		NewCorePlugin(),        // 核心插件
		NewArchivePlugin(),     // 通用归档插件
		NewAdminExportPlugin(), // 管理员导出插件
		NewUserTagPlugin(),     // 用户标签插件
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

// RegisterBuiltins 注册内置任务处理器、工作流定义以及业务插件。
// 该方法仅作为兼容入口保留；应用启动路径统一通过 bootstrap 注册清单装配。
func RegisterBuiltins(svcCtx *svc.ServiceContext, manager *taskqueue.Manager) error {
	_, err := Register(svcCtx, manager, BuiltinPlugins()...)
	return errors.Tag(err)
}
