package taskruntime

import usertagtask "admin_cron/internal/jobs/usertag/task"

// NewUserTagPlugin 创建用户标签任务插件。
// 具体 handler、workflow 和周期维护任务统一收口在 internal/jobs/usertag/task，taskruntime 只保留插件装配入口。
func NewUserTagPlugin() Plugin {
	return NewPluginFunc(usertagtask.PluginName, func(runtime *Runtime) error {
		return usertagtask.Setup(runtime)
	})
}
