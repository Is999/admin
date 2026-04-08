package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"admin_cron/internal/config"
)

// TestLoadConfigLoadsSample 确保默认装配入口能够正确读取项目样例配置。
func TestLoadConfigLoadsSample(t *testing.T) {
	cfg, err := LoadConfig("../../etc/config.sample.yaml")
	if err != nil {
		t.Skipf("样例配置包含环境变量占位，当前测试环境未注入，跳过: %v", err)
	}
	if cfg.Name == "" {
		t.Fatal("期望成功读取配置名称")
	}
	if cfg.Host == "" || cfg.Port == 0 {
		t.Fatalf("期望成功读取 host/port，实际为 %q:%d", cfg.Host, cfg.Port)
	}
}

// TestWireWithConfigModeReturnsConfigError 确保装配入口遇到配置错误时返回错误而不是 panic。
func TestWireWithConfigModeReturnsConfigError(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "missing.yaml")
	if _, err := WireWithConfigMode(context.Background(), missingFile, nil); err == nil {
		t.Fatal("期望缺失配置文件返回错误")
	}
}

// TestResolveRunMode 确保启动模式遵循“命令行优先、配置兜底、默认回退 7”的规则。
func TestResolveRunMode(t *testing.T) {
	cfg := config.Config{RunMode: 5}

	if mode, err := ResolveRunMode(nil, cfg); err != nil || mode != 5 {
		t.Fatalf("期望配置中的 run_mode 为 5，实际为 %d", mode)
	}

	cliMode := 2
	if mode, err := ResolveRunMode(&cliMode, cfg); err != nil || mode != 2 {
		t.Fatalf("期望命令行模式为 2，实际为 %d", mode)
	}

	zeroCliMode := 0
	if mode, err := ResolveRunMode(&zeroCliMode, cfg); err != nil || mode != 5 {
		t.Fatalf("期望命令行 mode=0 回退配置中的 5，实际为 %d", mode)
	}

	zeroCfg := config.Config{}
	if mode, err := ResolveRunMode(nil, zeroCfg); err != nil || mode != ModeAll {
		t.Fatalf("期望回退模式为 %d，实际为 %d", ModeAll, mode)
	}
}

// TestResolveRunModeRejectsInvalidMode 确保非法启动模式不会被静默裁剪成其它模式。
func TestResolveRunModeRejectsInvalidMode(t *testing.T) {
	invalidMode := 8
	if _, err := ResolveRunMode(&invalidMode, config.Config{}); err == nil {
		t.Fatal("期望非法启动模式返回错误，实际为 nil")
	}
}

// TestLoadConfigLoadsHotReload 确保配置解析入口可正确读取热加载配置段。
func TestLoadConfigLoadsHotReload(t *testing.T) {
	cfg, err := LoadConfig("../../etc/config.sample.yaml")
	if err != nil {
		t.Skipf("样例配置包含环境变量占位，当前测试环境未注入，跳过: %v", err)
	}
	if !cfg.HotReload.Enabled {
		t.Fatal("期望样例配置中启用热加载")
	}
	if cfg.HotReload.CheckIntervalSeconds <= 0 {
		t.Fatalf("期望热加载检查间隔大于 0，实际为 %d", cfg.HotReload.CheckIntervalSeconds)
	}
}

// TestLoadConfigInheritsCollectorKafkaCommon 确保 Collector Kafka 复用顶层公共连接参数。
func TestLoadConfigInheritsCollectorKafkaCommon(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
kafka:
  brokers:
    - "127.0.0.1:9092"
  batch_size: 321
  write_timeout: 7
  topics:
    user_tag: "user_tag_event"
collector:
  enabled: true
  kafka:
    enabled: true
    topic: "collector_events"
    group_id: "collector_group"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if got := cfg.Collector.Kafka.Brokers; len(got) != 1 || got[0] != "127.0.0.1:9092" {
		t.Fatalf("期望继承 Kafka brokers，实际为 %+v", got)
	}
	if cfg.Collector.Kafka.BatchSize != 321 {
		t.Fatalf("期望继承 Kafka batch_size=321，实际为 %d", cfg.Collector.Kafka.BatchSize)
	}
	if cfg.Collector.Kafka.WriteTimeout != 7 {
		t.Fatalf("期望继承 Kafka write_timeout=7，实际为 %d", cfg.Collector.Kafka.WriteTimeout)
	}
	if cfg.Kafka.Topics.UserTag != "user_tag_event" {
		t.Fatalf("期望加载 Kafka 用户标签 topic，实际为 %q", cfg.Kafka.Topics.UserTag)
	}
}

