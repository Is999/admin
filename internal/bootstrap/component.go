package bootstrap

import (
	"context"
	"strings"

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

// Component 描述应用启动阶段可插拔组件。
// 组件只负责装配依赖与注册能力，不直接启动后台协程，真正启动由 App.Start 统一编排。
type Component interface {
	Name() string                                    // Name 返回组件名称
	Register(context.Context, *ComponentState) error // Register 注册组件依赖
}

// lifecycleHook 表示一个启动期或停止期钩子。
// 组件在 Register 阶段把自己的后台生命周期钩子注册到这里，后续由 App 统一调度执行。
type lifecycleHook struct {
	name string                      // 钩子名称，便于排查启动/停止失败来源
	run  func(context.Context) error // 实际执行函数
}

// ComponentState 表示启动组件之间共享的装配上下文。
type ComponentState struct {
	Config         config.Config               // 当前启动配置快照
	Mode           int                         // 当前运行模式位掩码
	Options        Options                     // 启动扩展选项
	ServiceContext *svc.ServiceContext         // 全局服务上下文
	Shutdown       func(context.Context) error // 基础设施关闭钩子
	Server         *rest.Server                // HTTP 服务实例
	TaskManager    *taskqueue.Manager          // 任务队列管理器
	TaskRuntime    *taskruntime.Runtime        // 任务运行时注册中心
	TaskRedis      redis.UniversalClient       // 任务系统使用的 Redis 客户端
	TaskRedisOwned bool                        // 当前应用是否负责关闭 TaskRedis
	startHooks     []lifecycleHook             // 组件注册的启动钩子，按注册顺序执行
	stopHooks      []lifecycleHook             // 组件注册的停止钩子，按启动逆序执行
}

// componentRuntimeSnapshot 表示装配完成后交给 App 持有的运行时快照。
// 这里显式复制生命周期钩子切片，避免组件装配阶段后续误改共享到底层运行态。
type componentRuntimeSnapshot struct {
	Server         *rest.Server          // HTTP 服务实例
	ServiceContext *svc.ServiceContext   // 全局服务上下文
	TaskManager    *taskqueue.Manager    // 任务队列管理器
	TaskRuntime    *taskruntime.Runtime  // 任务运行时注册中心
	TaskRedis      redis.UniversalClient // 任务系统使用的 Redis 客户端
	TaskRedisOwned bool                  // 当前应用是否负责关闭 TaskRedis
	startHooks     []lifecycleHook       // 启动钩子快照
	stopHooks      []lifecycleHook       // 停止钩子快照
}

// AddLifecycleHooks 为当前组件追加一组生命周期钩子。
// startHook/stopHook 任意一项为空时会自动忽略，便于组件只声明自己真正需要的阶段。
func (s *ComponentState) AddLifecycleHooks(name string, startHook func(context.Context) error, stopHook func(context.Context) error) error {
	if s == nil {
		return errors.Errorf("组件状态为空")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.Errorf("生命周期钩子名称不能为空")
	}
	if startHook != nil {
		if hasLifecycleHook(s.startHooks, name) {
			return errors.Errorf("启动钩子已注册: %s", name)
		}
		s.startHooks = append(s.startHooks, lifecycleHook{
			name: name,
			run:  startHook,
		})
	}
	if stopHook != nil {
		if hasLifecycleHook(s.stopHooks, name) {
			return errors.Errorf("停止钩子已注册: %s", name)
		}
		s.stopHooks = append(s.stopHooks, lifecycleHook{
			name: name,
			run:  stopHook,
		})
	}
	return nil
}

// hasLifecycleHook 判断当前钩子列表中是否已存在同名条目。
func hasLifecycleHook(hooks []lifecycleHook, name string) bool {
	for _, hook := range hooks {
		if hook.name == name {
			return true
		}
	}
	return false
}

// ComponentFunc 允许通过函数快速声明启动组件，保持和任务插件一致的轻量风格。
type ComponentFunc struct {
	name     string                                       // 组件名称
	register func(context.Context, *ComponentState) error // 组件注册逻辑
}

// NewComponentFunc 创建函数式启动组件。
func NewComponentFunc(name string, register func(context.Context, *ComponentState) error) Component {
	return ComponentFunc{name: strings.TrimSpace(name), register: register}
}

// Name 返回组件名称。
func (c ComponentFunc) Name() string {
	return c.name
}

// Register 执行组件注册逻辑。
func (c ComponentFunc) Register(ctx context.Context, state *ComponentState) error {
	if c.register == nil {
		return nil
	}
	return c.register(ctx, state)
}

// ComponentRegistry 负责按固定顺序注册启动组件。
type ComponentRegistry struct {
	components []Component // 待注册组件列表
}

// NewComponentRegistry 创建启动组件注册中心。
func NewComponentRegistry(components ...Component) *ComponentRegistry {
	copied := make([]Component, 0, len(components))
	for _, component := range components {
		if component != nil {
			copied = append(copied, component)
		}
	}
	return &ComponentRegistry{components: copied}
}

// Components 返回当前注册中心持有的组件副本。
func (r *ComponentRegistry) Components() []Component {
	if r == nil {
		return nil
	}
	copied := make([]Component, len(r.components))
	copy(copied, r.components)
	return copied
}

// Register 按声明顺序注册组件，并校验组件名称唯一。
func (r *ComponentRegistry) Register(ctx context.Context, state *ComponentState) error {
	if r == nil {
		return nil
	}
	registered := make(map[string]struct{}, len(r.components))
	for _, component := range r.components {
		if component == nil {
			continue
		}
		name := strings.TrimSpace(component.Name())
		if name == "" {
			return errors.Errorf("启动组件名称不能为空")
		}
		if _, exists := registered[name]; exists {
			return errors.Errorf("启动组件已注册: %s", name)
		}
		// 每个组件只做装配注册，后台协程统一留给 App.Start 启动。
		if err := component.Register(ctx, state); err != nil {
			return errors.Wrapf(err, "注册启动组件失败 %s", name)
		}
		registered[name] = struct{}{}
		loggerx.Infow(ctx, "启动 组件注册成功",
			logx.Field("component", name),
		)
	}
	return nil
}

// newComponentState 创建启动组件共享上下文。
func newComponentState(c config.Config, mode int, options Options, svcCtx *svc.ServiceContext, shutdown func(context.Context) error) *ComponentState {
	return &ComponentState{
		Config:         c,
		Mode:           mode,
		Options:        options,
		ServiceContext: svcCtx,
		Shutdown:       shutdown,
	}
}

// snapshotComponentRuntime 把装配态状态冻结成运行时快照，收紧 ComponentState 与 App 的共享边界。
func snapshotComponentRuntime(state *ComponentState) componentRuntimeSnapshot {
	if state == nil {
		return componentRuntimeSnapshot{}
	}
	return componentRuntimeSnapshot{
		Server:         state.Server,
		ServiceContext: state.ServiceContext,
		TaskManager:    state.TaskManager,
		TaskRuntime:    state.TaskRuntime,
		TaskRedis:      state.TaskRedis,
		TaskRedisOwned: state.TaskRedisOwned,
		startHooks:     append([]lifecycleHook(nil), state.startHooks...),
		stopHooks:      append([]lifecycleHook(nil), state.stopHooks...),
	}
}

// resolveStartupComponents 合并内置组件与外部组件，保持外部组件在内置组件之后执行。
func resolveStartupComponents(options Options) []Component {
	components := make([]Component, 0, len(options.Components)+3)
	if options.UseDefaultComponents {
		components = append(components, defaultComponents()...)
	}
	components = append(components, options.Components...)
	return components
}

// cleanupComponentState 在启动装配失败时释放已经创建的组件资源。
func cleanupComponentState(ctx context.Context, state *ComponentState) {
	if state == nil {
		return
	}
	// 注册失败时沿用 App 停机同一套资源释放顺序，避免不同失败阶段遗漏 DB/Kafka 连接池。
	_ = closeServiceContextResources(state.ServiceContext, state.TaskRedis, state.TaskRedisOwned)
	if state.Shutdown != nil {
		_ = state.Shutdown(ctx)
	}
}
