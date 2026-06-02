package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"admin/internal/audit"
	"admin/internal/config"
	"admin/internal/infra/kafkax"
	"admin/internal/infra/loggerx"
	mysqlx "admin/internal/infra/mysql"
	"admin/internal/infra/redisx"
	"admin/internal/infra/tracing"
	"admin/internal/svc"
	"admin/internal/task/queue"
	"admin/internal/task/runtime"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"gorm.io/gorm"
)

const (
	// ModeAPI 启动 HTTP API（bit=1）。
	ModeAPI = 1 << iota
	// ModeWorker 启动异步任务 Worker（bit=2）。
	ModeWorker
	// ModeScheduler 启动周期调度器 leader（bit=4）。
	ModeScheduler
	// ModeAll 同时启动 API、Worker 和调度器（bit=7）。
	ModeAll = ModeAPI | ModeWorker | ModeScheduler
)

// configHotReloadState 保存配置热加载运行态资源，零值可用。
type configHotReloadState struct {
	configFile string             // 当前应用对应的配置文件路径
	cancel     context.CancelFunc // 配置热加载后台协程取消函数
	wg         sync.WaitGroup     // 等待配置热加载后台协程退出
	stateMu    sync.RWMutex       // 保护 watcher 生命周期与配置文件绑定状态
	statusMu   sync.Mutex         // 保护热加载状态快照更新
	logMu      sync.Mutex         // 保护重复失败日志限频状态
	execMu     sync.Mutex         // 串行化实际配置重载，避免 watcher 与手动触发并发覆盖
	lastError  string             // 最近一次失败日志签名，用于重复失败限频
	lastLogAt  time.Time          // 最近一次失败日志实际输出时间
}

// App 聚合服务运行所需的配置、HTTP Server 和关闭钩子，作为应用生命周期的统一入口。
type App struct {
	configValue    atomic.Value                // 当前生效的完整配置快照
	Server         *rest.Server                // HTTP 服务实例，纯后台模式下可能为空
	ServiceContext *svc.ServiceContext         // 聚合数据库、Redis、审计等全局依赖
	TaskManager    *taskqueue.Manager          // 任务队列管理器，负责 Worker、Scheduler 和投递能力
	TaskRuntime    *taskruntime.Runtime        // 任务运行时注册中心，承载插件化 handler/workflow 注册结果
	Mode           int                         // 当前运行模式位掩码
	DisplayName    string                      // 启动日志中使用的应用展示名
	startHooks     []lifecycleHook             // 组件注册的启动钩子，按注册顺序执行
	stopHooks      []lifecycleHook             // 组件注册的停止钩子，按启动逆序执行
	lifecycleMu    sync.Mutex                  // 保护已启动生命周期钩子集合
	startedHooks   map[string]struct{}         // 已成功启动的生命周期钩子名称集合
	shutdown       func(context.Context) error // tracing 等基础设施的优雅关闭钩子
	taskRedis      redis.UniversalClient       // 任务系统实际使用的 Redis 客户端
	taskRedisOwned bool                        // 当前 App 是否负责关闭 taskRedis
	hotReload      configHotReloadState        // 配置热加载运行态资源
	runtimeConfig  runtimeConfigWatchState     // DB 运行配置热加载运行态资源
}

// taskConfigWithAppID 把顶层 `app_id` 注入任务系统配置快照。
// 这样任务队列、工作流 Redis key、调度锁和幂等唯一键都能自动按站点隔离。
func taskConfigWithAppID(c config.Config) config.TaskQueueConfig {
	taskCfg := c.Task
	taskCfg.AppID = strings.TrimSpace(c.AppID)
	return taskCfg
}

