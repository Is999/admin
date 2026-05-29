package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"admin/internal/config"
	"admin/internal/infra/loggerx"
	runtimeconfig "admin/internal/logic/runtimeconfig"
	"admin/internal/svc"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// stopConfigHotReload 停止配置热加载后台协程。
func (a *App) stopConfigHotReload() {
	if a == nil {
		return
	}
	a.hotReload.stateMu.Lock()
	if a.hotReload.cancel == nil {
		a.hotReload.stateMu.Unlock()
		return
	}
	cancel := a.hotReload.cancel
	a.hotReload.cancel = nil
	a.hotReload.stateMu.Unlock()
	cancel()
	a.hotReload.wg.Wait()
}

// startConfigHotReload 在启用时启动后台配置轮询协程。
// 该协程面向 K8s ConfigMap 原子替换场景，只负责刷新配置快照，不在线重建基础设施连接。
func (a *App) startConfigHotReload() {
	if a == nil {
		return
	}
	cfg := a.CurrentConfig()
	interval := normalizeHotReloadCheckInterval(cfg.HotReload.CheckIntervalSeconds)
	configFile := a.boundConfigFile()
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = cfg.HotReload.Enabled
		status.Watching = false
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(interval / time.Second)
		if status.LastStatus == "" {
			status.LastStatus = "idle"
			status.LastMessage = "热加载监听尚未启动"
		}
		return status
	})
	if configFile == "" || !cfg.HotReload.Enabled {
		return
	}
	a.hotReload.stateMu.Lock()
	if a.hotReload.cancel != nil {
		a.hotReload.stateMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.hotReload.cancel = cancel
	a.hotReload.stateMu.Unlock()
	a.hotReload.wg.Add(1)
	go func() {
		defer a.hotReload.wg.Done()
		a.watchConfigFile(ctx, configFile)
	}()
	loggerx.Infow(ctx, "配置 热加载已启用",
		logx.Field("file", configFile),
		logx.Field(loggerx.FieldIntervalSeconds, int(interval/time.Second)),
	)
}

// watchConfigFile 轮询配置文件指纹，检测到变化后重新解析并刷新配置快照。
func (a *App) watchConfigFile(ctx context.Context, configFile string) {
	interval := normalizeHotReloadCheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds)
	lastFingerprint, err := configBundleFingerprint(configFile)
	if err != nil {
		a.markHotReloadFailure("初始化配置文件指纹失败", err, "", "startup", "fingerprint", configFile)
		a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
			status.Enabled = a.CurrentConfig().HotReload.Enabled
			status.Watching = false
			status.ConfigFile = configFile
			status.CheckIntervalSeconds = int(interval / time.Second)
			return status
		})
		return
	}
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = a.CurrentConfig().HotReload.Enabled
		status.Watching = true
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(interval / time.Second)
		status.ConfigVersion = lastFingerprint
		status.ConfigSummary = buildHotReloadConfigSummary(a.CurrentConfig())
		status.LastTriggerSource = "startup"
		if status.LastStatus == "" || status.LastStatus == "idle" {
			status.LastStatus = "idle"
			status.LastMessage = "热加载监听运行中"
		}
		return status
	})
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		if !a.CurrentConfig().HotReload.Enabled {
			a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
				status.Enabled = false
				status.Watching = false
				status.CheckIntervalSeconds = int(normalizeHotReloadCheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds) / time.Second)
				if strings.TrimSpace(status.LastMessage) == "" || status.LastStatus == "idle" {
					status.LastStatus = "idle"
					status.LastMessage = "热加载监听已关闭"
				}
				return status
			})
			return
		}
		select {
		case <-ctx.Done():
			a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
				status.Watching = false
				if status.LastStatus == "" {
					status.LastStatus = "idle"
				}
				if strings.TrimSpace(status.LastMessage) == "" {
					status.LastMessage = "热加载监听已停止"
				}
				return status
			})
			return
		case <-timer.C:
			now := time.Now()
			a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
				status.LastCheckedAt = now
				status.CheckIntervalSeconds = int(normalizeHotReloadCheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds) / time.Second)
				return status
			})
			currentFingerprint, statErr := configBundleFingerprint(configFile)
			if statErr != nil {
				a.markHotReloadFailure("读取配置文件状态失败", statErr, "", "watcher", "fingerprint", configFile)
				timer.Reset(normalizeHotReloadCheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds))
				continue
			}
			if currentFingerprint != lastFingerprint {
				previousFingerprint, reloadErr := a.reloadConfigFile(ctx, "watcher", configFile)
				if reloadErr == nil {
					_ = previousFingerprint
					lastFingerprint = a.CurrentConfigVersion()
				}
			}
			if !a.CurrentConfig().HotReload.Enabled {
				a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
					status.Enabled = false
					status.Watching = false
					status.LastStatus = "idle"
					status.LastMessage = "热加载监听已关闭"
					return status
				})
				return
			}
			timer.Reset(normalizeHotReloadCheckInterval(a.CurrentConfig().HotReload.CheckIntervalSeconds))
		}
	}
}

