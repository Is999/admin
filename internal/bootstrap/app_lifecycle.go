package bootstrap

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Is999/go-utils/errors"
)

// runStartHooks 按注册顺序启动所有组件生命周期钩子。
func (a *App) runStartHooks(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, hook := range a.startHooks {
		if hook.Run == nil {
			continue
		}
		if err := hook.Run(ctx); err != nil {
			a.notifyLifecycleFailure(ctx, "start", hook.Name, err)
			// 已启动的前置组件需要立即回滚，避免 Start 返回错误后后台协程继续运行。
			stopErr := a.runStopHooks(ctx)
			if stopErr != nil {
				a.notifyLifecycleFailure(ctx, "rollback_stop", hook.Name, stopErr)
				return errors.Wrapf(err, "启动生命周期钩子失败: %s; 回滚已启动组件失败: %v", hook.Name, stopErr)
			}
			return errors.Wrapf(err, "启动生命周期钩子失败: %s", hook.Name)
		}
		a.markLifecycleHookStarted(hook.Name)
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
		if hook.Run == nil {
			continue
		}
		if _, ok := startedHooks[hook.Name]; !ok {
			continue
		}
		if err := hook.Run(ctx); err != nil {
			a.notifyLifecycleFailure(ctx, "stop", hook.Name, err)
			if firstErr == nil {
				firstErr = errors.Wrapf(err, "停止生命周期钩子失败: %s", hook.Name)
			}
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

// waitForSignal 在纯后台模式下阻塞主 goroutine，直到收到退出信号。
func waitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	<-sigCh
}
