package bootstrap

import (
	"context"

	"admin/internal/bootstrap/appalert"
	"admin/internal/svc"
)

// notifyAppRuntimeAlert 通过任务系统统一上报 App 层运行异常。
func (a *App) notifyAppRuntimeAlert(ctx context.Context, alert svc.TaskRuntimeAlert) {
	if a == nil || a.ServiceContext == nil {
		return
	}
	svc.NotifyTaskRuntimeAlert(ctx, a.ServiceContext.Task, alert)
}

// notifyLifecycleFailure 上报服务启动或平滑停止生命周期失败。
func (a *App) notifyLifecycleFailure(ctx context.Context, phase, hookName string, err error) {
	if err == nil {
		return
	}
	a.notifyAppRuntimeAlert(ctx, appalert.LifecycleFailure(phase, hookName, err))
}

// notifyConfigReloadFailure 上报 config.yaml 热加载失败。
func (a *App) notifyConfigReloadFailure(message string, err error, source, category, configFile string) {
	if err == nil {
		return
	}
	a.notifyAppRuntimeAlert(context.Background(), appalert.ConfigReloadFailure(message, err, source, category, configFile))
}

// notifyRuntimeConfigReloadFailure 上报 DB 运行配置热加载失败。
func (a *App) notifyRuntimeConfigReloadFailure(ctx context.Context, source string, err error) {
	if err == nil {
		return
	}
	a.notifyAppRuntimeAlert(ctx, appalert.RuntimeConfigReloadFailure(source, err))
}
