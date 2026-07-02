package hotreload

import (
	"context"
	"strings"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

// failureSuppressWindow 控制相同热加载失败日志和告警的最小间隔。
const failureSuppressWindow = 30 * time.Second

// FailureNotifier 上报配置热加载失败，通常接入任务系统告警。
type FailureNotifier func(message string, err error, source, category, configFile string)

// ConfigProvider 返回当前进程配置快照。
type ConfigProvider func() config.Config

// Recorder 统一记录 config.yaml 热加载状态、日志和失败限频。
type Recorder struct {
	State          *State              // 热加载运行态资源
	ServiceContext *svc.ServiceContext // 热加载状态持久化到服务上下文
	CurrentConfig  ConfigProvider      // 当前配置快照来源
	NotifyFailure  FailureNotifier     // 失败告警回调
}

// UpdateStatus 在当前状态基础上执行原子更新。
func (r Recorder) UpdateStatus(mutator func(svc.HotReloadStatus) svc.HotReloadStatus) {
	if r.State == nil {
		return
	}
	r.State.UpdateStatus(r.ServiceContext, mutator)
}

// CurrentVersion 返回当前热加载状态中的配置版本指纹。
func (r Recorder) CurrentVersion() string {
	if r.ServiceContext == nil {
		return ""
	}
	return r.ServiceContext.CurrentHotReloadStatus().ConfigVersion
}

// Failure 记录最近一次热加载失败状态，并对重复错误做简单限频，避免日志刷屏。
func (r Recorder) Failure(messageKey, message string, err error, fingerprint, source, category, configFile string) {
	now := time.Now()
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.LastStatus = "failed"
		status.LastMessageKey = strings.TrimSpace(messageKey)
		if status.LastMessageKey == "" {
			status.LastMessageKey = i18n.MsgKeyHotReloadFailed
		}
		if strings.TrimSpace(message) != "" {
			status.LastMessage = strings.TrimSpace(message)
		} else {
			status.LastMessage = "配置热加载失败"
		}
		status.LastReloadAt = now
		status.LastFailureAt = now
		status.LastTriggerSource = Source(source)
		status.LastFailureCategory = FailureCategory(category)
		if fingerprint != "" {
			status.ConfigVersion = fingerprint
		}
		return status
	})
	errorKey := message + "|" + errText + "|" + source + "|" + category
	if r.State != nil && r.State.SuppressFailure(errorKey, now, failureSuppressWindow) {
		r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
			status.SuppressedFailureCount++
			return status
		})
		return
	}
	fields := []logx.LogField{
		logx.Field("file", configFile),
		logx.Field("detail", message),
		logx.Field("version", fingerprint),
		logx.Field("source", Source(source)),
		logx.Field("category", FailureCategory(category)),
	}
	loggerx.ErrorTextw(context.Background(), "配置 热加载失败", errText, fields...)
	if err != nil && r.NotifyFailure != nil {
		r.NotifyFailure(message, err, source, category, configFile)
	}
}

// Success 记录一次成功重载后的状态和日志。
func (r Recorder) Success(ctx context.Context, source, configFile string, loaded LoadedFile, applied AppliedConfig) {
	if ctx == nil {
		ctx = context.Background()
	}
	effectiveCfg := applied.Effective
	now := time.Now()
	successMessage := "配置热加载成功"
	if applied.RestartRequired {
		successMessage = "配置热加载成功，需重启进程的配置已保留为当前运行值"
	}
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = effectiveCfg.HotReload.Enabled
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(CheckInterval(effectiveCfg.HotReload.CheckIntervalSeconds) / time.Second)
		status.ConfigVersion = loaded.CurrentFingerprint
		status.ConfigSummary = Summary(effectiveCfg)
		status.RestartRequired = applied.RestartRequired
		status.RestartReason = applied.RestartReason
		status.LastStatus = "success"
		status.LastMessage = successMessage
		status.LastMessageKey = i18n.MsgKeyHotReloadSuccess
		if applied.RestartRequired {
			status.LastMessageKey = i18n.MsgKeyHotReloadSuccessRestart
		}
		status.LastTriggerSource = Source(source)
		status.LastFailureCategory = ""
		status.LastReloadAt = now
		status.LastSuccessAt = now
		status.ReloadCount++
		return status
	})
	if r.State != nil {
		r.State.ResetFailureLog()
	}
	loggerx.Infow(ctx, "配置 热加载成功",
		logx.Field("file", configFile),
		logx.Field("from_version", loaded.PreviousFingerprint),
		logx.Field("to_version", loaded.CurrentFingerprint),
		logx.Field(loggerx.FieldIntervalSeconds, effectiveCfg.HotReload.CheckIntervalSeconds),
		logx.Field("source", Source(source)),
		logx.Field("summary", Summary(effectiveCfg)),
		logx.Field("requested_summary", Summary(loaded.Requested)),
		logx.Field("restart_required", applied.RestartRequired),
		logx.Field("restart_reason", applied.RestartReason),
	)
}

