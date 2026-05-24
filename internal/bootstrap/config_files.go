package bootstrap

import (
	"os"
	"path/filepath"
	"strings"

	"admin/internal/config"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/conf"
	yaml "go.yaml.in/yaml/v3"
)

// 运行期外部配置支持的顶层配置段。
const (
	runtimeConfigSectionTaskPeriodic = "task_periodic" // 周期任务列表
	runtimeConfigSectionArchiveJobs  = "archive_jobs"  // 归档任务列表
	runtimeConfigSectionWorkflows    = "workflows"     // 工作流类配置聚合入口
)

// runtimeConfigFile 描述外部运行期大配置文件。
// 推荐在 K8s 中把该文件作为独立 ConfigMap key 挂载，主配置只保留 config_files.runtime 路径。
type runtimeConfigFile struct {
	TaskPeriodic []config.TaskPeriodicConfig `json:"task_periodic,optional"` // 周期任务列表
	ArchiveJobs  []config.ArchiveJobConfig   `json:"archive_jobs,optional"`  // 归档任务列表
	Workflows    config.WorkflowsConfig      `json:"workflows,optional"`     // 工作流类配置聚合入口
}

// runtimeConfigSectionSpec 描述一个允许运行期外置的配置段。
type runtimeConfigSectionSpec struct {
	Key   string                                                               // 外部运行期配置文件中的顶层键
	apply func(cfg *config.Config, ext runtimeConfigFile, source string) error // 将该配置段合并到主配置
}

// runtimeConfigSectionSpecs 返回运行期外部配置段规格。
func runtimeConfigSectionSpecs() []runtimeConfigSectionSpec {
	return []runtimeConfigSectionSpec{
		{
			Key: runtimeConfigSectionTaskPeriodic,
			apply: func(cfg *config.Config, ext runtimeConfigFile, source string) error {
				merged, err := mergeTaskPeriodicConfigs(cfg.Task.Periodic, ext.TaskPeriodic, source)
				if err != nil {
					return errors.Tag(err)
				}
				cfg.Task.Periodic = merged
				return nil
			},
		},
		{
			Key: runtimeConfigSectionArchiveJobs,
			apply: func(cfg *config.Config, ext runtimeConfigFile, source string) error {
				merged, err := mergeArchiveJobConfigs(cfg.Archive.Jobs, ext.ArchiveJobs, source)
				if err != nil {
					return errors.Tag(err)
				}
				cfg.Archive.Jobs = merged
				return nil
			},
		},
		{
			Key: runtimeConfigSectionWorkflows,
			apply: func(cfg *config.Config, ext runtimeConfigFile, source string) error {
				// workflows 是工作流类配置统一入口，运行期文件显式声明后整体覆盖。
				cfg.Workflows = ext.Workflows
				return nil
			},
		},
	}
}

// runtimeConfigSectionKeys 返回当前版本会主动读取的运行期外部配置顶层键。
func runtimeConfigSectionKeys() map[string]struct{} {
	specs := runtimeConfigSectionSpecs()
	keys := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		keys[spec.Key] = struct{}{}
	}
	return keys
}

