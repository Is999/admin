package configload

import (
	"os"
	"path/filepath"
	"testing"
)

// runtimeConfigFileYAML 是测试主配置引用外部运行期文件的公共片段。
const runtimeConfigFileYAML = `
config_files:
  runtime: "config.d/runtime.sample.yaml"
`

// minimalConfigYAML 构造配置解析测试需要的最小主配置。
func minimalConfigYAML(extra string) string {
	return `
name: test
host: 127.0.0.1
port: 8888
Mode: dev
app_id: "1"
snowflake:
  worker_id: 512
jwt_secret: test-secret-0123456789abcdef
mysql:
  write_data_source: "root:pass@tcp(127.0.0.1:3306)/admin"
  read_data_sources: []
  max_open_conns: 1
  max_idle_conns: 1
  conn_max_lifetime: 60
  debug: false
redis:
  type: "single"
  addrs:
    - "127.0.0.1:6379"
  password: ""
  db: 0
  pool_size: 1
` + extra
}

// writeRuntimeConfigFiles 写入带外部运行期配置引用的测试文件，并返回主配置路径。
func writeRuntimeConfigFiles(t *testing.T, mainExtra string, runtimeContent string) string {
	t.Helper()
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "config.d")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(runtimeConfigFileYAML+mainExtra)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(runtimeDir, "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(runtimeContent), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	return mainFile
}