// configFileFingerprint 返回单个配置文件当前的稳定指纹。
// 对 K8s ConfigMap 场景，真实路径、文件元信息和内容哈希变化都视为配置已更新。
func configFileFingerprint(file string) (string, error) {
	cleanFile := filepath.Clean(strings.TrimSpace(file))
	info, err := os.Stat(cleanFile)
	if err != nil {
		return "", errors.Tag(err)
	}
	data, err := os.ReadFile(cleanFile)
	if err != nil {
		return "", errors.Tag(err)
	}
	realPath, err := filepath.EvalSymlinks(cleanFile)
	if err != nil {
		realPath = cleanFile
	}
	return fmt.Sprintf("%s|%d|%d|%s", realPath, info.Size(), info.ModTime().UnixNano(), utils.Sha256(string(data))), nil
}

// configBundleFingerprint 返回主配置及其外部配置文件组成的配置包指纹。
// 任意一个外部文件发生 ConfigMap 原子替换、大小变化或 mtime 变化，都会触发热加载。
func configBundleFingerprint(file string) (string, error) {
	mainFingerprint, err := configFileFingerprint(file)
	if err != nil {
		return "", errors.Tag(err)
	}
	cfg, err := loadBaseConfig(file)
	if err != nil {
		// 主配置尚未能成功解析时，仍返回主文件指纹，让热加载进入 LoadConfig 阶段并记录明确错误。
		return mainFingerprint, nil
	}
	parts := []string{mainFingerprint}
	for _, include := range configIncludePaths(file, cfg.ConfigFiles) {
		fingerprint, innerErr := configFileFingerprint(include)
		if innerErr != nil {
			return "", errors.Wrapf(innerErr, "读取外部配置文件指纹失败 file=%s", include)
		}
		parts = append(parts, fingerprint)
	}
	return strings.Join(parts, "||"), nil
}

// refreshHotReloadStatus 在当前状态基础上执行原子更新，确保 watcher 和管理接口看到同一份快照。
func (a *App) refreshHotReloadStatus(mutator func(svc.HotReloadStatus) svc.HotReloadStatus) {
	if a == nil || a.ServiceContext == nil || mutator == nil {
		return
	}
	a.hotReload.statusMu.Lock()
	defer a.hotReload.statusMu.Unlock()
	status := a.ServiceContext.CurrentHotReloadStatus()
	a.ServiceContext.UpdateHotReloadStatus(mutator(status))
}

// markHotReloadFailure 记录最近一次热加载失败状态，并对重复错误做简单限频，避免日志刷屏。
func (a *App) markHotReloadFailure(message string, err error, fingerprint, source, category, configFile string) {
	if a == nil {
		return
	}
	now := time.Now()
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.LastStatus = "failed"
		if strings.TrimSpace(message) != "" {
			status.LastMessage = strings.TrimSpace(message)
		} else {
			status.LastMessage = "配置热加载失败"
		}
		status.LastReloadAt = now
		status.LastFailureAt = now
		status.LastTriggerSource = normalizeHotReloadSource(source)
		status.LastFailureCategory = normalizeHotReloadFailureCategory(category)
		if fingerprint != "" {
			status.ConfigVersion = fingerprint
		}
		return status
	})
	errorKey := message + "|" + errText + "|" + source + "|" + category
	a.hotReload.logMu.Lock()
	sameError := errorKey == a.hotReload.lastError && !a.hotReload.lastLogAt.IsZero() && now.Sub(a.hotReload.lastLogAt) < 30*time.Second
	if sameError {
		a.hotReload.lastError = errorKey
		a.hotReload.logMu.Unlock()
		a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
			status.SuppressedFailureCount++
			return status
		})
		return
	}
	a.hotReload.lastError = errorKey
	a.hotReload.lastLogAt = now
	a.hotReload.logMu.Unlock()
	fields := []logx.LogField{
		logx.Field("file", configFile),
		logx.Field("detail", message),
		logx.Field("version", fingerprint),
		logx.Field("source", normalizeHotReloadSource(source)),
		logx.Field("category", normalizeHotReloadFailureCategory(category)),
	}
	loggerx.ErrorTextw(nil, "配置 热加载失败", errText, fields...)
}

