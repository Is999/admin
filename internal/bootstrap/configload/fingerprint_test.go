package configload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConfigFileFingerprintChangesAfterRewrite 确保配置文件内容或映射目标变化时会产生新的指纹。
func TestConfigFileFingerprintChangesAfterRewrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte("name: first\n"), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	first, err := configFileFingerprint(file)
	if err != nil {
		t.Fatalf("获取首次指纹失败: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err = os.WriteFile(file, []byte("name: third\n"), 0o644); err != nil {
		t.Fatalf("重写配置文件失败: %v", err)
	}
	second, err := configFileFingerprint(file)
	if err != nil {
		t.Fatalf("获取第二次指纹失败: %v", err)
	}
	if first == second {
		t.Fatalf("期望重写后指纹发生变化，实际仍为 %q", first)
	}
}

// TestConfigBundleFingerprintChangesAfterExternalRewrite 确保外部配置文件变化也会触发热加载指纹变化。
func TestConfigBundleFingerprintChangesAfterExternalRewrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte("task_periodic: []\n"), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	first, err := BundleFingerprint(mainFile)
	if err != nil {
		t.Fatalf("获取首次配置包指纹失败: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err = os.WriteFile(runtimeFile, []byte("archive_jobs: []\n"), 0o644); err != nil {
		t.Fatalf("重写外部配置失败: %v", err)
	}
	second, err := BundleFingerprint(mainFile)
	if err != nil {
		t.Fatalf("获取第二次配置包指纹失败: %v", err)
	}
	if first == second {
		t.Fatalf("期望外部配置重写后指纹发生变化，实际仍为 %q", first)
	}
}
