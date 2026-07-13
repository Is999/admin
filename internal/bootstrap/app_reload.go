package bootstrap

import (
	"context"
	"strings"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/bootstrap/configload"
	"admin/internal/bootstrap/hotreload"
	"admin/internal/bootstrap/runtimewatch"
	"admin/internal/config"
	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// reloadConfigFile 串行执行一次配置文件重载，供 watcher 和手动接口共用。
func (a *App) reloadConfigFile(ctx context.Context, source string, configFile string) (string, error) {
	if a == nil {
		return "", errors.Errorf("应用实例为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	configFile = strings.TrimSpace(configFile)
	if configFile == "" {
		return "", errors.Errorf("未绑定配置文件路径")
	}
	a.hotReload.LockExec()
	reconcileWatcher := false
	reconcileEnabled := false
	defer func() {
		a.hotReload.UnlockExec()
		if !reconcileWatcher {
			return
		}
		if reconcileEnabled && !a.isConfigHotReloadRunning() {
			a.startConfigHotReload()
		}
		if !reconcileEnabled && hotreload.Source(source) != "watcher" {
			_ = a.stopConfigHotReload(context.Background())
		}
	}()
	select {
	case <-ctx.Done():
		err := errors.Tag(ctx.Err())
		a.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadCancelled, "配置热加载已取消", err, "", source, "cancelled", configFile)
		return "", err
	default:
	}

	loaded, err := a.loadReloadConfig(ctx, source, configFile)
	if err != nil {
		return "", errors.Tag(err)
	}
	if loaded.Unchanged {
		return loaded.PreviousFingerprint, nil
	}
	applied := a.applyReloadedConfig(loaded.Before, loaded.Requested)
	a.hotReloadRecorder().Success(ctx, source, configFile, loaded, applied)
	reconcileWatcher = true
	reconcileEnabled = applied.Effective.HotReload.Enabled
	return loaded.PreviousFingerprint, nil
}

// loadReloadConfig 读取配置文件并合并 DB 运行配置快照。
func (a *App) loadReloadConfig(ctx context.Context, source, configFile string) (hotreload.LoadedFile, error) {
	beforeCfg := a.CurrentConfig()
	currentFingerprint, err := configload.BundleFingerprint(configFile)
	if err != nil {
		a.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadFingerprintReadFailed, "读取配置文件指纹失败", err, "", source, "fingerprint", configFile)
		return hotreload.LoadedFile{}, errors.Tag(err)
	}
	previousFingerprint := a.hotReloadRecorder().CurrentVersion()
	if previousFingerprint != "" && currentFingerprint == previousFingerprint {
		a.hotReloadRecorder().Unchanged(configFile, source, currentFingerprint)
		return hotreload.LoadedFile{
			Before:              beforeCfg,
			CurrentFingerprint:  currentFingerprint,
			PreviousFingerprint: previousFingerprint,
			Unchanged:           true,
		}, nil
	}
	cfg, err := LoadConfig(configFile)
	if err != nil {
		a.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadFailed, "配置热加载失败", err, currentFingerprint, source, "load", configFile)
		return hotreload.LoadedFile{}, errors.Tag(err)
	}
	if loadErr := runtimewatch.ApplyActiveSnapshot(ctx, a.ServiceContext, beforeCfg, &cfg); loadErr != nil {
		a.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadRuntimeConfigLoadFailed, "加载数据库运行配置失败", loadErr, currentFingerprint, source, "load_runtime_config", configFile)
		return hotreload.LoadedFile{}, errors.Tag(loadErr)
	}
	return hotreload.LoadedFile{
		Before:              beforeCfg,
		Requested:           cfg,
		CurrentFingerprint:  currentFingerprint,
		PreviousFingerprint: previousFingerprint,
	}, nil
}

// applyReloadedConfig 发布重载后的配置；需重启字段会保留当前运行值。
func (a *App) applyReloadedConfig(before, after config.Config) hotreload.AppliedConfig {
	restartRequired, restartReason := configload.DetectReloadRestartImpact(before, after)
	effective := after
	if restartRequired {
		effective = configload.BuildReloadEffectiveConfig(before, after)
	}
	publishRuntimeConfig(effective)
	a.UpdateConfig(effective)
	return hotreload.AppliedConfig{
		Effective:       effective,
		RestartRequired: restartRequired,
		RestartReason:   restartReason,
	}
}

