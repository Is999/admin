package taskruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"admin_cron/internal/config"
	"admin_cron/internal/svc"
	"admin_cron/internal/taskqueue"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

// CacheRefreshHandler 定义单个缓存刷新目标的执行器。
// 业务模块只需要注册 target -> handler 的映射，不再改 taskruntime 的 switch 分支。
type CacheRefreshHandler func(context.Context, *svc.ServiceContext) error

// Plugin 描述任务系统的一个轻量级插件。
// 插件可以注册任务处理器、工作流定义以及缓存刷新目标。
type Plugin interface {
	Name() string
	Setup(*Runtime) error
}

// PluginFunc 允许通过函数快速声明一个插件，减少样板代码。
type PluginFunc struct {
	name  string
	setup func(*Runtime) error
}

// NewPluginFunc 创建一个函数式插件。
func NewPluginFunc(name string, setup func(*Runtime) error) Plugin {
	return PluginFunc{name: strings.TrimSpace(name), setup: setup}
}

// NewCacheRefreshPlugin 为单一缓存刷新目标创建一个轻量插件。
func NewCacheRefreshPlugin(name, target string, handler CacheRefreshHandler) Plugin {
	return NewPluginFunc(name, func(runtime *Runtime) error {
		return runtime.RegisterCacheRefreshTarget(target, handler)
	})
}

// NewPeriodicWorkflowPlugin 为单个周期工作流创建一个轻量插件。
func NewPeriodicWorkflowPlugin(name string, cfg config.TaskPeriodicConfig) Plugin {
	return NewPluginFunc(name, func(runtime *Runtime) error {
		return runtime.RegisterPeriodicTask(cfg)
	})
}

// Name 返回插件名称。
func (p PluginFunc) Name() string {
	return p.name
}

// Setup 执行插件初始化逻辑。
func (p PluginFunc) Setup(runtime *Runtime) error {
	if p.setup == nil {
		return nil
	}
	return p.setup(runtime)
}

// Runtime 是任务系统的注册中心，统一承接 handler / workflow / cache target 的装配。
type Runtime struct {
	svc                    *svc.ServiceContext            // 服务上下文，供任务处理器访问业务依赖
	manager                *taskqueue.Manager             // 任务队列管理器，承接 Asynq Worker / Scheduler 注册
	mu                     sync.RWMutex                   // 注册表读写锁，保护插件化扩展点并发注册
	registeredPlugins      map[string]struct{}            // 已注册插件名称集合，防止插件重复初始化
	registeredHandlers     map[string]struct{}            // 已注册任务处理器 pattern 集合
	registeredAggregators  map[string]struct{}            // 已注册任务聚合器 group 集合
	registeredMiddlewares  map[string]struct{}            // 已注册中间件名称集合
	registeredFailureHooks map[string]struct{}            // 已注册终态失败清理钩子集合
	registeredWorkflows    map[string]struct{}            // 已注册工作流名称集合
	registeredPeriodic     map[string]struct{}            // 已注册周期任务唯一键集合
	periodicTasks          []config.TaskPeriodicConfig    // 插件补充的周期任务配置列表
	cacheRefreshHandlers   map[string]CacheRefreshHandler // 缓存刷新目标到处理器的映射
	cacheRefreshTargetSet  map[string]struct{}            // 缓存刷新目标去重集合
}

// NewRuntime 创建任务运行时注册中心。
func NewRuntime(svcCtx *svc.ServiceContext, manager *taskqueue.Manager) *Runtime {
	return &Runtime{
		svc:                    svcCtx,
		manager:                manager,
		registeredPlugins:      make(map[string]struct{}),
		registeredHandlers:     make(map[string]struct{}),
		registeredAggregators:  make(map[string]struct{}),
		registeredMiddlewares:  make(map[string]struct{}),
		registeredFailureHooks: make(map[string]struct{}),
		registeredWorkflows:    make(map[string]struct{}),
		registeredPeriodic:     make(map[string]struct{}),
		cacheRefreshHandlers:   make(map[string]CacheRefreshHandler),
		cacheRefreshTargetSet:  make(map[string]struct{}),
	}
}

// ServiceContext 返回运行时绑定的服务上下文。
func (r *Runtime) ServiceContext() *svc.ServiceContext {
	return r.svc
}

// Manager 返回运行时绑定的任务管理器。
func (r *Runtime) Manager() *taskqueue.Manager {
	return r.manager
}