// reloadConfigFile 串行执行一次配置文件重载，供 watcher 和手动接口共用。
func (a *App) reloadConfigFile(ctx context.Context, source string, configFile string) (string, error) {
	if a == nil {
		return "", errors.Errorf("应用实例为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	configFile = strings.TrimSpace(configFile)
	if configFile == "" {
		return "", errors.Errorf("未绑定配置文件路径")
	}
	a.hotReload.execMu.Lock()
	defer a.hotReload.execMu.Unlock()
	select {
	case <-ctx.Done():
		err := errors.Tag(ctx.Err())
		a.markHotReloadFailure("配置热加载已取消", err, "", source, "cancelled", configFile)
		return "", err
	default:
	}

	beforeCfg := a.CurrentConfig()
	currentFingerprint, err := configBundleFingerprint(configFile)
	if err != nil {
		a.markHotReloadFailure("读取配置文件指纹失败", err, "", source, "fingerprint", configFile)
		return "", errors.Tag(err)
	}
	previousFingerprint := a.CurrentConfigVersion()
	if previousFingerprint != "" && currentFingerprint == previousFingerprint {
		a.markHotReloadUnchanged(configFile, source, currentFingerprint)
		return previousFingerprint, nil
	}
	cfg, err := LoadConfig(configFile)
	if err != nil {
		a.markHotReloadFailure("配置热加载失败", err, currentFingerprint, source, "load", configFile)
		return "", errors.Tag(err)
	}
	if runtimeconfig.IsDatabaseSource(beforeCfg) && runtimeconfig.IsDatabaseSource(cfg) {
		active, loadErr := runtimeconfig.LoadActiveSnapshotCached(ctx, a.ServiceContext)
		if loadErr != nil {
			a.markHotReloadFailure("加载数据库运行配置失败", loadErr, currentFingerprint, source, "load_runtime_config", configFile)
			return "", errors.Tag(loadErr)
		}
		runtimeconfig.ApplySnapshot(&cfg, active.Snapshot)
	}

	restartRequired, restartReason := detectHotReloadRestartImpact(beforeCfg, cfg)
	effectiveCfg := cfg
	if restartRequired {
		effectiveCfg = buildHotReloadEffectiveConfig(beforeCfg, cfg)
	}
	publishRuntimeConfig(effectiveCfg)
	a.UpdateConfig(effectiveCfg)
	now := time.Now()
	successMessage := "配置热加载成功"
	if restartRequired {
		successMessage = "配置热加载成功，需重启进程的配置已保留为当前运行值"
	}
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.Enabled = effectiveCfg.HotReload.Enabled
		status.ConfigFile = configFile
		status.CheckIntervalSeconds = int(normalizeHotReloadCheckInterval(effectiveCfg.HotReload.CheckIntervalSeconds) / time.Second)
		status.ConfigVersion = currentFingerprint
		status.ConfigSummary = buildHotReloadConfigSummary(effectiveCfg)
		status.RestartRequired = restartRequired
		status.RestartReason = restartReason
		status.LastStatus = "success"
		status.LastMessage = successMessage
		status.LastTriggerSource = normalizeHotReloadSource(source)
		status.LastFailureCategory = ""
		status.LastReloadAt = now
		status.LastSuccessAt = now
		status.ReloadCount++
		return status
	})
	a.hotReload.logMu.Lock()
	a.hotReload.lastError = ""
	a.hotReload.lastLogAt = time.Time{}
	a.hotReload.logMu.Unlock()
	loggerx.Infow(ctx, "配置 热加载成功",
		logx.Field("file", configFile),
		logx.Field("from_version", previousFingerprint),
		logx.Field("to_version", currentFingerprint),
		logx.Field(loggerx.FieldIntervalSeconds, effectiveCfg.HotReload.CheckIntervalSeconds),
		logx.Field("source", normalizeHotReloadSource(source)),
		logx.Field("summary", buildHotReloadConfigSummary(effectiveCfg)),
		logx.Field("requested_summary", buildHotReloadConfigSummary(cfg)),
		logx.Field("restart_required", restartRequired),
		logx.Field("restart_reason", restartReason),
	)
	if effectiveCfg.HotReload.Enabled && !a.isConfigHotReloadRunning() {
		a.startConfigHotReload()
	}
	if !effectiveCfg.HotReload.Enabled && normalizeHotReloadSource(source) != "watcher" {
		a.stopConfigHotReload()
	}
	return previousFingerprint, nil
}