// stopConfigHotReload 停止配置热加载后台协程。
func (a *App) stopConfigHotReload(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return errors.Tag(a.hotReload.StopWatcher(ctx))
}

// startConfigHotReload 在启用时启动后台配置轮询协程。
// 该协程面向 K8s ConfigMap 原子替换场景，只负责刷新配置快照，不在线重建基础设施连接。
func (a *App) startConfigHotReload() {
	if a == nil {
		return
	}
	cfg := a.CurrentConfig()
	interval := hotreload.CheckInterval(cfg.HotReload.CheckIntervalSeconds)
	configFile := a.boundConfigFile()
	a.hotReloadRecorder().WatcherStarting(cfg, configFile, interval)
	if configFile == "" || !cfg.HotReload.Enabled {
		return
	}
	if !a.hotReload.StartWatcher(func(ctx context.Context) {
		a.watchConfigFile(ctx, configFile)
	}) {
		return
	}
	loggerx.Infow(context.Background(), "配置 热加载已启用",
		logx.Field("file", configFile),
		logx.Field(loggerx.FieldIntervalSeconds, int(interval/time.Second)),
	)
}

// watchConfigFile 轮询配置文件指纹，检测到变化后重新解析并刷新配置快照。
func (a *App) watchConfigFile(ctx context.Context, configFile string) {
	interval := hotreload.CheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds)
	lastFingerprint, err := configload.BundleFingerprint(configFile)
	if err != nil {
		recorder := a.hotReloadRecorder()
		recorder.Failure(i18n.MsgKeyHotReloadFingerprintInitFailed, "初始化配置文件指纹失败", err, "", "startup", "fingerprint", configFile)
		recorder.WatcherInitFailed(configFile, interval)
		return
	}
	a.hotReloadRecorder().WatcherRunning(configFile, lastFingerprint, interval)
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		if !a.CurrentConfig().HotReload.Enabled {
			a.hotReloadRecorder().WatcherDisabled(false)
			return
		}
		select {
		case <-ctx.Done():
			a.hotReloadRecorder().WatcherStopped()
			return
		case <-timer.C:
			a.hotReloadRecorder().Checked(time.Now())
			currentFingerprint, statErr := configload.BundleFingerprint(configFile)
			if statErr != nil {
				a.hotReloadRecorder().Failure(i18n.MsgKeyHotReloadFileStatusReadFailed, "读取配置文件状态失败", statErr, "", "watcher", "fingerprint", configFile)
				timer.Reset(hotreload.CheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds))
				continue
			}
			if currentFingerprint != lastFingerprint {
				previousFingerprint, reloadErr := a.reloadConfigFile(ctx, "watcher", configFile)
				if reloadErr == nil {
					_ = previousFingerprint
					lastFingerprint = a.hotReloadRecorder().CurrentVersion()
				}
			}
			if !a.CurrentConfig().HotReload.Enabled {
				a.hotReloadRecorder().WatcherDisabled(true)
				return
			}
			timer.Reset(hotreload.CheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds))
		}
	}
}

// boundConfigFile 返回当前绑定的配置文件路径快照。
func (a *App) boundConfigFile() string {
	if a == nil {
		return ""
	}
	return a.hotReload.ConfigFile()
}

// isConfigHotReloadRunning 返回当前是否已有热加载 watcher 在运行。
func (a *App) isConfigHotReloadRunning() bool {
	if a == nil {
		return false
	}
	return a.hotReload.WatcherRunning()
}

// hotReloadRecorder 返回当前 App 绑定的热加载状态记录器。
func (a *App) hotReloadRecorder() hotreload.Recorder {
	if a == nil {
		return hotreload.Recorder{}
	}
	return hotreload.Recorder{
		State:          &a.hotReload,
		ServiceContext: a.ServiceContext,
		CurrentConfig:  a.CurrentConfig,
		NotifyFailure:  a.notifyConfigReloadFailure,
	}
}
