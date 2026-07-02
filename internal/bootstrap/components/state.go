package components

import (
	"context"
	"strings"

	"admin/internal/config"
	"admin/internal/handler"
	"admin/internal/svc"
	"admin/internal/task/queue"
	taskruntime "admin/internal/task/runtime"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest"
)

// State 表示启动组件之间共享的装配上下文。
type State struct {
	Config         config.Config               // 当前启动配置快照
	Mode           int                         // 当前运行模式位掩码
	RouteModules   []handler.RouteModule       // 外部追加的 HTTP 路由模块
	TaskPlugins    []taskruntime.Plugin        // 外部追加的任务运行时插件
	ServiceContext *svc.ServiceContext         // 全局服务上下文
	Shutdown       func(context.Context) error // 基础设施关闭钩子
	Server         *rest.Server                // HTTP 服务实例
	TaskManager    *taskqueue.Manager          // 任务队列管理器
	TaskRuntime    *taskruntime.Runtime        // 任务运行时注册中心
	TaskRedis      redis.UniversalClient       // 任务系统使用的 Redis 客户端
	TaskRedisOwned bool                        // 当前应用是否负责关闭 TaskRedis
	startHooks     []LifecycleHook             // 组件注册的启动钩子，按注册顺序执行
	stopHooks      []LifecycleHook             // 组件注册的停止钩子，按启动逆序执行
}

// RuntimeSnapshot 表示装配完成后交给 App 持有的运行时快照。
type RuntimeSnapshot struct {
	Server         *rest.Server          // HTTP 服务实例
	ServiceContext *svc.ServiceContext   // 全局服务上下文
	TaskManager    *taskqueue.Manager    // 任务队列管理器
	TaskRuntime    *taskruntime.Runtime  // 任务运行时注册中心
	TaskRedis      redis.UniversalClient // 任务系统使用的 Redis 客户端
	TaskRedisOwned bool                  // 当前应用是否负责关闭 TaskRedis
	StartHooks     []LifecycleHook       // 启动钩子快照
	StopHooks      []LifecycleHook       // 停止钩子快照
}

// LifecycleHook 表示一个启动期或停止期钩子。
type LifecycleHook struct {
	Name string                      // 钩子名称，便于排查启动/停止失败来源
	Run  func(context.Context) error // 实际执行函数
}

// NewState 创建启动组件共享上下文。
func NewState(c config.Config, mode int, svcCtx *svc.ServiceContext, shutdown func(context.Context) error, routeModules []handler.RouteModule, taskPlugins []taskruntime.Plugin) *State {
	return &State{
		Config:         c,
		Mode:           mode,
		RouteModules:   routeModules,
		TaskPlugins:    taskPlugins,
		ServiceContext: svcCtx,
		Shutdown:       shutdown,
	}
}

// Snapshot 把装配态状态冻结成运行时快照，收紧 State 与 App 的共享边界。
func Snapshot(state *State) RuntimeSnapshot {
	if state == nil {
		return RuntimeSnapshot{}
	}
	return RuntimeSnapshot{
		Server:         state.Server,
		ServiceContext: state.ServiceContext,
		TaskManager:    state.TaskManager,
		TaskRuntime:    state.TaskRuntime,
		TaskRedis:      state.TaskRedis,
		TaskRedisOwned: state.TaskRedisOwned,
		StartHooks:     append([]LifecycleHook(nil), state.startHooks...),
		StopHooks:      append([]LifecycleHook(nil), state.stopHooks...),
	}
}

// AddLifecycleHooks 为当前组件追加一组生命周期钩子。
func (s *State) AddLifecycleHooks(name string, startHook func(context.Context) error, stopHook func(context.Context) error) error {
	if s == nil {
		return errors.Errorf("组件状态为空")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.Errorf("生命周期钩子名称不能为空")
	}
	if err := validateLifecycleHook(s.startHooks, "启动", name, startHook); err != nil {
		return errors.Tag(err)
	}
	if err := validateLifecycleHook(s.stopHooks, "停止", name, stopHook); err != nil {
		return errors.Tag(err)
	}
	s.startHooks = appendLifecycleHook(s.startHooks, name, startHook)
	s.stopHooks = appendLifecycleHook(s.stopHooks, name, stopHook)
	return nil
}

// validateLifecycleHook 校验非空生命周期钩子名称，避免失败注册留下半写入状态。
func validateLifecycleHook(hooks []LifecycleHook, phase, name string, run func(context.Context) error) error {
	if run == nil {
		return nil
	}
	if hasLifecycleHook(hooks, name) {
		return errors.Errorf("%s钩子已注册: %s", phase, name)
	}
	return nil
}

// appendLifecycleHook 追加非空生命周期钩子；调用方需先完成名称校验。
func appendLifecycleHook(hooks []LifecycleHook, name string, run func(context.Context) error) []LifecycleHook {
	if run == nil {
		return hooks
	}
	return append(hooks, LifecycleHook{
		Name: name,
		Run:  run,
	})
}

// hasLifecycleHook 判断当前钩子列表中是否已存在同名条目。
func hasLifecycleHook(hooks []LifecycleHook, name string) bool {
	for _, hook := range hooks {
		if hook.Name == name {
			return true
		}
	}
	return false
}