// markHotReloadUnchanged 记录一次无配置变更的热加载检查，不刷新运行配置快照。
func (a *App) markHotReloadUnchanged(configFile, source, fingerprint string) {
	if a == nil {
		return
	}
	now := time.Now()
	a.refreshHotReloadStatus(func(status svc.HotReloadStatus) svc.HotReloadStatus {
		status.ConfigFile = strings.TrimSpace(configFile)
		status.ConfigVersion = strings.TrimSpace(fingerprint)
		status.ConfigSummary = buildHotReloadConfigSummary(a.CurrentConfig())
		status.LastStatus = "success"
		status.LastMessage = "配置无变化"
		status.LastTriggerSource = normalizeHotReloadSource(source)
		status.LastFailureCategory = ""
		status.LastCheckedAt = now
		return status
	})
}

// CurrentConfigVersion 返回当前热加载状态中的配置版本指纹。
func (a *App) CurrentConfigVersion() string {
	if a == nil || a.ServiceContext == nil {
		return ""
	}
	return a.ServiceContext.CurrentHotReloadStatus().ConfigVersion
}

// buildHotReloadConfigSummary 生成关键配置摘要，便于管理接口和日志快速确认核心开关。
func buildHotReloadConfigSummary(cfg config.Config) string {
	return fmt.Sprintf(
		"mode=%s task=%t periodic=%d hot_reload=%t kafka=%t",
		strings.TrimSpace(cfg.Mode),
		cfg.Task.Enabled,
		len(cfg.Task.Periodic),
		cfg.HotReload.Enabled,
		cfg.Kafka.Enabled,
	)
}

// normalizeHotReloadCheckInterval 统一热加载轮询间隔的最小兜底值。
func normalizeHotReloadCheckInterval(seconds int) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 5 * time.Second
}

// hotReloadRestartChanged 判断一个配置边界是否发生变化。
type hotReloadRestartChanged func(before, after config.Config) bool

// hotReloadRestartPreserve 保留当前进程仍在使用的旧配置。
type hotReloadRestartPreserve func(effective *config.Config, before config.Config, after config.Config)

// hotReloadRestartSpec 描述一个热加载后仍需重启才能完全生效的配置边界。
type hotReloadRestartSpec struct {
	Reason   string                   // 展示给管理接口和日志的重启原因
	Changed  hotReloadRestartChanged  // 判断该边界是否发生变化
	Preserve hotReloadRestartPreserve // 保留当前进程仍在使用的旧配置
}

