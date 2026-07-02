package runtimewatch

import (
	"context"
	"time"

	"admin/internal/bootstrap/hotreload"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	runtimeconfig "admin/internal/logic/runtimeconfig"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// ConfigProvider 返回当前进程有效配置快照。
type ConfigProvider func() config.Config

// ConfigApplier 将新配置应用到进程，并返回重启影响判断。
type ConfigApplier func(before, after config.Config) hotreload.AppliedConfig

// ConfigReloader 执行一次 DB active release 重载。
type ConfigReloader func(context.Context, string) (svc.RuntimeConfigReloadResult, error)

// FailureNotifier 上报 DB 运行配置热加载失败。
type FailureNotifier func(context.Context, string, error)

// ReloadScope 收口一次 DB active release 重载需要的 App 边界。
type ReloadScope struct {
	ServiceContext *svc.ServiceContext  // 访问 runtime_config 发布快照的服务上下文
	CurrentConfig  ConfigProvider       // 读取当前配置快照
	ApplyConfig    ConfigApplier        // 应用新配置快照
	MarkApplied    func(uint64, string) // 记录已应用 active release 水位
	Source         string               // 重载来源
}

// WatchScope 收口 DB 运行配置 watcher 的依赖。
type WatchScope struct {
	State          *State              // watcher 生命周期和 active release 水位
	ServiceContext *svc.ServiceContext // 访问 runtime_config active state 的服务上下文
	CurrentConfig  ConfigProvider      // 读取当前配置快照
	Reload         ConfigReloader      // 版本变化时执行重载
	NotifyFailure  FailureNotifier     // watcher 重载失败告警
}

// ApplyStartup 在组件注册前应用 DB active release。
func ApplyStartup(ctx context.Context, svcCtx *svc.ServiceContext, cfg *config.Config, publish func(config.Config)) error {
	if cfg == nil || !runtimeconfig.IsDatabaseSource(*cfg) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	active, err := runtimeconfig.EnsureInitialRelease(ctx, svcCtx)
	if err != nil {
		return errors.Wrap(err, "加载或初始化数据库运行配置 active release 失败")
	}
	runtimeconfig.ApplySnapshot(cfg, active.Snapshot)
	if svcCtx != nil {
		svcCtx.UpdateConfig(*cfg)
	}
	if publish != nil {
		publish(*cfg)
	}
	return nil
}

// ApplyActiveSnapshot 在配置文件热加载时叠加当前 DB active release 快照。
func ApplyActiveSnapshot(ctx context.Context, svcCtx *svc.ServiceContext, before config.Config, after *config.Config) error {
	if after == nil || !runtimeconfig.IsDatabaseSource(before) || !runtimeconfig.IsDatabaseSource(*after) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	active, err := runtimeconfig.LoadActiveSnapshotCached(ctx, svcCtx)
	if err != nil {
		return errors.Tag(err)
	}
	runtimeconfig.ApplySnapshot(after, active.Snapshot)
	return nil
}

// Reload 应用当前 DB active release，供发布接口和 watcher 复用。
func Reload(ctx context.Context, scope ReloadScope) (svc.RuntimeConfigReloadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	before := scope.currentConfig()
	if !runtimeconfig.IsDatabaseSource(before) {
		return svc.RuntimeConfigReloadResult{}, nil
	}
	active, err := runtimeconfig.LoadActiveSnapshotCached(ctx, scope.ServiceContext)
	if err != nil {
		return svc.RuntimeConfigReloadResult{}, errors.Tag(err)
	}
	after := before
	runtimeconfig.ApplySnapshot(&after, active.Snapshot)
	applied := scope.applyConfig(before, after)
	if scope.MarkApplied != nil {
		scope.MarkApplied(active.Release.VersionNo, active.Release.Checksum)
	}
	loggerx.Infow(ctx, "运行配置 DB热加载成功",
		logx.Field("source", hotreload.Source(scope.Source)),
		logx.Field("release_id", active.Release.ID),
		logx.Field("version", active.Release.VersionNo),
		logx.Field("checksum", active.Release.Checksum),
		logx.Field("restart_required", applied.RestartRequired),
		logx.Field("restart_reason", applied.RestartReason),
	)
	return svc.RuntimeConfigReloadResult{
		ReleaseID:       active.Release.ID,
		VersionNo:       active.Release.VersionNo,
		Checksum:        active.Release.Checksum,
		RestartRequired: applied.RestartRequired,
		RestartReason:   applied.RestartReason,
	}, nil
}

// Start 在 DB 运行配置模式下启动 active release 轻量轮询。
func Start(scope WatchScope) bool {
	if scope.State == nil || !runtimeconfig.IsDatabaseSource(scope.currentConfig()) {
		return false
	}
	if !scope.State.Start(func(ctx context.Context) {
		watch(ctx, scope)
	}) {
		return false
	}
	cfg := scope.currentConfig()
	loggerx.Infow(context.Background(), "运行配置 DB热加载已启用",
		logx.Field(loggerx.FieldIntervalSeconds, runtimeconfig.PollIntervalSeconds(cfg)),
	)
	return true
}

// PollDelay 返回带轻微抖动的 DB 运行配置轮询间隔，避免多实例同时打库。
func PollDelay(cfg config.Config) time.Duration {
	interval := time.Duration(runtimeconfig.PollIntervalSeconds(cfg)) * time.Second
	jitter := interval / 10
	if jitter <= 0 {
		return interval
	}
	return interval + time.Duration(time.Now().UnixNano()%int64(jitter))
}

// currentConfig 返回当前配置快照，未注入时使用零值配置。
func (s ReloadScope) currentConfig() config.Config {
	if s.CurrentConfig == nil {
		return config.Config{}
	}
	return s.CurrentConfig()
}

// applyConfig 应用配置变更，未注入时返回新配置作为有效配置。
func (s ReloadScope) applyConfig(before, after config.Config) hotreload.AppliedConfig {
	if s.ApplyConfig == nil {
		return hotreload.AppliedConfig{Effective: after}
	}
	return s.ApplyConfig(before, after)
}

// currentConfig 返回 watcher 当前配置快照，未注入时使用零值配置。
func (s WatchScope) currentConfig() config.Config {
	if s.CurrentConfig == nil {
		return config.Config{}
	}
	return s.CurrentConfig()
}

// watch 只轮询 active state 版本号，版本不变时不读取发布快照。
func watch(ctx context.Context, scope WatchScope) {
	timer := time.NewTimer(PollDelay(scope.currentConfig()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !runtimeconfig.IsDatabaseSource(scope.currentConfig()) {
				return
			}
			if err := reloadIfChanged(ctx, scope); err != nil {
				loggerx.ErrorTextw(ctx, "运行配置 DB热加载失败", err.Error(),
					logx.Field("source", "watcher"),
				)
				if scope.NotifyFailure != nil {
					scope.NotifyFailure(ctx, "watcher", err)
				}
			}
			timer.Reset(PollDelay(scope.currentConfig()))
		}
	}
}

// reloadIfChanged 检测 active release 版本变化并触发运行配置重载。
func reloadIfChanged(ctx context.Context, scope WatchScope) error {
	if scope.Reload == nil {
		return errors.Errorf("未绑定运行配置重载器")
	}
	state, err := runtimeconfig.LoadActiveStateCached(ctx, scope.ServiceContext)
	if err != nil {
		return errors.Tag(err)
	}
	if state.ActiveReleaseID == 0 {
		return errors.Errorf("运行配置未发布 active release")
	}
	if scope.State != nil && scope.State.Applied(state.ActiveVersion, state.ActiveChecksum) {
		return nil
	}
	result, err := scope.Reload(ctx, "runtime_config_watcher")
	if err != nil {
		return errors.Tag(err)
	}
	if scope.State != nil {
		scope.State.MarkApplied(result.VersionNo, result.Checksum)
	}
	return nil
}
