package runtimefile

import (
	"strings"

	"admin/internal/config"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
)

// mergeTaskPeriodic 合并周期任务配置，并按调度稳定键和 unique_key 双重校验重复。
func mergeTaskPeriodic(base []config.TaskPeriodicConfig, extra []config.TaskPeriodicConfig, source string) ([]config.TaskPeriodicConfig, error) {
	taskKeys := make(map[string]struct{}, len(base)+len(extra))
	uniqueKeys := make(map[string]struct{}, len(base)+len(extra))
	return mergeItems(base, extra, source, func(item config.TaskPeriodicConfig, source string) error {
		return rememberPeriodicIdentity(taskKeys, uniqueKeys, item, source)
	})
}

// rememberPeriodicIdentity 记录周期任务唯一身份，避免同一调度入口在多个配置来源中重复投递。
func rememberPeriodicIdentity(taskKeys map[string]struct{}, uniqueKeys map[string]struct{}, item config.TaskPeriodicConfig, source string) error {
	taskKey, err := taskqueue.PeriodicTaskConfigKey(item)
	if err != nil {
		return errors.Wrapf(err, "周期任务配置无效 source=%s name=%s workflow=%s", source, strings.TrimSpace(item.Name), strings.TrimSpace(item.Workflow))
	}
	if _, ok := taskKeys[taskKey]; ok {
		return errors.Errorf("周期任务稳定键重复: key=%s source=%s", taskKey, source)
	}
	taskKeys[taskKey] = struct{}{}
	uniqueKey := strings.TrimSpace(item.UniqueKey)
	if uniqueKey != "" {
		if _, ok := uniqueKeys[uniqueKey]; ok {
			return errors.Errorf("周期任务唯一键重复: unique_key=%s source=%s", uniqueKey, source)
		}
		uniqueKeys[uniqueKey] = struct{}{}
	}
	return nil
}

// mergeArchiveJobs 合并归档任务配置，并按任务名校验重复。
func mergeArchiveJobs(base []config.ArchiveJobConfig, extra []config.ArchiveJobConfig, source string) ([]config.ArchiveJobConfig, error) {
	names := make(map[string]struct{}, len(base)+len(extra))
	return mergeItems(base, extra, source, func(item config.ArchiveJobConfig, source string) error {
		return rememberArchiveJobName(names, item, source)
	})
}

// mergeItems 合并主配置与外部运行期配置项，并按来源做重复校验。
func mergeItems[T any](base []T, extra []T, source string, remember func(T, string) error) ([]T, error) {
	if len(extra) == 0 {
		return base, nil
	}
	result := append([]T(nil), base...)
	for _, item := range result {
		if err := remember(item, "主配置"); err != nil {
			return nil, errors.Tag(err)
		}
	}
	for _, item := range extra {
		if err := remember(item, source); err != nil {
			return nil, errors.Tag(err)
		}
		result = append(result, item)
	}
	return result, nil
}

// rememberArchiveJobName 记录归档任务名称，避免同名任务来自多个配置文件。
func rememberArchiveJobName(names map[string]struct{}, item config.ArchiveJobConfig, source string) error {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return nil
	}
	if _, ok := names[name]; ok {
		return errors.Errorf("归档任务名称重复: name=%s source=%s", name, source)
	}
	names[name] = struct{}{}
	return nil
}