// TestLoadConfigMergesRuntimeConfigFile 确保主配置可以引用单个外部运行期配置文件。
func TestLoadConfigMergesRuntimeConfigFile(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
task_periodic:
  - name: "external-periodic"
    cron: "0 2 * * *"
    workflow: "external.workflow"
archive_jobs:
  - name: "external_archive"
    enabled: true
    database: "main"
    table_name: "admin_log"
workflows:
  user_tag:
    enabled: true
    default_shard_total: 16
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if got := len(cfg.Task.Periodic); got != 1 {
		t.Fatalf("期望加载 1 个周期任务，实际为 %d", got)
	}
	if got := len(cfg.Archive.Jobs); got != 1 {
		t.Fatalf("期望加载 1 个归档任务，实际为 %d", got)
	}
	if !cfg.Workflows.UserTag.Enabled || cfg.Workflows.UserTag.DefaultShardTotal != 16 {
		t.Fatalf("期望外部 workflows.user_tag 覆盖主配置，实际为 %+v", cfg.Workflows.UserTag)
	}
}

// TestLoadConfigIgnoresUnknownRuntimeConfigKeys 验证运行期外部配置会忽略当前版本不认识的顶层配置。
func TestLoadConfigIgnoresUnknownRuntimeConfigKeys(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
user_tag:
  enabled: true
  default_shard_total: 12
future_feature:
  enabled: true
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("未知顶层配置不应导致启动失败: %v", err)
	}
	if cfg.Workflows.UserTag.DefaultShardTotal != 0 {
		t.Fatalf("未知顶层 user_tag 不应被旧结构隐式合并，实际为 %+v", cfg.Workflows.UserTag)
	}
}

func TestLoadConfigIgnoresUnknownRuntimeConfigBlock(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
obsolete_feature:
  enabled: true
  database: "main"
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	if _, err := LoadConfig(mainFile); err != nil {
		t.Fatalf("未知运行期配置块应被忽略: %v", err)
	}
}

// TestLoadConfigRejectsDuplicateExternalRuntimeConfig 确保外部配置不会静默覆盖主配置任务。
func TestLoadConfigRejectsDuplicateExternalRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
task_periodic:
  - name: "dup-periodic"
    cron: "0 2 * * *"
    workflow: "external.workflow"
  - name: "dup-periodic"
    cron: "0 3 * * *"
    workflow: "external.workflow"
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	if _, err := LoadConfig(mainFile); err == nil {
		t.Fatal("期望重复周期任务名称返回错误，实际为 nil")
	}
}

// TestLoadConfigRejectsDuplicateAnonymousPeriodicConfig 确保匿名周期任务也按调度稳定键校验重复。
func TestLoadConfigRejectsDuplicateAnonymousPeriodicConfig(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
task_periodic:
  - cron: "0 2 * * *"
    workflow: "external.workflow"
    queue: "maintenance"
  - cron: "0 2 * * *"
    workflow: "external.workflow"
    queue: "maintenance"
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	if _, err := LoadConfig(mainFile); err == nil {
		t.Fatal("期望匿名周期任务稳定键重复返回错误，实际为 nil")
	}
}

// TestLoadConfigRejectsPartialTaskRedisConfig 确保 task.redis 非地址字段不会被静默忽略。
func TestLoadConfigRejectsPartialTaskRedisConfig(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
task:
  redis:
    db: 3
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte("task_periodic: []\n"), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	if _, err := LoadConfig(mainFile); err == nil {
		t.Fatal("期望 task.redis 半配置返回错误，实际为 nil")
	}
}

