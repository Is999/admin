package appalert

import (
	"strings"

	"admin/internal/bootstrap/hotreload"
	"admin/internal/svc"
)

// LifecycleFailure 生成服务生命周期失败告警。
func LifecycleFailure(phase, hookName string, err error) svc.TaskRuntimeAlert {
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
	return svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindAppLifecycleFailed,
		Title:     "【P1 服务生命周期失败】",
		Status:    status,
		Component: "app_lifecycle",
		Operation: phase,
		TaskName:  hookName,
		UniqueKey: phase + ":" + hookName,
		Reason:    errorReason(err),
		Advice:    "请检查对应组件的启动/停止日志和外部依赖状态；如发生在停止阶段，需要确认进程托管平台是否完成优雅退出以及资源是否存在残留连接。",
	}
}

// ConfigReloadFailure 生成 config.yaml 热加载失败告警。
func ConfigReloadFailure(message string, err error, source, category, configFile string) svc.TaskRuntimeAlert {
	reason := configReloadReason(message, err, configFile)
	return svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindConfigReloadFailed,
		Title:     "【P1 配置热加载失败】",
		Status:    "配置热加载未生效，当前进程继续使用上一版有效配置",
		Component: "config_reload",
		Operation: hotreload.FailureCategory(category),
		TaskName:  hotreload.Source(source),
		UniqueKey: hotreload.Source(source) + ":" + hotreload.FailureCategory(category),
		Reason:    reason,
		Advice:    "请检查配置文件内容、外部 include 文件、runtime_config.source 与启动期配置边界；修复后重新触发热加载，并关注 restartRequired/restartReason。",
	}
}

// configReloadReason 拼接配置热加载失败原因，保留配置文件路径便于排查。
func configReloadReason(message string, err error, configFile string) string {
	parts := make([]string, 0, 3)
	if message = strings.TrimSpace(message); message != "" {
		parts = append(parts, message)
	}
	if configFile = strings.TrimSpace(configFile); configFile != "" {
		parts = append(parts, "配置文件="+configFile)
	}
	if reason := errorReason(err); reason != "" {
		parts = append(parts, reason)
	}
	return strings.Join(parts, "；")
}

// RuntimeConfigReloadFailure 生成 DB 运行配置热加载失败告警。
func RuntimeConfigReloadFailure(source string, err error) svc.TaskRuntimeAlert {
	return svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindRuntimeConfigReloadFailed,
		Title:     "【P1 DB运行配置热加载失败】",
		Status:    "active release 未加载到当前进程，继续使用上一版运行配置",
		Component: "runtime_config",
		Operation: "reload_active_release",
		TaskName:  hotreload.Source(source),
		UniqueKey: hotreload.Source(source),
		Reason:    errorReason(err),
		Advice:    "请检查 runtime_config_state active release、发布快照完整性、数据库连接和配置字段兼容性；修复后重新发布或手动触发热加载。",
	}
}

// errorReason 返回告警原因文本，避免空错误导致告警字段缺失。
func errorReason(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
