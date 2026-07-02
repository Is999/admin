package runtimefile

import (
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/conf"
)

// Apply 按主配置 config_files 声明合并外部大配置文件。
// 周期任务和归档任务会合并并校验唯一，workflows 显式存在时整体覆盖。
func Apply(mainFile string, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if strings.TrimSpace(cfg.ConfigFiles.Runtime) == "" {
		return nil
	}
	path := resolveIncludePath(mainFile, cfg.ConfigFiles.Runtime)
	if err := apply(path, cfg); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// apply 合并推荐的单个运行期大配置文件。
func apply(path string, cfg *config.Config) error {
	content, keys, err := knownContent(path)
	if err != nil {
		return errors.Tag(err)
	}
	var ext file
	if err = conf.LoadFromYamlBytes(content, &ext); err != nil {
		return errors.Wrapf(err, "加载运行期外部配置失败 file=%s", path)
	}
	for _, spec := range sectionSpecs() {
		if _, ok := keys[spec.Key]; !ok {
			continue
		}
		if err = spec.apply(cfg, ext, path); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}