// TestLoadConfigAllowsMainRuntimeConfigSections 验证主配置运行期块不影响基础加载。
func TestLoadConfigAllowsMainRuntimeConfigSections(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
task:
  periodic:
    - name: "main-periodic"
      cron: "0 1 * * *"
      workflow: "main.workflow"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte("task_periodic: []\n"), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("主配置 task.periodic 不应额外扫描拒绝: %v", err)
	}
	if got := len(cfg.Task.Periodic); got != 1 {
		t.Fatalf("期望保留主配置中的 1 个周期任务，实际为 %d", got)
	}
}

// TestLoadConfigIgnoresMainTopLevelRuntimeConfig 验证主配置未知顶层块会被忽略。
func TestLoadConfigIgnoresMainTopLevelRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
task_periodic:
  - name: "main-top-periodic"
    cron: "0 1 * * *"
    workflow: "main.workflow"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte("task_periodic: []\n"), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("主配置未知顶层 task_periodic 不应额外扫描拒绝: %v", err)
	}
	if got := len(cfg.Task.Periodic); got != 0 {
		t.Fatalf("未知顶层 task_periodic 不应隐式写入 task.periodic，实际为 %d", got)
	}
}

// TestLoadConfigResolvesRuntimeRelativeToMainFile 确保外部配置相对路径固定按主配置目录解析。
func TestLoadConfigResolvesRuntimeRelativeToMainFile(t *testing.T) {
	dir := t.TempDir()
	mainDir := filepath.Join(dir, "main")
	workingDir := filepath.Join(dir, "working")
	if err := os.MkdirAll(filepath.Join(mainDir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建主配置外部目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workingDir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建工作目录外部目录失败: %v", err)
	}
	mainFile := filepath.Join(mainDir, "config.yaml")
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "config.d", "runtime.sample.yaml"), []byte(`
workflows:
  user_tag:
    enabled: true
    default_shard_total: 16
`), 0o644); err != nil {
		t.Fatalf("写入主配置目录 runtime 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workingDir, "config.d", "runtime.sample.yaml"), []byte(`
workflows:
  user_tag:
    enabled: true
    default_shard_total: 99
`), 0o644); err != nil {
		t.Fatalf("写入工作目录 runtime 失败: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取当前工作目录失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err = os.Chdir(workingDir); err != nil {
		t.Fatalf("切换工作目录失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if cfg.Workflows.UserTag.DefaultShardTotal != 16 {
		t.Fatalf("期望读取主配置目录 runtime，实际 workflows.user_tag.default_shard_total=%d", cfg.Workflows.UserTag.DefaultShardTotal)
	}
}

// TestLoadConfigIgnoresOldRuntimeConfigShape 确保外部运行期旧结构不会阻断当前进程启动。
func TestLoadConfigIgnoresOldRuntimeConfigShape(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatalf("创建外部配置目录失败: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte(minimalConfigYAML(`
config_files:
  runtime: "config.d/runtime.sample.yaml"
`)), 0o644); err != nil {
		t.Fatalf("写入主配置失败: %v", err)
	}
	runtimeFile := filepath.Join(dir, "config.d", "runtime.sample.yaml")
	if err := os.WriteFile(runtimeFile, []byte(`
task:
  periodic:
    - name: "old-periodic"
      cron: "0 2 * * *"
      workflow: "old.workflow"
`), 0o644); err != nil {
		t.Fatalf("写入外部配置失败: %v", err)
	}
	cfg, err := LoadConfig(mainFile)
	if err != nil {
		t.Fatalf("旧结构外部配置不应导致启动失败: %v", err)
	}
	if len(cfg.Task.Periodic) != 0 {
		t.Fatalf("旧结构 task.periodic 不应被运行期文件隐式合并，实际数量=%d", len(cfg.Task.Periodic))
	}
}

// minimalConfigYAML 构造配置解析测试需要的最小主配置。
func minimalConfigYAML(extra string) string {
	return `
name: test
host: 127.0.0.1
port: 8888
jwt_secret: test-secret
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