// RegisterHandler 注册任务处理器。
func (r *Runtime) RegisterHandler(pattern string, handler asynq.Handler) error {
	if r == nil {
		return nil
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.Errorf("任务处理器 pattern 不能为空")
	}
	if handler == nil {
		return errors.Errorf("任务处理器不能为空: %s", pattern)
	}

	// 先占位注册表，避免并发注册同一 pattern 时穿透到 Asynq ServeMux 触发 panic。
	r.mu.Lock()
	if _, exists := r.registeredHandlers[pattern]; exists {
		r.mu.Unlock()
		return errors.Errorf("任务处理器已注册: %s", pattern)
	}
	r.registeredHandlers[pattern] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		if err := r.manager.RegisterHandler(pattern, handler); err != nil {
			r.mu.Lock()
			delete(r.registeredHandlers, pattern)
			r.mu.Unlock()
			return errors.Tag(err)
		}
	}
	return nil
}

// RegisterGroupAggregator 注册任务聚合器，把同一 group 的任务合并成批处理任务。
func (r *Runtime) RegisterGroupAggregator(group string, aggregator taskqueue.GroupAggregator) error {
	if r == nil {
		return nil
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.Errorf("任务分组不能为空")
	}
	if aggregator == nil {
		return errors.Errorf("任务分组聚合器不能为空: %s", group)
	}

	// 先写运行时注册表，确保同一 group 的并发注册只有一个进入 Manager。
	r.mu.Lock()
	if _, exists := r.registeredAggregators[group]; exists {
		r.mu.Unlock()
		return errors.Errorf("任务分组聚合器已注册: %s", group)
	}
	r.registeredAggregators[group] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		if err := r.manager.RegisterGroupAggregator(group, aggregator); err != nil {
			r.mu.Lock()
			delete(r.registeredAggregators, group)
			r.mu.Unlock()
			return errors.Tag(err)
		}
	}
	return nil
}

// RegisterMiddleware 注册任务处理中间件，供业务插件在启动期扩展日志、限流或审计能力。
func (r *Runtime) RegisterMiddleware(name string, middleware asynq.MiddlewareFunc) error {
	if r == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.Errorf("任务中间件名称不能为空")
	}
	if middleware == nil {
		return errors.Errorf("任务中间件不能为空: %s", name)
	}

	// 中间件名称作为插件化扩展点的幂等键，避免链路能力被重复挂载。
	r.mu.Lock()
	if _, exists := r.registeredMiddlewares[name]; exists {
		r.mu.Unlock()
		return errors.Errorf("任务中间件已注册: %s", name)
	}
	r.registeredMiddlewares[name] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		r.manager.Use(middleware)
	}
	return nil
}

// RegisterFinalFailureHook 注册任务终态失败后的业务清理钩子。
// 业务插件通过该扩展点释放自身持有的外部租约，避免把具体业务包反向耦合进 taskqueue。
func (r *Runtime) RegisterFinalFailureHook(name string, hook taskqueue.TaskFinalFailureHook) error {
	if r == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.Errorf("任务终态失败清理钩子名称不能为空")
	}
	if hook == nil {
		return errors.Errorf("任务终态失败清理钩子不能为空: %s", name)
	}

	// 清理钩子同样按名称幂等注册，避免插件重复初始化导致一个终态失败触发多次业务释放。
	r.mu.Lock()
	if _, exists := r.registeredFailureHooks[name]; exists {
		r.mu.Unlock()
		return errors.Errorf("任务终态失败清理钩子已注册: %s", name)
	}
	r.registeredFailureHooks[name] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		if err := r.manager.RegisterFinalFailureHook(hook); err != nil {
			r.mu.Lock()
			delete(r.registeredFailureHooks, name)
			r.mu.Unlock()
			return errors.Tag(err)
		}
	}
	return nil
}

// RegisterWorkflow 注册工作流定义。
func (r *Runtime) RegisterWorkflow(def *taskqueue.WorkflowDefinition) error {
	if r == nil {
		return nil
	}
	if def == nil {
		return errors.Errorf("工作流定义不能为空")
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return errors.Errorf("工作流名称不能为空")
	}

	// 工作流定义先在运行时占位，Manager 校验失败时再回滚，保证并发注册语义明确。
	r.mu.Lock()
	if _, exists := r.registeredWorkflows[name]; exists {
		r.mu.Unlock()
		return errors.Errorf("工作流已注册: %s", name)
	}
	r.registeredWorkflows[name] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		if err := r.manager.RegisterWorkflow(def); err != nil {
			r.mu.Lock()
			delete(r.registeredWorkflows, name)
			r.mu.Unlock()
			return errors.Tag(err)
		}
	}
	return nil
}

