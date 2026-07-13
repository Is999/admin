package bootstrap

import (
	"context"

	"admin/internal/bootstrap/appalert"
	"admin/internal/svc"
)

// notifyComponentRegisterFailure 上报组件注册阶段失败；独立告警器未完成装配时安全降级。
func notifyComponentRegisterFailure(ctx context.Context, svcCtx *svc.ServiceContext, err error) {
	if svcCtx == nil || err == nil {
		return
	}
	svc.NotifyTaskRuntimeAlert(ctx, svcCtx.RuntimeAlerter, appalert.LifecycleFailure("register", "component_registry", err))
}

// notifyAppRuntimeAlert 通过独立告警入口上报 App 层运行异常。
func (a *App) notifyAppRuntimeAlert(ctx context.Context, alert svc.TaskRuntimeAlert) {
	if a == nil || a.ServiceContext == nil {
		return
	}
	svc.NotifyTaskRuntimeAlert(ctx, a.ServiceContext.RuntimeAlerter, alert)
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
