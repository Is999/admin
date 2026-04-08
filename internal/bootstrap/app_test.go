package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin_cron/internal/config"
	"admin_cron/internal/svc"
	"admin_cron/internal/taskqueue"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest"
)

// TestAppUpdateConfigPropagatesSnapshot 确保应用配置更新会同步刷新 ServiceContext 与 TaskManager 快照。
func TestAppUpdateConfigPropagatesSnapshot(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		JwtSecret: "old-secret",
		Task:      config.TaskQueueConfig{DefaultQueue: "old-queue"},
	}, svc.Dependencies{})

	app := &App{ServiceContext: svcCtx}
	app.UpdateConfig(config.Config{
		JwtSecret: "new-secret",
		Task:      config.TaskQueueConfig{Enabled: true, DefaultQueue: "new-queue", Periodic: []config.TaskPeriodicConfig{{Name: "demo"}}},
		Workflows: config.WorkflowsConfig{UserTag: config.UserTagConfig{Enabled: true}},
		HotReload: config.HotReloadConfig{Enabled: true},
		Kafka:     config.KafkaConfig{Enabled: true},
	})

	if got := app.CurrentConfig().JwtSecret; got != "new-secret" {
		t.Fatalf("期望应用配置中的 JwtSecret 已更新，实际为 %q", got)
	}
	if got := svcCtx.CurrentConfig().JwtSecret; got != "new-secret" {
		t.Fatalf("期望 ServiceContext 中的 JwtSecret 已更新，实际为 %q", got)
	}
	if got := svcCtx.CurrentHotReloadStatus().ConfigSummary; got == "" {
		t.Fatal("期望热加载配置摘要已更新")
	}
}

// TestAppUpdateConfigPropagatesTaskManagerSnapshot 验证任务系统快照会随配置热更新。
func TestAppUpdateConfigPropagatesTaskManagerSnapshot(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:      true,
		DefaultQueue: "old-queue",
	}, client)
	if manager == nil {
		t.Fatal("期望成功创建任务管理器")
	}
	t.Cleanup(func() {
		_ = manager.Stop(context.Background())
	})

	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "old-app",
		Task:  config.TaskQueueConfig{Enabled: true, DefaultQueue: "old-queue"},
	}, svc.Dependencies{Rds: client})
	app := &App{
		ServiceContext: svcCtx,
		TaskManager:    manager,
	}

	app.UpdateConfig(config.Config{
		AppID: "site-1",
		Task: config.TaskQueueConfig{
			Enabled:      true,
			DefaultQueue: "maintenance",
			Queues: map[string]int{
				"default": 1,
			},
		},
	})

	if got := manager.CurrentConfig().DefaultQueue; got != "maintenance" {
		t.Fatalf("期望任务系统默认队列已刷新，实际为 %q", got)
	}
	if got := manager.CurrentConfig().AppID; got != "site-1" {
		t.Fatalf("期望任务系统继承顶层 app_id，实际为 %q", got)
	}
}

// TestNormalizeModeSupportsDocumentedCombinations 确保启动模式只接受文档声明的 1..7 组合。
func TestNormalizeModeSupportsDocumentedCombinations(t *testing.T) {
	tests := []struct {
		name string
		mode int
		want int
	}{
		{name: "default", mode: 0, want: ModeAll},
		{name: "api", mode: 1, want: ModeAPI},
		{name: "worker", mode: 2, want: ModeWorker},
		{name: "api_worker", mode: 3, want: ModeAPI | ModeWorker},
		{name: "scheduler", mode: 4, want: ModeScheduler},
		{name: "api_scheduler", mode: 5, want: ModeAPI | ModeScheduler},
		{name: "worker_scheduler", mode: 6, want: ModeWorker | ModeScheduler},
		{name: "all", mode: 7, want: ModeAll},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMode(tt.mode)
			if err != nil {
				t.Fatalf("规范化启动模式失败: %v", err)
			}
			if got != tt.want {
				t.Fatalf("期望启动模式为 %d，实际为 %d", tt.want, got)
			}
		})
	}
}

// TestNormalizeModeRejectsUnsupportedCombinations 确保非法模式不会被按位裁剪后误启动。
func TestNormalizeModeRejectsUnsupportedCombinations(t *testing.T) {
	for _, mode := range []int{-1, 8, 9, 15} {
		if _, err := normalizeMode(mode); err == nil {
			t.Fatalf("期望非法启动模式 %d 返回错误，实际为 nil", mode)
		}
	}
}

