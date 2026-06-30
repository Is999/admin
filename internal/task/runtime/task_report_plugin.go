package taskruntime

import taskreporttask "admin/internal/jobs/taskreport/task"

// NewTaskReportPlugin 创建任务运行日报插件。
// 具体 handler 和 workflow 统一收口在 internal/jobs/taskreport/task。
func NewTaskReportPlugin() Plugin {
	return NewPluginFunc(taskreporttask.PluginName, func(runtime *Runtime) error {
		return taskreporttask.Setup(runtime)
	})
}