// BuildServiceContext 统一完成基础设施初始化，避免 main 和 debug 入口各自拼装依赖导致行为漂移。
func BuildServiceContext(ctx context.Context, c config.Config) (*svc.ServiceContext, func(context.Context) error, error) {
	loggerx.Setup(c)
	if err := configureSnowflakeWorkerID(c.Snowflake); err != nil {
		return nil, nil, errors.Wrap(err, "配置雪花 ID worker 失败")
	}

	shutdown, err := tracing.Setup(ctx, c.Observability)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	resources := buildResources{Dependencies: svc.Dependencies{}, Shutdown: shutdown}

	siteDBs, err := buildSiteDatabases(ctx, c)
	resources.SiteDBs = siteDBs
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}

	rdb, err := redisx.New(ctx, c.Redis, c.Observability)
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}
	resources.Rds = rdb

	kafkaProducer, err := kafkax.NewProducer(c.Kafka)
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}
	resources.Kafka = kafkaProducer

	svcCtx := svc.NewServiceContext(c, resources.Dependencies)
	// 审计日志使用主库写连接。
	resources.Audit = audit.NewRecorder(svcCtx.WriteDB(svc.DatabaseMain), c.Observability.LogBodyMaxBytes)
	svcCtx.Audit = resources.Audit
	publishRuntimeConfig(c)
	return svcCtx, shutdown, nil
}

// buildSiteDatabases 初始化主库和可选命名扩展库连接。
// 默认只打开顶层 mysql；site_mysql.<name> 仅用于后续拆库扩展。
func buildSiteDatabases(ctx context.Context, c config.Config) (svc.SiteDatabases, error) {
	if !hasMySQLDataSource(c.MySQL) {
		return svc.SiteDatabases{}, errors.Errorf("缺少 mysql.write_data_source 配置")
	}
	mainDB, err := openSiteDatabase(ctx, "mysql", c.MySQL, c.Observability)
	if err != nil {
		return svc.SiteDatabases{}, errors.Tag(err)
	}
	dbs := svc.SiteDatabases{
		MainDB:   mainDB,
		NamedDBs: make(map[svc.DbName]*gorm.DB),
	}
	for name, dbCfg := range c.SiteMySQL {
		if !hasMySQLDataSource(dbCfg) {
			continue
		}
		dbName := svc.DbName(strings.TrimSpace(name))
		db, err := openSiteDatabase(ctx, "site_mysql."+string(dbName), dbCfg, c.Observability)
		if err != nil {
			_ = closeSiteDatabases(dbs)
			return svc.SiteDatabases{}, errors.Tag(err)
		}
		dbs.NamedDBs[dbName] = db
	}
	return dbs, nil
}

// openSiteDatabase 初始化单个 MySQL 连接；缺少写库 DSN 时直接返回启动错误。
func openSiteDatabase(ctx context.Context, name string, cfg config.MySQLConfig, obs config.ObservabilityConfig) (*gorm.DB, error) {
	if strings.TrimSpace(cfg.WriteDataSource) == "" {
		return nil, errors.Errorf("缺少 %s.write_data_source 配置", name)
	}
	db, err := mysqlx.New(ctx, cfg, obs)
	if err != nil {
		return nil, errors.Wrapf(err, "打开 MySQL[%s] 失败", name)
	}
	return db, nil
}

// hasMySQLDataSource 判断 MySQL 配置是否提供写库 DSN。
func hasMySQLDataSource(cfg config.MySQLConfig) bool {
	return strings.TrimSpace(cfg.WriteDataSource) != ""
}