// TestModeLifecycleMatrix 确保运行模式与后台生命周期职责保持一致。
func TestModeLifecycleMatrix(t *testing.T) {
	tests := []struct {
		mode       int
		workerHook bool
		taskHook   bool
		api        bool
	}{
		{mode: 1, workerHook: false, taskHook: false, api: true},
		{mode: 2, workerHook: true, taskHook: true, api: false},
		{mode: 3, workerHook: true, taskHook: true, api: true},
		{mode: 4, workerHook: false, taskHook: true, api: false},
		{mode: 5, workerHook: false, taskHook: true, api: true},
		{mode: 6, workerHook: true, taskHook: true, api: false},
		{mode: 7, workerHook: true, taskHook: true, api: true},
	}

	for _, tt := range tests {
		if got := shouldStartWorkerLifecycle(tt.mode); got != tt.workerHook {
			t.Fatalf("mode=%d Worker 生命周期期望=%v 实际=%v", tt.mode, tt.workerHook, got)
		}
		if got := shouldStartTaskLifecycle(tt.mode); got != tt.taskHook {
			t.Fatalf("mode=%d 任务生命周期期望=%v 实际=%v", tt.mode, tt.taskHook, got)
		}
		if got := hasMode(tt.mode, ModeAPI); got != tt.api {
			t.Fatalf("mode=%d API 期望=%v 实际=%v", tt.mode, tt.api, got)
		}
	}
}

// TestFormatMode 确保位掩码模式按“数字(角色组合)”格式输出，便于和 run_mode 配置对照。
func TestFormatMode(t *testing.T) {
	tests := []struct {
		name string
		mode int
		want string
	}{
		{name: "none", mode: 0, want: "none"},
		{name: "api", mode: 1, want: "1(api)"},
		{name: "worker", mode: 2, want: "2(worker)"},
		{name: "scheduler", mode: 4, want: "4(scheduler)"},
		{name: "all", mode: 7, want: "7(api+worker+scheduler)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMode(tt.mode); got != tt.want {
				t.Fatalf("mode=%d 格式化结果不符合预期，期望=%q，实际=%q", tt.mode, tt.want, got)
			}
		})
	}
}

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
	first, err := configBundleFingerprint(mainFile)
	if err != nil {
		t.Fatalf("获取首次配置包指纹失败: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err = os.WriteFile(runtimeFile, []byte("archive_jobs: []\n"), 0o644); err != nil {
		t.Fatalf("重写外部配置失败: %v", err)
	}
	second, err := configBundleFingerprint(mainFile)
	if err != nil {
		t.Fatalf("获取第二次配置包指纹失败: %v", err)
	}
	if first == second {
		t.Fatalf("期望外部配置重写后指纹发生变化，实际仍为 %q", first)
	}
}

// TestMarkHotReloadFailureSuppressesRepeatedLogs 确保重复失败会累计到状态快照的抑制计数中。
func TestMarkHotReloadFailureSuppressesRepeatedLogs(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	app := &App{
		ServiceContext: svcCtx,
	}

	app.markHotReloadFailure("配置热加载失败", errors.New("boom"), "v1", "watcher", "load", "/tmp/config.yaml")
	first := svcCtx.CurrentHotReloadStatus()
	if first.LastStatus != "failed" {
		t.Fatalf("期望状态为 failed，实际为 %q", first.LastStatus)
	}
	if first.LastTriggerSource != "watcher" || first.LastFailureCategory != "load" {
		t.Fatalf("期望记录失败来源和分类，实际为 %+v", first)
	}
	if first.SuppressedFailureCount != 0 {
		t.Fatalf("期望首次失败不被抑制，实际计数为 %d", first.SuppressedFailureCount)
	}

	app.markHotReloadFailure("配置热加载失败", errors.New("boom"), "v1", "watcher", "load", "/tmp/config.yaml")
	second := svcCtx.CurrentHotReloadStatus()
	if second.SuppressedFailureCount != 1 {
		t.Fatalf("期望失败抑制计数为 1，实际为 %d", second.SuppressedFailureCount)
	}
}

// TestWatchConfigFileInitFailureMarksWatchingFalse 确保热加载 watcher 初始化失败时不会继续显示为 watching=true。
func TestWatchConfigFileInitFailureMarksWatchingFalse(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		HotReload: config.HotReloadConfig{Enabled: true, CheckIntervalSeconds: 3},
	}, svc.Dependencies{})
	missingFile := filepath.Join(t.TempDir(), "missing.yaml")
	app := &App{
		ServiceContext: svcCtx,
	}

	app.watchConfigFile(context.Background(), missingFile)

	status := svcCtx.CurrentHotReloadStatus()
	if status.Watching {
		t.Fatal("期望 watcher 初始化失败后状态为未监听，实际仍为 watching=true")
	}
	if status.LastStatus != "failed" {
		t.Fatalf("期望失败状态为 failed，实际为 %q", status.LastStatus)
	}
}

// TestBindConfigFileTrimsAndUpdatesStatus 确保配置文件绑定路径会做裁剪，并同步更新热加载状态快照。
func TestBindConfigFileTrimsAndUpdatesStatus(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	app := &App{ServiceContext: svcCtx}

	app.BindConfigFile("  /tmp/runtime.sample.yaml  ")

	if got := app.boundConfigFile(); got != "/tmp/runtime.sample.yaml" {
		t.Fatalf("期望绑定后的配置路径已裁剪，实际为 %q", got)
	}
	if got := svcCtx.CurrentHotReloadStatus().ConfigFile; got != "/tmp/runtime.sample.yaml" {
		t.Fatalf("期望热加载状态中的配置路径已更新，实际为 %q", got)
	}
}

