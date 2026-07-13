package bootstrap

import (
	"context"

	"admin/common/runtimecfg"
	"admin/internal/bootstrap/runtimewatch"
	"admin/internal/config"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
)

// publishRuntimeConfig 发布进程级运行配置快照，供 Redis key、签名和 MFA 等跨包能力读取。
func publishRuntimeConfig(c config.Config) runtimecfg.Snapshot {
	previous := runtimecfg.Get()
	runtimecfg.Set(c)
	return previous
}

// applyStartupRuntimeConfig 在组件注册前应用 DB active release。
func applyStartupRuntimeConfig(ctx context.Context, svcCtx *svc.ServiceContext, cfg *config.Config) (svc.RuntimeConfigReloadResult, error) {
	return runtimewatch.ApplyStartup(ctx, svcCtx, cfg, func(c config.Config) {
		publishRuntimeConfig(c)
	})
}

// ReloadRuntimeConfig 应用当前 active release，供发布接口和 watcher 复用。
func (a *App) ReloadRuntimeConfig(ctx context.Context, source string) (svc.RuntimeConfigReloadResult, error) {
	if a == nil {
		return svc.RuntimeConfigReloadResult{}, errors.Errorf("应用实例为空")
	}
	a.hotReload.LockExec()
	defer a.hotReload.UnlockExec()
	return runtimewatch.Reload(ctx, runtimewatch.ReloadScope{
		ServiceContext: a.ServiceContext,
		CurrentConfig:  a.CurrentConfig,
		ApplyConfig:    a.applyReloadedConfig,
		MarkApplied:    a.runtimeConfig.MarkApplied,
		Source:         source,
	})
}

// startRuntimeConfigWatcher 启动 DB 运行配置版本轻量轮询。
func (a *App) startRuntimeConfigWatcher() {
	if a == nil {
		return
	}
	runtimewatch.Start(runtimewatch.WatchScope{
		State:          &a.runtimeConfig,
		ServiceContext: a.ServiceContext,
		CurrentConfig:  a.CurrentConfig,
		Reload:         a.ReloadRuntimeConfig,
		NotifyFailure:  a.notifyRuntimeConfigReloadFailure,
	})
}

// stopRuntimeConfigWatcher 停止 DB 运行配置轮询。
func (a *App) stopRuntimeConfigWatcher(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return errors.Tag(a.runtimeConfig.Stop(ctx))
}