// applyExternalConfigFiles 按主配置 config_files 声明合并外部大配置文件。
// 周期任务和归档任务会合并并校验唯一，workflows 显式存在时整体覆盖。
func applyExternalConfigFiles(mainFile string, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if strings.TrimSpace(cfg.ConfigFiles.Runtime) != "" {
		path := resolveConfigIncludePath(mainFile, cfg.ConfigFiles.Runtime)
		if err := applyRuntimeConfigFile(path, cfg); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// configIncludePaths 返回主配置声明的外部配置文件绝对/相对解析结果。
func configIncludePaths(mainFile string, files config.ConfigFilesConfig) []string {
	if strings.TrimSpace(files.Runtime) == "" {
		return nil
	}
	return []string{resolveConfigIncludePath(mainFile, files.Runtime)}
}

// applyRuntimeConfigFile 合并推荐的单个运行期大配置文件。
func applyRuntimeConfigFile(path string, cfg *config.Config) error {
	content, keys, err := runtimeConfigKnownContent(path)
	if err != nil {
		return errors.Tag(err)
	}
	var ext runtimeConfigFile
	if err = conf.LoadFromYamlBytes(content, &ext); err != nil {
		return errors.Wrapf(err, "加载运行期外部配置失败 file=%s", path)
	}
	for _, spec := range runtimeConfigSectionSpecs() {
		if _, ok := keys[spec.Key]; !ok {
			continue
		}
		if err = spec.apply(cfg, ext, path); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// mergeTaskPeriodicConfigs 合并周期任务配置，并按调度稳定键和 unique_key 双重校验重复。
func mergeTaskPeriodicConfigs(base []config.TaskPeriodicConfig, extra []config.TaskPeriodicConfig, source string) ([]config.TaskPeriodicConfig, error) {
	if len(extra) == 0 {
		return base, nil
	}
	result := append([]config.TaskPeriodicConfig(nil), base...)
	taskKeys := make(map[string]struct{}, len(result)+len(extra))
	uniqueKeys := make(map[string]struct{}, len(result)+len(extra))
	for _, item := range result {
		if err := rememberPeriodicIdentity(taskKeys, uniqueKeys, item, "主配置"); err != nil {
			return nil, errors.Tag(err)
		}
	}
	for _, item := range extra {
		if err := rememberPeriodicIdentity(taskKeys, uniqueKeys, item, source); err != nil {
			return nil, errors.Tag(err)
		}
		result = append(result, item)
	}
	return result, nil
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

// mergeArchiveJobConfigs 合并归档任务配置，并按任务名校验重复。
func mergeArchiveJobConfigs(base []config.ArchiveJobConfig, extra []config.ArchiveJobConfig, source string) ([]config.ArchiveJobConfig, error) {
	if len(extra) == 0 {
		return base, nil
	}
	result := append([]config.ArchiveJobConfig(nil), base...)
	names := make(map[string]struct{}, len(result)+len(extra))
	for _, item := range result {
		if err := rememberArchiveJobName(names, item, "主配置"); err != nil {
			return nil, errors.Tag(err)
		}
	}
	for _, item := range extra {
		if err := rememberArchiveJobName(names, item, source); err != nil {
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

// resolveConfigIncludePath 解析外部配置文件路径。
// 相对路径固定按主配置文件所在目录解析，避免不同启动工作目录读到不同 runtime 文件。
func resolveConfigIncludePath(mainFile string, include string) string {
	include = strings.TrimSpace(include)
	if include == "" || filepath.IsAbs(include) {
		return filepath.Clean(include)
	}
	baseDir := filepath.Dir(filepath.Clean(mainFile))
	return filepath.Clean(filepath.Join(baseDir, include))
}

// runtimeConfigKnownContent 从运行期外部配置中提取当前版本认识的顶层配置块。
// 未识别的顶层配置保留给其它版本或其它项目使用，不参与当前进程解析，也不会导致启动失败。
func runtimeConfigKnownContent(path string) ([]byte, map[string]struct{}, error) {
	root, err := yamlFileRootMapping(path)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	keys := make(map[string]struct{})
	if root == nil {
		return []byte("{}\n"), keys, nil
	}
	knownKeys := runtimeConfigSectionKeys()
	filtered := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: make([]*yaml.Node, 0, len(root.Content)),
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := strings.TrimSpace(root.Content[i].Value)
		if _, ok := knownKeys[key]; !ok {
			continue
		}
		keys[key] = struct{}{}
		filtered.Content = append(filtered.Content, root.Content[i], root.Content[i+1])
	}
	if len(filtered.Content) == 0 {
		return []byte("{}\n"), keys, nil
	}
	content, err := yaml.Marshal(filtered)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "提取运行期外部配置失败 file=%s", path)
	}
	return content, keys, nil
}

// yamlFileRootMapping 读取 YAML 文件根映射节点。
func yamlFileRootMapping(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "读取 YAML 配置失败 file=%s", path)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return nil, errors.Wrapf(err, "解析 YAML 配置失败 file=%s", path)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return node.Content[0], nil
}