// TestReloadConfigFileUsesExplicitFileSnapshot 确保重载流程使用传入的文件快照，避免后台流程依赖共享路径状态。
func TestReloadConfigFileUsesExplicitFileSnapshot(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(minimalConfigYAML(`
hot_reload:
  enabled: false
  check_interval_seconds: 3
`)), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	app := &App{ServiceContext: svcCtx}
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载初始配置失败: %v", err)
	}
	app.UpdateConfig(cfg)

	if _, err := app.reloadConfigFile(context.Background(), "manual_api", configFile); err != nil {
		t.Fatalf("按显式配置文件快照重载失败: %v", err)
	}

	status := svcCtx.CurrentHotReloadStatus()
	if status.ConfigFile != configFile {
		t.Fatalf("期望状态中记录显式重载文件 %q，实际为 %q", configFile, status.ConfigFile)
	}
	if app.CurrentConfig().Name != "test" {
		t.Fatalf("期望应用配置已按显式文件刷新，实际 Name=%q", app.CurrentConfig().Name)
	}
}

// TestReloadConfigFileKeepsRestartOnlyConfigPending 验证需重启配置不会被冒充为已生效。
func TestReloadConfigFileKeepsRestartOnlyConfigPending(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	beforeYAML := strings.Replace(minimalConfigYAML(`
app_key: "old-key"
hot_reload:
  enabled: false
`), "127.0.0.1:6379", "127.0.0.1:6380", 1)
	if err := os.WriteFile(configFile, []byte(beforeYAML), 0o644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}
	beforeCfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载初始配置失败: %v", err)
	}
	svcCtx := svc.NewServiceContext(beforeCfg, svc.Dependencies{})
	app := &App{ServiceContext: svcCtx}
	app.UpdateConfig(beforeCfg)

	afterYAML := strings.Replace(minimalConfigYAML(`
app_key: "new-key"
hot_reload:
  enabled: false
`), "127.0.0.1:6379", "127.0.0.1:6381", 1)
	if err = os.WriteFile(configFile, []byte(afterYAML), 0o644); err != nil {
		t.Fatalf("写入热加载配置失败: %v", err)
	}

	if _, err = app.reloadConfigFile(context.Background(), "manual_api", configFile); err != nil {
		t.Fatalf("热加载配置失败: %v", err)
	}

	current := app.CurrentConfig()
	if got := current.Redis.Addrs[0]; got != beforeCfg.Redis.Addrs[0] {
		t.Fatalf("期望 Redis 运行配置保持旧值 %q，实际为 %q", beforeCfg.Redis.Addrs[0], got)
	}
	if got := current.AppKey; got != "new-key" {
		t.Fatalf("期望动态配置 app_key 已刷新，实际为 %q", got)
	}
	status := svcCtx.CurrentHotReloadStatus()
	if !status.RestartRequired || !strings.Contains(status.RestartReason, "redis") {
		t.Fatalf("期望标记 Redis 变更需重启，实际状态为 %+v", status)
	}
}

// TestDetectHotReloadRestartImpact 确保基础设施配置变更会明确标记“需重启才能完全生效”。
func TestDetectHotReloadRestartImpact(t *testing.T) {
	before := config.Config{
		RestConf: rest.RestConf{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Redis: config.RedisConfig{
			Addrs: []string{"127.0.0.1:6379"},
		},
	}
	after := before
	after.Redis = config.RedisConfig{
		Addrs: []string{"127.0.0.1:6380"},
	}

	restartRequired, restartReason := detectHotReloadRestartImpact(before, after)
	if !restartRequired {
		t.Fatal("期望 Redis 配置变更后标记为需重启生效，实际为 false")
	}
	if !strings.Contains(restartReason, "redis") {
		t.Fatalf("期望重启原因包含 redis，实际为 %q", restartReason)
	}

	after = before
	after.Workflows.UserTag.Enabled = true
	restartRequired, restartReason = detectHotReloadRestartImpact(before, after)
	if !restartRequired {
		t.Fatal("期望用户标签插件启停变更后标记为需重启生效，实际为 false")
	}
	if !strings.Contains(restartReason, "workflows.user_tag.enabled") {
		t.Fatalf("期望重启原因包含 workflows.user_tag.enabled，实际为 %q", restartReason)
	}
}

// TestValidateTaskRedisConfigRejectsPartial 确保 task.redis 半配置不会静默回退到主 Redis。
func TestValidateTaskRedisConfigRejectsPartial(t *testing.T) {
	if err := validateTaskRedisConfig(config.RedisConfig{DB: 3}); err == nil {
		t.Fatal("期望 task.redis 只配置 db 时返回错误，实际为 nil")
	}
	if err := validateTaskRedisConfig(config.RedisConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 3}); err != nil {
		t.Fatalf("期望完整 task.redis 配置通过，实际错误为 %v", err)
	}
}