// hotReloadRestartSpecs 返回热加载不可在线重建的配置边界，顺序即 restartReason 展示顺序。
func hotReloadRestartSpecs() []hotReloadRestartSpec {
	return []hotReloadRestartSpec{
		{
			Reason: "app_id",
			Changed: func(before, after config.Config) bool {
				return strings.TrimSpace(before.AppID) != strings.TrimSpace(after.AppID)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.AppID = before.AppID
			},
		},
		{
			Reason: "run_mode",
			Changed: func(before, after config.Config) bool {
				return before.RunMode != after.RunMode
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.RunMode = before.RunMode
			},
		},
		{
			Reason: componentNameHTTPServer,
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.RestConf, after.RestConf)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.RestConf = before.RestConf
			},
		},
		{
			Reason: "mode",
			Changed: func(before, after config.Config) bool {
				return reflect.DeepEqual(before.RestConf, after.RestConf) && strings.TrimSpace(before.Mode) != strings.TrimSpace(after.Mode)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.Mode = before.Mode
			},
		},
		{
			Reason: "mysql",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.MySQL, after.MySQL)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.MySQL = before.MySQL
			},
		},
		{
			Reason: "site_mysql",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.SiteMySQL, after.SiteMySQL)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.SiteMySQL = before.SiteMySQL
			},
		},
		{
			Reason: "redis",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.Redis, after.Redis)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.Redis = before.Redis
			},
		},
		{
			Reason: "task.runtime",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(taskRuntimeConfigForRestartCheck(before.Task), taskRuntimeConfigForRestartCheck(after.Task))
			},
			Preserve: func(effective *config.Config, before config.Config, after config.Config) {
				periodic := append([]config.TaskPeriodicConfig(nil), after.Task.Periodic...)
				effective.Task = before.Task
				effective.Task.Periodic = periodic
			},
		},
		{
			Reason: "runtime_config.source_env",
			Changed: func(before, after config.Config) bool {
				return runtimeconfig.NormalizeSource(before.RuntimeConfig.Source) != runtimeconfig.NormalizeSource(after.RuntimeConfig.Source) ||
					runtimeconfig.RuntimeEnv(before) != runtimeconfig.RuntimeEnv(after)
			},
			Preserve: func(effective *config.Config, before config.Config, after config.Config) {
				pollInterval := after.RuntimeConfig.PollIntervalSeconds
				effective.RuntimeConfig = before.RuntimeConfig
				effective.RuntimeConfig.PollIntervalSeconds = pollInterval
			},
		},
		{
			Reason: "kafka",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.Kafka, after.Kafka)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.Kafka = before.Kafka
			},
		},
		{
			Reason: "observability",
			Changed: func(before, after config.Config) bool {
				return !reflect.DeepEqual(before.Observability, after.Observability)
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.Observability = before.Observability
			},
		},
		{
			Reason: "workflows.user_tag.enabled",
			Changed: func(before, after config.Config) bool {
				return before.Workflows.UserTag.Enabled != after.Workflows.UserTag.Enabled
			},
			Preserve: func(effective *config.Config, before config.Config, _ config.Config) {
				effective.Workflows.UserTag.Enabled = before.Workflows.UserTag.Enabled
			},
		},
	}
}

// changedHotReloadRestartSpecs 返回本次热加载实际命中的重启边界。
func changedHotReloadRestartSpecs(before, after config.Config) []hotReloadRestartSpec {
	specs := hotReloadRestartSpecs()
	changed := make([]hotReloadRestartSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Changed == nil || !spec.Changed(before, after) {
			continue
		}
		changed = append(changed, spec)
	}
	return changed
}

// buildHotReloadEffectiveConfig 生成本进程可以立即生效的配置快照。
// 只动态刷新运行期配置；基础设施和任务运行时变更记为待重启。
func buildHotReloadEffectiveConfig(before, after config.Config) config.Config {
	effective := after
	for _, spec := range changedHotReloadRestartSpecs(before, after) {
		if spec.Preserve == nil {
			continue
		}
		spec.Preserve(&effective, before, after)
	}
	return effective
}

// detectHotReloadRestartImpact 识别本次热加载中哪些配置虽然已加载，但仍需重启进程才能完全生效。
func detectHotReloadRestartImpact(before, after config.Config) (bool, string) {
	changedSpecs := changedHotReloadRestartSpecs(before, after)
	reasons := make([]string, 0, len(changedSpecs))
	for _, spec := range changedSpecs {
		reasons = append(reasons, spec.Reason)
	}
	if len(reasons) == 0 {
		return false, ""
	}
	return true, "以下配置已变更，需重启进程才能完全生效: " + strings.Join(reasons, ", ")
}

// taskRuntimeConfigForRestartCheck 返回影响任务运行时生命周期的配置片段。
// 周期任务列表可动态同步，其它任务运行时字段参与重启判定。
func taskRuntimeConfigForRestartCheck(cfg config.TaskQueueConfig) config.TaskQueueConfig {
	cfg.Periodic = nil
	return cfg
}
