package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	i18n "admin/common/i18n"
	"admin/internal/bootstrap/components"
	componentbuiltin "admin/internal/bootstrap/components/builtin"
	"admin/internal/bootstrap/configload"
	"admin/internal/bootstrap/hotreload"
	bootstrapresources "admin/internal/bootstrap/resources"
	"admin/internal/bootstrap/runmode"
	"admin/internal/bootstrap/runtimewatch"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	"admin/internal/svc"
	"admin/internal/task/queue"
	"admin/internal/task/runtime"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

const (
	// ModeAPI 启动 HTTP API（bit=1）。
	ModeAPI = runmode.API
	// ModeWorker 启动异步任务 Worker（bit=2）。
	ModeWorker = runmode.Worker
	// ModeScheduler 启动周期调度器 leader（bit=4）。
	ModeScheduler = runmode.Scheduler
	// ModeAll 同时启动 API、Worker 和调度器（bit=7）。
	ModeAll = runmode.All
)

// App 聚合服务运行所需的配置、HTTP Server 和关闭钩子，作为应用生命周期的统一入口。
type App struct {
	configValue    atomic.Value                // 当前生效的完整配置快照
	Server         *rest.Server                // HTTP 服务实例，纯后台模式下可能为空
	ServiceContext *svc.ServiceContext         // 聚合数据库、Redis、审计等全局依赖
	TaskManager    *taskqueue.Manager          // 任务队列管理器，负责 Worker、Scheduler 和投递能力
	TaskRuntime    *taskruntime.Runtime        // 任务运行时注册中心，承载插件化 handler/workflow 注册结果
	Mode           int                         // 当前运行模式位掩码
	DisplayName    string                      // 启动日志中使用的应用展示名
	startHooks     []components.LifecycleHook  // 组件注册的启动钩子，按注册顺序执行
	stopHooks      []components.LifecycleHook  // 组件注册的停止钩子，按启动逆序执行
	lifecycleMu    sync.Mutex                  // 保护已启动生命周期钩子集合
	startedHooks   map[string]struct{}         // 已成功启动的生命周期钩子名称集合
	shutdown       func(context.Context) error // tracing 等基础设施的优雅关闭钩子
	taskRedis      redis.UniversalClient       // 任务系统实际使用的 Redis 客户端
	taskRedisOwned bool                        // 当前 App 是否负责关闭 taskRedis
	hotReload      hotreload.State             // 配置热加载运行态资源
	runtimeConfig  runtimewatch.State          // DB 运行配置热加载运行态资源
}

// New 负责把依赖装配与 HTTP 服务注册串起来，并支持通过可选参数注入外部任务插件。
func New(ctx context.Context, c config.Config, mode int, options ...Option) (*App, error) {
	configload.Normalize(&c)
	resolvedOptions := resolveOptions(options)
	mode, err := runmode.Normalize(mode)
	if err != nil {
		return nil, errors.Tag(err)
	}
	// 初始化数据库、Redis、Kafka 等基础设施
	svcCtx, shutdown, err := BuildServiceContext(ctx, c)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err = applyStartupRuntimeConfig(ctx, svcCtx, &c); err != nil {
		_ = bootstrapresources.CloseServiceContextResources(svcCtx, nil, false)
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		return nil, errors.Tag(err)
	}

	// 启动组件统一由注册中心按顺序装配，避免组件散落在入口文件和 New 内部。
	state := components.NewState(c, mode, svcCtx, shutdown, resolvedOptions.RouteModules, resolvedOptions.TaskPlugins)
	registry := components.NewRegistry(componentbuiltin.ResolveStartup(resolvedOptions.UseDefaultComponents, resolvedOptions.Components)...)
	if err = registry.Register(ctx, state); err != nil {
		cleanupComponentState(context.Background(), state)
		return nil, errors.Tag(err)
	}
	runtimeSnapshot := components.Snapshot(state)

	app := &App{
		Server:         runtimeSnapshot.Server,
		ServiceContext: runtimeSnapshot.ServiceContext,
		TaskManager:    runtimeSnapshot.TaskManager,
		TaskRuntime:    runtimeSnapshot.TaskRuntime,
		Mode:           mode,
		DisplayName:    resolvedOptions.DisplayName,
		startHooks:     runtimeSnapshot.StartHooks,
		stopHooks:      runtimeSnapshot.StopHooks,
		shutdown:       shutdown,
		taskRedis:      runtimeSnapshot.TaskRedis,
		taskRedisOwned: runtimeSnapshot.TaskRedisOwned,
	}
	svcCtx.ConfigReload = app
	app.UpdateConfig(c)
	return app, nil
}