// New 负责把依赖装配与 HTTP 服务注册串起来，并支持通过可选参数注入外部任务插件。
func New(ctx context.Context, c config.Config, mode int, options ...Option) (*App, error) {
	normalizeBootstrapConfig(&c)
	resolvedOptions := resolveOptions(options)
	mode, err := normalizeMode(mode)
	if err != nil {
		return nil, errors.Tag(err)
	}
	// 初始化数据库、Redis、Kafka 等基础设施
	svcCtx, shutdown, err := BuildServiceContext(ctx, c)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err = applyStartupRuntimeConfig(ctx, svcCtx, &c); err != nil {
		_ = closeServiceContextResources(svcCtx, nil, false)
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		return nil, errors.Tag(err)
	}

	// 启动组件统一由注册中心按顺序装配，避免组件散落在入口文件和 New 内部。
	state := newComponentState(c, mode, resolvedOptions, svcCtx, shutdown)
	registry := NewComponentRegistry(resolveStartupComponents(resolvedOptions)...)
	if err = registry.Register(ctx, state); err != nil {
		cleanupComponentState(context.Background(), state)
		return nil, errors.Tag(err)
	}
	runtimeSnapshot := snapshotComponentRuntime(state)

	app := &App{
		Server:         runtimeSnapshot.Server,
		ServiceContext: runtimeSnapshot.ServiceContext,
		TaskManager:    runtimeSnapshot.TaskManager,
		TaskRuntime:    runtimeSnapshot.TaskRuntime,
		Mode:           mode,
		DisplayName:    resolvedOptions.DisplayName,
		startHooks:     runtimeSnapshot.startHooks,
		stopHooks:      runtimeSnapshot.stopHooks,
		shutdown:       shutdown,
		taskRedis:      runtimeSnapshot.TaskRedis,
		taskRedisOwned: runtimeSnapshot.TaskRedisOwned,
	}
	svcCtx.ConfigReload = app
	app.UpdateConfig(c)
	return app, nil
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
			logx.Field("mode", formatMode(a.Mode)),
		)
		a.Server.Start()
		return nil
	}

	// 启动后台运行时
	loggerx.Infow(context.Background(), "应用 后台已启动",
		logx.Field("service", a.displayName()),
		logx.Field("mode", formatMode(a.Mode)),
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
	recordErr(closeServiceContextResources(a.ServiceContext, a.taskRedis, a.taskRedisOwned))
	// 最后执行基础设施总关闭钩子，保证 tracing/exporter 等底层资源有机会优雅退出。
	if a.shutdown != nil {
		recordErr(a.shutdown(ctx))
	}
	return errors.Tag(firstErr)
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
			status.ConfigSummary = buildHotReloadConfigSummary(c)
			return status
		})
	}
	if a.TaskManager != nil {
		a.TaskManager.UpdateConfig(taskConfigWithAppID(c))
	}
}

// BindConfigFile 绑定当前应用实例使用的配置文件路径，供热加载协程监听。
func (a *App) BindConfigFile(configFile string) {
	if a == nil {
		return
	}
	configFile = strings.TrimSpace(configFile)
	a.hotReload.stateMu.Lock()
	a.hotReload.configFile = configFile
	a.hotReload.stateMu.Unlock()
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
		a.markHotReloadFailure("手动触发配置重载失败", err, "", normalizeHotReloadSource(source), "not_bound", "")
		return err
	}
	_, err := a.reloadConfigFile(ctx, normalizeHotReloadSource(source), configFile)
	return errors.Tag(err)
}

// runStartHooks 按注册顺序启动所有组件生命周期钩子。
func (a *App) runStartHooks(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, hook := range a.startHooks {
		if hook.run == nil {
			continue
		}
		if err := hook.run(ctx); err != nil {
			// 已启动的前置组件需要立即回滚，避免 Start 返回错误后后台协程继续运行。
			stopErr := a.runStopHooks(ctx)
			if stopErr != nil {
				return errors.Wrapf(err, "启动生命周期钩子失败: %s; 回滚已启动组件失败: %v", hook.name, stopErr)
			}
			return errors.Wrapf(err, "启动生命周期钩子失败: %s", hook.name)
		}
		a.markLifecycleHookStarted(hook.name)
	}
	return nil
}

// runStopHooks 按启动逆序停止所有组件生命周期钩子，尽量保证依赖关系正确回收。
func (a *App) runStopHooks(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	startedHooks := a.takeStartedLifecycleHooks()
	if len(startedHooks) == 0 {
		return nil
	}
	var firstErr error
	for idx := len(a.stopHooks) - 1; idx >= 0; idx-- {
		hook := a.stopHooks[idx]
		if hook.run == nil {
			continue
		}
		if _, ok := startedHooks[hook.name]; !ok {
			continue
		}
		if err := hook.run(ctx); err != nil && firstErr == nil {
			firstErr = errors.Wrapf(err, "停止生命周期钩子失败: %s", hook.name)
		}
	}
	return errors.Tag(firstErr)
}

// markLifecycleHookStarted 记录一个已成功启动的生命周期钩子。
func (a *App) markLifecycleHookStarted(name string) {
	if a == nil || strings.TrimSpace(name) == "" {
		return
	}
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.startedHooks == nil {
		a.startedHooks = make(map[string]struct{})
	}
	a.startedHooks[name] = struct{}{}
}

