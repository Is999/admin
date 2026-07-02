package runtimefile

import (
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

// 运行期外部配置支持的顶层配置段。
const (
	sectionTaskPeriodic = "task_periodic" // 周期任务列表
	sectionArchiveJobs  = "archive_jobs"  // 归档任务列表
	sectionWorkflows    = "workflows"     // 工作流类配置聚合入口
)

// file 描述外部运行期大配置文件。
// 推荐在 K8s 中把该文件作为独立 ConfigMap key 挂载，主配置只保留 config_files.runtime 路径。
type file struct {
	TaskPeriodic []config.TaskPeriodicConfig `json:"task_periodic,optional"` // 周期任务列表
	ArchiveJobs  []config.ArchiveJobConfig   `json:"archive_jobs,optional"`  // 归档任务列表
	Workflows    config.WorkflowsConfig      `json:"workflows,optional"`     // 工作流类配置聚合入口
}

// sectionSpec 描述一个允许运行期外置的配置段。
type sectionSpec struct {
	Key   string                                                  // 外部运行期配置文件中的顶层键
	apply func(cfg *config.Config, ext file, source string) error // 将该配置段合并到主配置
}

// sectionSpecs 返回运行期外部配置段规格。
func sectionSpecs() []sectionSpec {
	return []sectionSpec{
		{
			Key:   sectionTaskPeriodic,
			apply: applyTaskPeriodic,
		},
		{
			Key:   sectionArchiveJobs,
			apply: applyArchiveJobs,
		},
		{
			Key:   sectionWorkflows,
			apply: applyWorkflows,
		},
	}
}

// sectionKeys 返回当前版本会主动读取的运行期外部配置顶层键。
func sectionKeys() map[string]struct{} {
	specs := sectionSpecs()
	keys := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		keys[spec.Key] = struct{}{}
	}
	return keys
}

// applyTaskPeriodic 合并外部周期任务配置。
func applyTaskPeriodic(cfg *config.Config, ext file, source string) error {
	merged, err := mergeTaskPeriodic(cfg.Task.Periodic, ext.TaskPeriodic, source)
	if err != nil {
		return errors.Tag(err)
	}
	cfg.Task.Periodic = merged
	return nil
}

// applyArchiveJobs 合并外部归档任务配置。
func applyArchiveJobs(cfg *config.Config, ext file, source string) error {
	merged, err := mergeArchiveJobs(cfg.Archive.Jobs, ext.ArchiveJobs, source)
	if err != nil {
		return errors.Tag(err)
	}
	cfg.Archive.Jobs = merged
	return nil
}

// applyWorkflows 覆盖外部 workflows 配置块。
func applyWorkflows(cfg *config.Config, ext file, _ string) error {
	// workflows 是工作流类配置统一入口，运行期文件显式声明后整体覆盖。
	cfg.Workflows = ext.Workflows
	return nil
}