// CurrentConfig 返回当前生效的完整配置快照。
func (a *App) CurrentConfig() config.Config {
	if a == nil {
		return config.Config{}
	}
	if cfg, ok := a.configValue.Load().(config.Config); ok {
		return cfg
	}
	return config.Config{}
}

// UpdateConfig 原子替换应用、服务上下文和任务管理器共享的配置快照。
func (a *App) UpdateConfig(c config.Config) {
	if a == nil {
		return
	}
	a.configValue.Store(c)
	if a.ServiceContext != nil {
		a.ServiceContext.UpdateConfig(c)
		a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
			status.ConfigSummary = hotreload.Summary(c)
			return status
		})
	}
	if a.TaskManager != nil {
		a.TaskManager.UpdateConfig(componentbuiltin.TaskConfigWithAppID(c))
	}
}

// BindConfigFile 绑定当前应用实例使用的配置文件路径，供热加载协程监听。
func (a *App) BindConfigFile(configFile string) {
	if a == nil {
		return
	}
	configFile = a.hotReload.SetConfigFile(configFile)
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.ConfigFile = configFile
		return status
	})
}

// ReloadConfig 手动触发一次配置重载，供管理接口复用。
func (a *App) ReloadConfig(ctx context.Context, source string) error {
	if a == nil {
		return errors.Errorf("应用实例为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	configFile := a.boundConfigFile()
	if configFile == "" {
		err := errors.Errorf("未绑定配置文件路径")
		a.markHotReloadFailure(i18n.MsgKeyHotReloadManualFailed, "手动触发配置重载失败", err, "", hotreload.Source(source), "not_bound", "")
		return err
	}
	_, err := a.reloadConfigFile(ctx, hotreload.Source(source), configFile)
	return errors.Tag(err)
}

// Start 按运行模式启动 API、Worker 和调度器。
// 返回 error 由入口统一收口，避免在启动链路中直接 panic。
func (a *App) Start() error {
	// 启动配置热重载协程
	a.startConfigHotReload()
	a.startRuntimeConfigWatcher()

	// 统一启动组件注册的后台生命周期钩子，避免在入口层硬编码具体组件类型。
	if err := a.runStartHooks(context.Background()); err != nil {
		a.stopConfigHotReload()
		a.stopRuntimeConfigWatcher()
		return errors.Tag(err)
	}

	// 启动 HTTP 服务
	if a.Server != nil {
		cfg := a.CurrentConfig()
		loggerx.Infow(context.Background(), "应用 服务已启动",
			logx.Field("service", a.displayName()),
			logx.Field("host", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)),
			logx.Field("mode", runmode.Format(a.Mode)),
		)
		a.Server.Start()
		return nil
	}

	// 启动后台运行时
	loggerx.Infow(context.Background(), "应用 后台已启动",
		logx.Field("service", a.displayName()),
		logx.Field("mode", runmode.Format(a.Mode)),
	)
	waitForSignal()
	return nil
}

// Stop 按相反顺序释放资源，确保 tracing exporter 等后台组件有机会优雅退出。
func (a *App) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 先停止 HTTP 服务入口，避免关闭过程中继续接收新请求。
	if a.Server != nil {
		a.Server.Stop()
	}
	// 再停止配置热加载协程，避免资源释放过程中仍有后台线程刷新配置快照。
	a.stopConfigHotReload()
	a.stopRuntimeConfigWatcher()
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	// 组件生命周期钩子按启动逆序停止，尽量保持依赖关系的释放顺序正确。
	recordErr(a.runStopHooks(ctx))
	recordErr(bootstrapresources.CloseServiceContextResources(a.ServiceContext, a.taskRedis, a.taskRedisOwned))
	// 最后执行基础设施总关闭钩子，保证 tracing/exporter 等底层资源有机会优雅退出。
	if a.shutdown != nil {
		recordErr(a.shutdown(ctx))
	}
	return errors.Tag(firstErr)
}

// displayName 返回启动日志使用的展示名，缺省时回退默认值。
func (a *App) displayName() string {
	if a == nil || strings.TrimSpace(a.DisplayName) == "" {
		return defaultDisplayName
	}
	return strings.TrimSpace(a.DisplayName)
}
