package configload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"admin/internal/bootstrap/configload/runtimefile"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

// configFileFingerprint 返回单个配置文件当前的稳定指纹。
// 对 K8s ConfigMap 场景，真实路径、文件元信息和内容哈希变化都视为配置已更新。
func configFileFingerprint(file string) (string, error) {
	cleanFile := filepath.Clean(strings.TrimSpace(file))
	info, err := os.Stat(cleanFile)
	if err != nil {
		return "", errors.Tag(err)
	}
	data, err := os.ReadFile(cleanFile)
	if err != nil {
		return "", errors.Tag(err)
	}
	realPath, err := filepath.EvalSymlinks(cleanFile)
	if err != nil {
		realPath = cleanFile
	}
	return fmt.Sprintf("%s|%d|%d|%s", realPath, info.Size(), info.ModTime().UnixNano(), utils.SHA256(string(data))), nil
}

// BundleFingerprint 返回主配置及其外部配置文件组成的配置包指纹。
// 任意一个外部文件发生 ConfigMap 原子替换、大小变化或 mtime 变化，都会触发热加载。
func BundleFingerprint(file string) (string, error) {
	mainFingerprint, err := configFileFingerprint(file)
	if err != nil {
		return "", errors.Tag(err)
	}
	cfg, err := loadBaseConfig(file)
	if err != nil {
		// 主配置尚未能成功解析时，仍返回主文件指纹，让热加载进入 Load 阶段并记录明确错误。
		return mainFingerprint, nil
	}
	parts := []string{mainFingerprint}
	for _, include := range runtimefile.IncludePaths(file, cfg.ConfigFiles) {
		fingerprint, innerErr := configFileFingerprint(include)
		if innerErr != nil {
			return "", errors.Wrapf(innerErr, "读取外部配置文件指纹失败 file=%s", include)
		}
		parts = append(parts, fingerprint)
	}
	return strings.Join(parts, "||"), nil
}