// takeStartedLifecycleHooks 取出并清空已启动钩子集合，保证 Stop 幂等。
func (a *App) takeStartedLifecycleHooks() map[string]struct{} {
	if a == nil {
		return nil
	}
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if len(a.startedHooks) == 0 {
		return nil
	}
	started := make(map[string]struct{}, len(a.startedHooks))
	for name := range a.startedHooks {
		started[name] = struct{}{}
	}
	a.startedHooks = nil
	return started
}

// boundConfigFile 返回当前绑定的配置文件路径快照。
func (a *App) boundConfigFile() string {
	if a == nil {
		return ""
	}
	a.hotReload.stateMu.RLock()
	defer a.hotReload.stateMu.RUnlock()
	return a.hotReload.configFile
}

// isConfigHotReloadRunning 返回当前是否已有热加载 watcher 在运行。
func (a *App) isConfigHotReloadRunning() bool {
	if a == nil {
		return false
	}
	a.hotReload.stateMu.RLock()
	defer a.hotReload.stateMu.RUnlock()
	return a.hotReload.cancel != nil
}

// normalizeHotReloadSource 统一热加载触发来源，避免状态与日志出现空值。
func normalizeHotReloadSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "unknown"
	}
	return source
}

// normalizeHotReloadFailureCategory 统一失败分类，避免状态与日志出现空值。
func normalizeHotReloadFailureCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "unknown"
	}
	return category
}

// normalizeMode 规范化位掩码模式，传入 0 时默认回退为全量模式。
// 生产启动只允许 1..7 的明确组合，避免非法值被按位裁剪后意外启动更多组件。
func normalizeMode(mode int) (int, error) {
	if mode == 0 {
		return ModeAll, nil
	}
	if mode < 0 || mode&^ModeAll != 0 {
		return 0, errors.Errorf("不支持的启动模式: %d，仅支持 1(API)、2(Worker)、4(Scheduler) 及 3/5/6/7 组合", mode)
	}
	return mode, nil
}

// hasMode 判断当前运行模式是否包含指定能力位。
func hasMode(mode, flag int) bool {
	return mode&flag == flag
}

// shouldStartWorkerLifecycle 判断当前模式是否允许启动 Worker 类后台消费者。
func shouldStartWorkerLifecycle(mode int) bool {
	return hasMode(mode, ModeWorker)
}

// shouldStartTaskLifecycle 判断当前模式是否允许启动任务系统后台生命周期。
func shouldStartTaskLifecycle(mode int) bool {
	return hasMode(mode, ModeWorker) || hasMode(mode, ModeScheduler)
}

// hasTaskRedisConfig 判断是否为任务系统显式配置了独立 Redis。
func hasTaskRedisConfig(cfg config.RedisConfig) bool {
	for _, addr := range cfg.Addrs {
		if strings.TrimSpace(addr) != "" {
			return true
		}
	}
	return false
}

// validateTaskRedisConfig 校验任务系统独立 Redis 配置是否完整。
// task.redis 只要出现任一非地址字段，就必须显式配置 addrs，避免运维误以为已切独立 Redis，实际仍复用主 Redis。
func validateTaskRedisConfig(cfg config.RedisConfig) error {
	if !taskRedisConfigTouched(cfg) {
		return nil
	}
	if !hasTaskRedisConfig(cfg) {
		return errors.Errorf("task.redis 已配置非地址字段时必须同时配置 addrs")
	}
	return nil
}

// taskRedisConfigTouched 判断 task.redis 是否被显式配置过。
// map 即使只包含空值也视为已触碰配置，目的是尽早暴露无效配置而不是静默忽略。
func taskRedisConfigTouched(cfg config.RedisConfig) bool {
	return strings.TrimSpace(cfg.Type) != "" ||
		len(cfg.Addrs) > 0 ||
		len(cfg.AddrMap) > 0 ||
		strings.TrimSpace(cfg.Password) != "" ||
		cfg.DB != 0 ||
		cfg.PoolSize != 0 ||
		cfg.TLS ||
		cfg.TLSInsecureSkipVerify
}

