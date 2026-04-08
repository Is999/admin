package taskruntime

import archivetask "admin_cron/internal/jobs/archive/task"

// NewArchivePlugin 创建通用归档任务插件。
// 具体 handler、workflow 和周期维护任务统一收口在 internal/jobs/archive/task，taskruntime 只保留插件装配入口。
func NewArchivePlugin() Plugin {
	return NewPluginFunc(archivetask.PluginName, func(runtime *Runtime) error {
		return archivetask.Setup(runtime)
	})
}
