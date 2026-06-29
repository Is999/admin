package bootstrap

import (
	"context"
	"sync"
	"time"

	"admin/internal/config"
	"admin/internal/infra/loggerx"
	runtimeconfig "admin/internal/logic/runtimeconfig"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// runtimeConfigWatchState 保存 DB 运行配置轻量轮询资源。
type runtimeConfigWatchState struct {
	cancel       context.CancelFunc // 后台轮询取消函数
	stateMu      sync.Mutex         // 保护 watcher 生命周期
	wg           sync.WaitGroup     // 等待 watcher 退出
	lastVersion  uint64             // 最近一次已应用版本号
	lastChecksum string             // 最近一次已应用快照校验和
}

// applyStartupRuntimeConfig 在组件注册前应用 DB active release。
func applyStartupRuntimeConfig(ctx context.Context, svcCtx *svc.ServiceContext, cfg *config.Config) error {
	if cfg == nil || !runtimeconfig.IsDatabaseSource(*cfg) {
		return nil
	}
	active, err := runtimeconfig.EnsureInitialRelease(ctx, svcCtx)
	if err != nil {
		return errors.Wrap(err, "加载或初始化数据库运行配置 active release 失败")
	}
	runtimeconfig.ApplySnapshot(cfg, active.Snapshot)
	svcCtx.UpdateConfig(*cfg)
	publishRuntimeConfig(*cfg)
	return nil
}

// startRuntimeConfigWatcher 启动 DB 运行配置版本轻量轮询。
func (a *App) startRuntimeConfigWatcher() {
	if a == nil || !runtimeconfig.IsDatabaseSource(a.CurrentConfig()) {
		return
	}
	a.runtimeConfig.stateMu.Lock()
	if a.runtimeConfig.cancel != nil {
		a.runtimeConfig.stateMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.runtimeConfig.cancel = cancel
	a.runtimeConfig.stateMu.Unlock()
	a.runtimeConfig.wg.Add(1)
	go func() {
		defer a.runtimeConfig.wg.Done()
		a.watchRuntimeConfig(ctx)
	}()
	cfg := a.CurrentConfig()
	loggerx.Infow(ctx, "运行配置 DB热加载已启用",
		logx.Field(loggerx.FieldIntervalSeconds, runtimeconfig.PollIntervalSeconds(cfg)),
	)
}

// stopRuntimeConfigWatcher 停止 DB 运行配置轮询。
func (a *App) stopRuntimeConfigWatcher() {
	if a == nil {
		return
	}
	a.runtimeConfig.stateMu.Lock()
	if a.runtimeConfig.cancel == nil {
		a.runtimeConfig.stateMu.Unlock()
		return
	}
	cancel := a.runtimeConfig.cancel
	a.runtimeConfig.cancel = nil
	a.runtimeConfig.stateMu.Unlock()
	cancel()
	a.runtimeConfig.wg.Wait()
}

// watchRuntimeConfig 只轮询 active state 版本号，版本不变时不读取发布快照。
func (a *App) watchRuntimeConfig(ctx context.Context) {
	timer := time.NewTimer(runtimeConfigPollDelay(a.CurrentConfig()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !runtimeconfig.IsDatabaseSource(a.CurrentConfig()) {
				return
			}
			if err := a.reloadRuntimeConfigIfChanged(ctx); err != nil {
				loggerx.ErrorTextw(ctx, "运行配置 DB热加载失败", err.Error(),
					logx.Field("source", "watcher"),
				)
				a.notifyRuntimeConfigReloadFailure(ctx, "watcher", err)
			}
			timer.Reset(runtimeConfigPollDelay(a.CurrentConfig()))
		}
	}
}

// reloadRuntimeConfigIfChanged 检测 active release 版本变化并触发运行配置重载。
func (a *App) reloadRuntimeConfigIfChanged(ctx context.Context) error {
	state, err := runtimeconfig.LoadActiveStateCached(ctx, a.ServiceContext)
	if err != nil {
		return errors.Tag(err)
	}
	if state.ActiveReleaseID == 0 {
		return errors.Errorf("运行配置未发布 active release")
	}
	if state.ActiveVersion == a.runtimeConfig.lastVersion && state.ActiveChecksum == a.runtimeConfig.lastChecksum {
		return nil
	}
	result, err := a.ReloadRuntimeConfig(ctx, "runtime_config_watcher")
	if err != nil {
		return errors.Tag(err)
	}
	a.runtimeConfig.lastVersion = result.VersionNo
	a.runtimeConfig.lastChecksum = result.Checksum
	return nil
}

// ReloadRuntimeConfig 应用当前 active release，供发布接口和 watcher 复用。
func (a *App) ReloadRuntimeConfig(ctx context.Context, source string) (svc.RuntimeConfigReloadResult, error) {
	if a == nil {
		return svc.RuntimeConfigReloadResult{}, errors.Errorf("应用实例为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !runtimeconfig.IsDatabaseSource(a.CurrentConfig()) {
		return svc.RuntimeConfigReloadResult{}, nil
	}
	a.hotReload.execMu.Lock()
	defer a.hotReload.execMu.Unlock()
	active, err := runtimeconfig.LoadActiveSnapshotCached(ctx, a.ServiceContext)
	if err != nil {
		return svc.RuntimeConfigReloadResult{}, errors.Tag(err)
	}
	before := a.CurrentConfig()
	after := before
	runtimeconfig.ApplySnapshot(&after, active.Snapshot)
	restartRequired, restartReason := detectHotReloadRestartImpact(before, after)
	effective := after
	if restartRequired {
		effective = buildHotReloadEffectiveConfig(before, after)
	}
	publishRuntimeConfig(effective)
	a.UpdateConfig(effective)
	a.runtimeConfig.lastVersion = active.Release.VersionNo
	a.runtimeConfig.lastChecksum = active.Release.Checksum
	loggerx.Infow(ctx, "运行配置 DB热加载成功",
		logx.Field("source", normalizeHotReloadSource(source)),
		logx.Field("release_id", active.Release.ID),
		logx.Field("version", active.Release.VersionNo),
		logx.Field("checksum", active.Release.Checksum),
		logx.Field("restart_required", restartRequired),
		logx.Field("restart_reason", restartReason),
	)
	return svc.RuntimeConfigReloadResult{
		ReleaseID:       active.Release.ID,
		VersionNo:       active.Release.VersionNo,
		Checksum:        active.Release.Checksum,
		RestartRequired: restartRequired,
		RestartReason:   restartReason,
	}, nil
}

// runtimeConfigPollDelay 返回带轻微抖动的 DB 运行配置轮询间隔，避免多实例同时打库。
func runtimeConfigPollDelay(cfg config.Config) time.Duration {
	interval := time.Duration(runtimeconfig.PollIntervalSeconds(cfg)) * time.Second
	jitter := interval / 10
	if jitter <= 0 {
		return interval
	}
	return interval + time.Duration(time.Now().UnixNano()%int64(jitter))
}