// Unchanged 记录一次无配置变更的热加载检查，不刷新运行配置快照。
func (r Recorder) Unchanged(configFile, source, fingerprint string) {
	now := time.Now()
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.ConfigFile = strings.TrimSpace(configFile)
		status.ConfigVersion = strings.TrimSpace(fingerprint)
		status.ConfigSummary = Summary(r.currentConfig())
		status.LastStatus = "success"
		status.LastMessage = "配置无变化"
		status.LastMessageKey = i18n.MsgKeyHotReloadUnchanged
		status.LastTriggerSource = Source(source)
		status.LastFailureCategory = ""
		status.LastCheckedAt = now
		return status
	})
}

// WatcherStarting 记录 watcher 启动前的基础监听状态。
func (r Recorder) WatcherStarting(cfg config.Config, configFile string, interval time.Duration) {
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = cfg.HotReload.Enabled
		status.Watching = false
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(interval / time.Second)
		if status.LastStatus == "" {
			status.LastStatus = "idle"
			status.LastMessage = "热加载监听尚未启动"
			status.LastMessageKey = i18n.MsgKeyHotReloadWatcherNotStarted
		}
		return status
	})
}

// WatcherInitFailed 标记 watcher 初始化失败后的监听状态。
func (r Recorder) WatcherInitFailed(configFile string, interval time.Duration) {
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = r.currentConfig().HotReload.Enabled
		status.Watching = false
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(interval / time.Second)
		return status
	})
}

// WatcherRunning 标记配置文件 watcher 已进入轮询状态。
func (r Recorder) WatcherRunning(configFile, fingerprint string, interval time.Duration) {
	cfg := r.currentConfig()
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = cfg.HotReload.Enabled
		status.Watching = true
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(interval / time.Second)
		status.ConfigVersion = fingerprint
		status.ConfigSummary = Summary(cfg)
		status.LastTriggerSource = "startup"
		if status.LastStatus == "" || status.LastStatus == "idle" {
			status.LastStatus = "idle"
			status.LastMessage = "热加载监听运行中"
			status.LastMessageKey = i18n.MsgKeyHotReloadWatcherRunning
		}
		return status
	})
}

// WatcherDisabled 标记 watcher 已因配置关闭退出。
func (r Recorder) WatcherDisabled(forceMessage bool) {
	cfg := r.currentConfig()
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = false
		status.Watching = false
		status.CheckIntervalSeconds = int(CheckInterval(cfg.HotReload.CheckIntervalSeconds) / time.Second)
		if forceMessage || strings.TrimSpace(status.LastMessage) == "" || status.LastStatus == "idle" {
			status.LastStatus = "idle"
			status.LastMessage = "热加载监听已关闭"
			status.LastMessageKey = i18n.MsgKeyHotReloadWatcherClosed
		}
		return status
	})
}

// WatcherStopped 标记 watcher 已收到退出信号。
func (r Recorder) WatcherStopped() {
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Watching = false
		if status.LastStatus == "" {
			status.LastStatus = "idle"
		}
		if strings.TrimSpace(status.LastMessage) == "" {
			status.LastMessage = "热加载监听已停止"
			status.LastMessageKey = i18n.MsgKeyHotReloadWatcherStopped
		}
		return status
	})
}

// Checked 记录 watcher 最近一次配置指纹检查时间。
func (r Recorder) Checked(now time.Time) {
	cfg := r.currentConfig()
	r.UpdateStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.LastCheckedAt = now
		status.CheckIntervalSeconds = int(CheckInterval(cfg.HotReload.CheckIntervalSeconds) / time.Second)
		return status
	})
}

// currentConfig 返回当前配置快照，缺省时回退零值配置。
func (r Recorder) currentConfig() config.Config {
	if r.CurrentConfig == nil {
		return config.Config{}
	}
	return r.CurrentConfig()
}