// RegisterCacheRefreshTarget 注册缓存刷新目标处理器。
func (r *Runtime) RegisterCacheRefreshTarget(target string, handler CacheRefreshHandler) error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(target) == "" {
		return errors.Errorf("缓存刷新目标不能为空")
	}
	if handler == nil {
		return errors.Errorf("缓存刷新目标处理器不能为空: %s", target)
	}
	target = strings.TrimSpace(target)

	// 缓存刷新目标通常由多个业务插件补充，统一加锁保证注册表并发安全。
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cacheRefreshTargetSet[target]; exists {
		return errors.Errorf("缓存刷新目标已注册: %s", target)
	}
	r.cacheRefreshHandlers[target] = handler
	r.cacheRefreshTargetSet[target] = struct{}{}
	return nil
}

// CacheRefreshHandler 返回指定缓存目标的处理器。
func (r *Runtime) CacheRefreshHandler(target string) CacheRefreshHandler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cacheRefreshHandlers[strings.TrimSpace(target)]
}

// RegisterPeriodicTask 注册周期任务配置，支持在保留配置文件能力的同时由插件补充调度入口。
func (r *Runtime) RegisterPeriodicTask(cfg config.TaskPeriodicConfig) error {
	if r == nil {
		return nil
	}
	key, err := periodicTaskKey(cfg)
	if err != nil {
		return errors.Tag(err)
	}

	// 周期任务先占位，避免并发插件重复挂载同一 cron/workflow。
	r.mu.Lock()
	if _, exists := r.registeredPeriodic[key]; exists {
		r.mu.Unlock()
		return errors.Errorf("周期任务已注册: %s", key)
	}
	r.registeredPeriodic[key] = struct{}{}
	r.mu.Unlock()

	if r.manager != nil {
		if err := r.manager.RegisterPeriodicTask(cfg); err != nil {
			r.mu.Lock()
			delete(r.registeredPeriodic, key)
			r.mu.Unlock()
			return errors.Tag(err)
		}
	}

	r.mu.Lock()
	r.periodicTasks = append(r.periodicTasks, cfg)
	r.mu.Unlock()
	return nil
}

// PeriodicTasks 返回已通过插件注册的周期任务列表副本。
func (r *Runtime) PeriodicTasks() []config.TaskPeriodicConfig {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]config.TaskPeriodicConfig, len(r.periodicTasks))
	copy(items, r.periodicTasks)
	return items
}

// DefaultCacheRefreshTargets 返回已注册缓存刷新目标的稳定有序列表。
func (r *Runtime) DefaultCacheRefreshTargets() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	targets := make([]string, 0, len(r.cacheRefreshTargetSet))
	for target := range r.cacheRefreshTargetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

// RegisterPlugins 批量注册插件，按传入顺序执行，便于控制基础设施先后关系。
func (r *Runtime) RegisterPlugins(plugins ...Plugin) error {
	if r == nil {
		return nil
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		name := strings.TrimSpace(plugin.Name())
		if name == "" {
			return errors.Errorf("任务插件名称不能为空")
		}

		// 插件名称先占位，避免并发初始化同一个插件导致 handler / workflow 重复注册。
		r.mu.Lock()
		if _, exists := r.registeredPlugins[name]; exists {
			r.mu.Unlock()
			return errors.Errorf("任务插件已注册: %s", name)
		}
		r.registeredPlugins[name] = struct{}{}
		r.mu.Unlock()

		if err := plugin.Setup(r); err != nil {
			r.mu.Lock()
			delete(r.registeredPlugins, name)
			r.mu.Unlock()
			return errors.Wrapf(err, "初始化任务插件失败 %s", name)
		}
	}
	return nil
}

// periodicTaskKey 生成周期任务的唯一键，优先使用显式名称，缺省时退化为配置摘要。
func periodicTaskKey(cfg config.TaskPeriodicConfig) (string, error) {
	cron := strings.TrimSpace(cfg.Cron)
	if cfg.EverySeconds < 0 {
		return "", errors.Errorf("周期任务 every_seconds 不能小于 0")
	}
	if cron != "" && cfg.EverySeconds > 0 {
		return "", errors.Errorf("周期任务 cron 和 every_seconds 不能同时配置")
	}
	if cfg.EverySeconds > 0 {
		cron = fmt.Sprintf("@every %ds", cfg.EverySeconds)
	}
	workflow := strings.TrimSpace(cfg.Workflow)
	if cron == "" {
		return "", errors.Errorf("周期任务 cron 或 every_seconds 必须配置一个")
	}
	if workflow == "" {
		return "", errors.Errorf("周期任务 workflow 不能为空")
	}
	if name := strings.TrimSpace(cfg.Name); name != "" {
		return "name:" + name, nil
	}
	return fmt.Sprintf("cron:%s|workflow:%s|queue:%s", cron, workflow, strings.TrimSpace(cfg.Queue)), nil
}
