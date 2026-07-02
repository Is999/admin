package jobs

import (
	archivetask "admin/internal/jobs/archive/task"
	exportjob "admin/internal/jobs/export"
	taskreporttask "admin/internal/jobs/taskreport/task"
	usertagtask "admin/internal/jobs/usertag/task"
	taskqueue "admin/internal/task/queue"
	taskruntime "admin/internal/task/runtime"
)

// PluginSpecs 返回业务和维护任务插件规格，顺序即启动注册顺序。
func PluginSpecs() []taskruntime.PluginSpec {
	return []taskruntime.PluginSpec{
		{
			Name:        "job_task_metadata",
			File:        "internal/jobs/plugins.go",
			Method:      "jobs.TaskMetadataPlugin",
			Description: "注册业务任务类型和工作流的展示元数据",
			Build:       TaskMetadataPlugin,
		},
		{
			Name:        archivetask.PluginName,
			File:        "internal/jobs/archive/task/plugin.go",
			Method:      "jobs.ArchivePlugin / archivetask.Setup",
			Description: "注册通用归档任务 handler 和工作流",
			Build:       ArchivePlugin,
		},
		{
			Name:        exportjob.PluginName,
			File:        "internal/jobs/export/plugin.go",
			Method:      "jobs.AdminExportPlugin / exportjob.Setup",
			Description: "注册管理员列表异步导出 handler",
			Build:       AdminExportPlugin,
		},
		{
			Name:        taskreporttask.PluginName,
			File:        "internal/jobs/taskreport/task/plugin.go",
			Method:      "jobs.TaskReportPlugin / taskreporttask.Setup",
			Description: "注册周期任务和工作流运行日报任务",
			Build:       TaskReportPlugin,
		},
		{
			Name:        usertagtask.PluginName,
			File:        "internal/jobs/usertag/task/plugin.go",
			Method:      "jobs.UserTagPlugin / usertagtask.Setup",
			Description: "注册用户标签任务 handler、周期维护任务和工作流",
			Build:       UserTagPlugin,
		},
	}
}

// TaskMetadataPlugin 创建业务任务展示元数据插件。
func TaskMetadataPlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc("job_task_metadata", func(runtime *taskruntime.Runtime) error {
		_ = runtime
		registerTaskMetadata()
		return nil
	})
}

// registerTaskMetadata 注册任务列表和工作流触发页使用的展示元数据。
func registerTaskMetadata() {
	taskqueue.RegisterTaskTypeDisplaySpecs(taskTypeDisplaySpecs()...)
	taskqueue.RegisterWorkflowDisplaySpecs(workflowDisplaySpecs()...)
}

// ArchivePlugin 创建通用归档任务插件。
func ArchivePlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc(archivetask.PluginName, func(runtime *taskruntime.Runtime) error {
		return archivetask.Setup(runtime)
	})
}

// AdminExportPlugin 创建管理员列表异步导出任务插件。
func AdminExportPlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc(exportjob.PluginName, func(runtime *taskruntime.Runtime) error {
		return exportjob.Setup(runtime)
	})
}

// TaskReportPlugin 创建任务运行日报插件。
func TaskReportPlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc(taskreporttask.PluginName, func(runtime *taskruntime.Runtime) error {
		return taskreporttask.Setup(runtime)
	})
}

// UserTagPlugin 创建用户标签任务插件。
func UserTagPlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc(usertagtask.PluginName, func(runtime *taskruntime.Runtime) error {
		return usertagtask.Setup(runtime)
	})
}
