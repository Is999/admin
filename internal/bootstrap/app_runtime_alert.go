package bootstrap

import (
	"context"
	"strings"

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
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "unknown"
	}
	hookName = strings.TrimSpace(hookName)
	status := "服务生命周期钩子失败，进程启动或平滑停止未按预期完成"
	if phase == "start" {
		status = "服务启动钩子失败，已尝试回滚已启动组件"
	} else if phase == "stop" {
		status = "服务平滑停止钩子失败，部分资源可能未完整释放"
	}
	a.notifyAppRuntimeAlert(ctx, svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindAppLifecycleFailed,
		Title:     "【P1 服务生命周期失败】",
		Status:    status,
		Component: "app_lifecycle",
		Operation: phase,
		TaskName:  hookName,
		UniqueKey: phase + ":" + hookName,
		Reason:    err.Error(),
		Advice:    "请检查对应组件的启动/停止日志和外部依赖状态；如发生在停止阶段，需要确认进程托管平台是否完成优雅退出以及资源是否存在残留连接。",
	})
}

// notifyConfigReloadFailure 上报 config.yaml 热加载失败。
func (a *App) notifyConfigReloadFailure(message string, err error, source, category, configFile string) {
	if err == nil {
		return
	}
	reason := strings.TrimSpace(message)
	if reason != "" {
		reason += "；"
	}
	reason += err.Error()
	a.notifyAppRuntimeAlert(context.Background(), svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindConfigReloadFailed,
		Title:     "【P1 配置热加载失败】",
		Status:    "配置热加载未生效，当前进程继续使用上一版有效配置",
		Component: "config_reload",
		Operation: normalizeHotReloadFailureCategory(category),
		TaskName:  normalizeHotReloadSource(source),
		UniqueKey: normalizeHotReloadSource(source) + ":" + normalizeHotReloadFailureCategory(category),
		Reason:    reason,
		Advice:    "请检查配置文件内容、外部 include 文件、runtime_config.source 与启动期配置边界；修复后重新触发热加载，并关注 restartRequired/restartReason。",
	})
}

// notifyRuntimeConfigReloadFailure 上报 DB 运行配置热加载失败。
func (a *App) notifyRuntimeConfigReloadFailure(ctx context.Context, source string, err error) {
	if err == nil {
		return
	}
	a.notifyAppRuntimeAlert(ctx, svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindRuntimeConfigReloadFailed,
		Title:     "【P1 DB运行配置热加载失败】",
		Status:    "active release 未加载到当前进程，继续使用上一版运行配置",
		Component: "runtime_config",
		Operation: "reload_active_release",
		TaskName:  normalizeHotReloadSource(source),
		UniqueKey: normalizeHotReloadSource(source),
		Reason:    err.Error(),
		Advice:    "请检查 runtime_config_state active release、发布快照完整性、数据库连接和配置字段兼容性；修复后重新发布或手动触发热加载。",
	})
}
