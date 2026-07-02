package runtimefile

import (
	"path/filepath"
	"strings"

	"admin/internal/config"
)

// IncludePaths 返回主配置声明的外部配置文件绝对/相对解析结果。
func IncludePaths(mainFile string, files config.ConfigFilesConfig) []string {
	if strings.TrimSpace(files.Runtime) == "" {
		return nil
	}
	return []string{resolveIncludePath(mainFile, files.Runtime)}
}

// resolveIncludePath 解析外部配置文件路径。
// 相对路径固定按主配置文件所在目录解析，避免不同启动工作目录读到不同 runtime 文件。
func resolveIncludePath(mainFile string, include string) string {
	include = strings.TrimSpace(include)
	if include == "" || filepath.IsAbs(include) {
		return filepath.Clean(include)
	}
	baseDir := filepath.Dir(filepath.Clean(mainFile))
	return filepath.Clean(filepath.Join(baseDir, include))
}
