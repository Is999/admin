package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Is999/go-utils/errors"

	i18n "admin/common/i18n"
	"admin/common/runtimecfg"
	"admin/internal/bootstrap/configload"
	"admin/internal/config"
	"admin/internal/svc"
)

// TestMarkHotReloadFailureSuppressesRepeatedLogs 确保重复失败会累计到状态快照的抑制计数中。
func TestMarkHotReloadFailureSuppressesRepeatedLogs(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	app := &App{
		ServiceContext: svcCtx,
	}

	app.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadFailed, "配置热加载失败", errors.New("boom"), "v1", "watcher", "load", "/tmp/config.yaml")
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

	app.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadFailed, "配置热加载失败", errors.New("boom"), "v1", "watcher", "load", "/tmp/config.yaml")
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
	fingerprint, err := configload.BundleFingerprint(configFile)
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
	if status.LastMessageKey != i18n.MsgKeyHotReloadUnchanged {
		t.Fatalf("期望记录配置无变化 key，实际为 %q", status.LastMessageKey)
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
		t.Fatalf("期望 Redis 运行配置保持原值 %q，实际为 %q", beforeCfg.Redis.Addrs[0], got)
	}
	if got := current.AppKey; got != beforeCfg.AppKey {
		t.Fatalf("期望数据密钥保持原值 %q，实际为 %q", beforeCfg.AppKey, got)
	}
	status := svcCtx.CurrentHotReloadStatus()
	if !status.RestartRequired || !strings.Contains(status.RestartReason, "redis") || !strings.Contains(status.RestartReason, "app_key") {
		t.Fatalf("期望标记 Redis 与 app_key 变更需重启，实际状态为 %+v", status)
	}
}

// TestReloadConfigFileDisablesWatcherAfterUnlock 确保手动关闭热加载时不会与等待重载锁的 watcher 互相死锁。
func TestReloadConfigFileDisablesWatcherAfterUnlock(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(minimalConfigYAML(`
hot_reload:
  enabled: false
`)), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	beforeCfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载初始配置失败: %v", err)
	}
	beforeCfg.HotReload.Enabled = true
	svcCtx := svc.NewServiceContext(beforeCfg, svc.Dependencies{})
	app := &App{ServiceContext: svcCtx}
	app.UpdateConfig(beforeCfg)

	watcherStarted := make(chan struct{})
	watcherFinished := make(chan struct{})
	if ok := app.hotReload.StartWatcher(func(ctx context.Context) {
		close(watcherStarted)
		<-ctx.Done()
		app.hotReload.LockExec()
		app.hotReload.UnlockExec()
		close(watcherFinished)
	}); !ok {
		t.Fatal("启动测试 watcher 失败")
	}
	<-watcherStarted

	reloadDone := make(chan error, 1)
	go func() {
		_, reloadErr := app.reloadConfigFile(context.Background(), "manual_api", configFile)
		reloadDone <- reloadErr
	}()
	select {
	case reloadErr := <-reloadDone:
		if reloadErr != nil {
			t.Fatalf("关闭热加载失败: %v", reloadErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("关闭热加载发生死锁")
	}
	select {
	case <-watcherFinished:
	case <-time.After(time.Second):
		t.Fatal("关闭热加载后 watcher 未退出")
	}
	if app.isConfigHotReloadRunning() {
		t.Fatal("关闭热加载后 watcher 仍显示运行中")
	}
}

// minimalConfigYAML 构造配置热加载测试需要的最小主配置。
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
