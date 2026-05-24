package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/task/queue"

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
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "old-app"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "old-app",
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

	runtimecfg.Set(config.Config{AppID: "site-1"})
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

// TestBuildServiceContextDoesNotPublishRuntimeConfigOnFailure 确保启动失败不会污染进程级运行配置。
func TestBuildServiceContextDoesNotPublishRuntimeConfigOnFailure(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "stable-app"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	svcCtx, shutdown, err := BuildServiceContext(context.Background(), config.Config{AppID: "failed-app"})
	if err == nil {
		if svcCtx != nil {
			_ = closeServiceContextResources(svcCtx, nil, false)
		}
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		t.Fatal("期望缺少 MySQL 配置时启动失败")
	}
	if got := runtimecfg.AppID(); got != "stable-app" {
		t.Fatalf("启动失败后 runtimecfg.AppID() = %q, want stable-app", got)
	}
}

// TestCollectorConfigWithAppIDScopesRedisStream 确保 Collector Redis Stream 按 app_id 隔离。
func TestCollectorConfigWithAppIDScopesRedisStream(t *testing.T) {
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-1"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	cfg := collectorConfigWithAppID(config.Config{
		AppID: "site-1",
		Collector: config.CollectorConfig{
			Redis: config.CollectorRedisConfig{Stream: "collector:events"},
		},
	})
	if got := cfg.Redis.Stream; got != "app:site-1:collector:events" {
		t.Fatalf("期望 Collector Redis Stream 按 app_id 加前缀，实际为 %q", got)
	}

	runtimecfg.Set(config.Config{AppID: "site-2"})
	cfg = collectorConfigWithAppID(config.Config{
		AppID: "site-2",
		Collector: config.CollectorConfig{
			Redis: config.CollectorRedisConfig{Stream: "app:site-1:collector:events"},
		},
	})
	if got := cfg.Redis.Stream; got != "" {
		t.Fatalf("期望已带其它 app 前缀的 Collector Redis Stream 失败闭合，实际为 %q", got)
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

// TestReloadConfigFileSkipsUnchangedSnapshot 确保配置未变化时不重复发布运行态配置。
func TestReloadConfigFileSkipsUnchangedSnapshot(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(minimalConfigYAML(`
hot_reload:
  enabled: false
`)), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载初始配置失败: %v", err)
	}
	fingerprint, err := configBundleFingerprint(configFile)
	if err != nil {
		t.Fatalf("计算配置指纹失败: %v", err)
	}
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{})
	svcCtx.UpdateHotReloadStatus(svc.HotReloadStatus{
		ConfigVersion: fingerprint,
		ReloadCount:   3,
	})
	app := &App{ServiceContext: svcCtx}
	app.UpdateConfig(cfg)

	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "stable-app"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	if _, err = app.reloadConfigFile(context.Background(), "manual_api", configFile); err != nil {
		t.Fatalf("无变化热加载失败: %v", err)
	}

	status := svcCtx.CurrentHotReloadStatus()
	if status.ReloadCount != 3 {
		t.Fatalf("配置无变化不应增加 ReloadCount，实际为 %d", status.ReloadCount)
	}
	if status.LastMessage != "配置无变化" {
		t.Fatalf("期望记录配置无变化，实际为 %q", status.LastMessage)
	}
	if got := runtimecfg.AppID(); got != "stable-app" {
		t.Fatalf("配置无变化不应重复设置 runtimecfg，实际 app_id=%q", got)
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

	after = before
	after.Task.Periodic = []config.TaskPeriodicConfig{{Name: "runtime-periodic", Workflow: "runtime.workflow"}}
	restartRequired, restartReason = detectHotReloadRestartImpact(before, after)
	if restartRequired {
		t.Fatalf("单独变更 task.periodic 不应要求重启，实际原因为 %q", restartReason)
	}
}