// buildResources 聚合 BuildServiceContext 启动过程中已成功初始化、但尚未交给 App 托管的资源。
// 失败回滚时只扩展该结构体字段，避免 closeBuildResources 参数列表随业务依赖增长而失控。
type buildResources struct {
	svc.Dependencies                             // ServiceContext 可直接复用的依赖集合，包含数据库、Redis、Kafka 与审计资源
	Shutdown         func(context.Context) error // tracing 等基础设施关闭钩子，最后释放
}

// closeBuildResources 回收 BuildServiceContext 已经创建但尚未交给 App 托管的资源。
// 启动阶段任一后置依赖失败时都必须释放前置连接池，避免健康检查或容器重启循环中持续泄漏连接。
func closeBuildResources(ctx context.Context, resources buildResources) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	if resources.Kafka != nil {
		recordErr(resources.Kafka.Close())
	}
	if resources.Rds != nil {
		recordErr(resources.Rds.Close())
	}
	recordErr(closeSiteDatabases(resources.SiteDBs))
	if resources.Shutdown != nil {
		recordErr(resources.Shutdown(ctx))
	}
	return errors.Tag(firstErr)
}

// closeServiceContextResources 释放 ServiceContext 托管的外部资源。
// 任务 Redis 可能复用业务主 Redis，关闭时通过 owned 标记避免重复关闭共享连接。
func closeServiceContextResources(svcCtx *svc.ServiceContext, taskRedis redis.UniversalClient, taskRedisOwned bool) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	if taskRedisOwned && taskRedis != nil {
		recordErr(taskRedis.Close())
	}
	if svcCtx == nil {
		return errors.Tag(firstErr)
	}
	if svcCtx.Rds != nil && (!taskRedisOwned || svcCtx.Rds != taskRedis) {
		recordErr(svcCtx.Rds.Close())
	}
	if svcCtx.Kafka != nil {
		recordErr(svcCtx.Kafka.Close())
	}
	recordErr(closeSiteDatabases(svcCtx.SiteDBs))
	return errors.Tag(firstErr)
}

// closeSiteDatabases 关闭站点数据库 GORM 底层连接池。
// 测试或特殊装配可能复用同一个 *gorm.DB 指针，因此这里按指针去重，避免重复 Close 产生噪声错误。
func closeSiteDatabases(siteDBs svc.SiteDatabases) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	seen := make(map[*gorm.DB]struct{}, 4)
	closeOne := func(name string, db *gorm.DB) {
		if db == nil {
			return
		}
		if _, ok := seen[db]; ok {
			return
		}
		seen[db] = struct{}{}
		sqlDB, err := db.DB()
		if err != nil {
			recordErr(errors.Wrapf(err, "获取 MySQL[%s]底层连接池失败", name))
			return
		}
		if sqlDB == nil {
			return
		}
		if err = sqlDB.Close(); err != nil {
			recordErr(errors.Wrapf(err, "关闭 MySQL[%s]连接池失败", name))
		}
	}
	closeOne("mysql", siteDBs.MainDB)
	for name, db := range siteDBs.NamedDBs {
		closeOne("site_mysql."+string(name), db)
	}
	return errors.Tag(firstErr)
}

// formatMode 把位掩码模式转换成便于日志阅读的文字描述。
func formatMode(mode int) string {
	parts := make([]string, 0, 3)
	if hasMode(mode, ModeAPI) {
		parts = append(parts, "api")
	}
	if hasMode(mode, ModeWorker) {
		parts = append(parts, "worker")
	}
	if hasMode(mode, ModeScheduler) {
		parts = append(parts, "scheduler")
	}
	if len(parts) == 0 {
		return "none"
	}
	return fmt.Sprintf("%d(%s)", mode, strings.Join(parts, "+"))
}

// displayName 返回启动日志使用的展示名，缺省时回退默认值。
func (a *App) displayName() string {
	if a == nil || strings.TrimSpace(a.DisplayName) == "" {
		return defaultDisplayName
	}
	return strings.TrimSpace(a.DisplayName)
}

// waitForSignal 在纯后台模式下阻塞主 goroutine，直到收到退出信号。
func waitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	<-sigCh
}