// TestHotReloadRestartSpecsValid 确保热加载重启边界规格完整且顺序稳定。
func TestHotReloadRestartSpecsValid(t *testing.T) {
	specs := hotReloadRestartSpecs()
	wantReasons := []string{
		"run_mode",
		componentNameHTTPServer,
		"mode",
		"mysql",
		"site_mysql",
		"redis",
		"task.runtime",
		"kafka",
		"observability",
		"workflows.user_tag.enabled",
	}
	if len(specs) != len(wantReasons) {
		t.Fatalf("热加载重启边界数量不符合预期: got=%d want=%d", len(specs), len(wantReasons))
	}
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if spec.Reason != wantReasons[index] {
			t.Fatalf("热加载重启边界顺序不符合预期: index=%d got=%s want=%s", index, spec.Reason, wantReasons[index])
		}
		if spec.Changed == nil {
			t.Fatalf("热加载重启边界缺少变化判断: %s", spec.Reason)
		}
		if spec.Preserve == nil {
			t.Fatalf("热加载重启边界缺少旧值保留逻辑: %s", spec.Reason)
		}
		if _, ok := seen[spec.Reason]; ok {
			t.Fatalf("热加载重启边界重复: %s", spec.Reason)
		}
		seen[spec.Reason] = struct{}{}
	}
}

// TestBuildHotReloadEffectiveConfigPreservesRestartOnlyFields 确保待重启字段保留旧值，运行期字段仍可刷新。
func TestBuildHotReloadEffectiveConfigPreservesRestartOnlyFields(t *testing.T) {
	before := config.Config{
		RestConf: rest.RestConf{
			Host: "0.0.0.0",
			Port: 8080,
		},
		RunMode: 5,
		AppKey:  "old-key",
		Redis: config.RedisConfig{
			Addrs: []string{"127.0.0.1:6379"},
		},
		Task: config.TaskQueueConfig{
			Enabled:      true,
			DefaultQueue: "old-queue",
			Periodic: []config.TaskPeriodicConfig{
				{Name: "old-periodic", Workflow: "old.workflow"},
			},
		},
		Workflows: config.WorkflowsConfig{
			UserTag: config.UserTagConfig{
				Enabled:           false,
				DefaultShardTotal: 8,
			},
		},
	}
	after := before
	after.RestConf.Port = 9090
	after.RunMode = 7
	after.AppKey = "new-key"
	after.Redis = config.RedisConfig{Addrs: []string{"127.0.0.1:6380"}}
	after.Task.DefaultQueue = "new-queue"
	after.Task.Periodic = []config.TaskPeriodicConfig{{Name: "new-periodic", Workflow: "new.workflow"}}
	after.Workflows.UserTag.Enabled = true
	after.Workflows.UserTag.DefaultShardTotal = 16

	effective := buildHotReloadEffectiveConfig(before, after)
	if effective.RestConf.Port != before.RestConf.Port || effective.RunMode != before.RunMode {
		t.Fatalf("期望 HTTP 与运行模式保持旧值，实际 port=%d run_mode=%d", effective.RestConf.Port, effective.RunMode)
	}
	if effective.Redis.Addrs[0] != before.Redis.Addrs[0] {
		t.Fatalf("期望 Redis 保持旧值，实际为 %+v", effective.Redis)
	}
	if effective.Task.DefaultQueue != before.Task.DefaultQueue {
		t.Fatalf("期望任务运行时配置保持旧值，实际 default_queue=%s", effective.Task.DefaultQueue)
	}
	if len(effective.Task.Periodic) != 1 || effective.Task.Periodic[0].Name != "new-periodic" {
		t.Fatalf("期望周期任务列表刷新为新值，实际为 %+v", effective.Task.Periodic)
	}
	if effective.Workflows.UserTag.Enabled != before.Workflows.UserTag.Enabled {
		t.Fatalf("期望用户标签插件启停保持旧值，实际为 %t", effective.Workflows.UserTag.Enabled)
	}
	if effective.Workflows.UserTag.DefaultShardTotal != after.Workflows.UserTag.DefaultShardTotal {
		t.Fatalf("期望用户标签运行参数刷新为新值，实际为 %d", effective.Workflows.UserTag.DefaultShardTotal)
	}
	if effective.AppKey != after.AppKey {
		t.Fatalf("期望普通运行期配置刷新为新值，实际 app_key=%s", effective.AppKey)
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
