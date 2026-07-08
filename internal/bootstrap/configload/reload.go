package configload

import (
	"reflect"
	"strings"

	"admin/internal/config"
	runtimeconfig "admin/internal/logic/runtimeconfig"
)

const (
	restartReasonHTTPServer = "http_server" // HTTP 监听配置变更需重启服务
)

// hotReloadRestartChanged 判断一个配置边界是否发生变化。
type hotReloadRestartChanged func(before, after config.Config) bool

// hotReloadRestartPreserve 保留当前进程仍在使用的上一版配置。
type hotReloadRestartPreserve func(effective *config.Config, before config.Config, after config.Config)

// hotReloadRestartSpec 描述一个热加载后仍需重启才能完全生效的配置边界。
type hotReloadRestartSpec struct {
	Reason   string                   // 展示给管理接口和日志的重启原因
	Changed  hotReloadRestartChanged  // 判断该边界是否发生变化
	Preserve hotReloadRestartPreserve // 保留当前进程仍在使用的上一版配置
}

// hotReloadRestartSpecs 返回热加载不可在线重建的配置边界，顺序即 restartReason 展示顺序。
func hotReloadRestartSpecs() []hotReloadRestartSpec {
	return []hotReloadRestartSpec{
		trimmedStringRestartSpec("app_id", func(cfg config.Config) string {
			return cfg.AppID
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.AppID = before.AppID
		})),
		valueRestartSpec("snowflake", func(cfg config.Config) any {
			return cfg.Snowflake
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Snowflake = before.Snowflake
		})),
		valueRestartSpec("user.route_shard_count", func(cfg config.Config) any {
			return cfg.User.RouteShardCount
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.User.RouteShardCount = before.User.RouteShardCount
		})),
		valueRestartSpec("run_mode", func(cfg config.Config) any {
			return cfg.RunMode
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.RunMode = before.RunMode
		})),
		valueRestartSpec(restartReasonHTTPServer, func(cfg config.Config) any {
			return cfg.RestConf
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.RestConf = before.RestConf
		})),
		newHotReloadRestartSpec("mode", func(before, after config.Config) bool {
			return reflect.DeepEqual(before.RestConf, after.RestConf) && strings.TrimSpace(before.Mode) != strings.TrimSpace(after.Mode)
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Mode = before.Mode
		})),
		valueRestartSpec("mysql", func(cfg config.Config) any {
			return cfg.MySQL
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.MySQL = before.MySQL
		})),
		valueRestartSpec("site_mysql", func(cfg config.Config) any {
			return cfg.SiteMySQL
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.SiteMySQL = before.SiteMySQL
		})),
		valueRestartSpec("redis", func(cfg config.Config) any {
			return cfg.Redis
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Redis = before.Redis
		})),
		newHotReloadRestartSpec("task.runtime", func(before, after config.Config) bool {
			return !reflect.DeepEqual(taskRuntimeConfigForRestartCheck(before.Task), taskRuntimeConfigForRestartCheck(after.Task))
		}, preserveTaskRuntimeConfig),
		newHotReloadRestartSpec("runtime_config.source", func(before, after config.Config) bool {
			return runtimeconfig.NormalizeSource(before.RuntimeConfig.Source) != runtimeconfig.NormalizeSource(after.RuntimeConfig.Source)
		}, preserveRuntimeConfigSource),
		valueRestartSpec("kafka", func(cfg config.Config) any {
			return cfg.Kafka
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Kafka = before.Kafka
		})),
		valueRestartSpec("observability", func(cfg config.Config) any {
			return cfg.Observability
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Observability = before.Observability
		})),
		valueRestartSpec("workflows.user_tag.enabled", func(cfg config.Config) any {
			return cfg.Workflows.UserTag.Enabled
		}, preserveBefore(func(effective *config.Config, before config.Config) {
			effective.Workflows.UserTag.Enabled = before.Workflows.UserTag.Enabled
		})),
	}
}

// newHotReloadRestartSpec 生成一个需重启配置边界规格。
func newHotReloadRestartSpec(reason string, changed hotReloadRestartChanged, preserve hotReloadRestartPreserve) hotReloadRestartSpec {
	return hotReloadRestartSpec{
		Reason:   reason,
		Changed:  changed,
		Preserve: preserve,
	}
}

// valueRestartSpec 生成使用 DeepEqual 比较的需重启配置边界。
func valueRestartSpec(reason string, value func(config.Config) any, preserve hotReloadRestartPreserve) hotReloadRestartSpec {
	return newHotReloadRestartSpec(reason, func(before, after config.Config) bool {
		return !reflect.DeepEqual(value(before), value(after))
	}, preserve)
}

// trimmedStringRestartSpec 生成忽略首尾空白的字符串需重启边界。
func trimmedStringRestartSpec(reason string, value func(config.Config) string, preserve hotReloadRestartPreserve) hotReloadRestartSpec {
	return newHotReloadRestartSpec(reason, func(before, after config.Config) bool {
		return strings.TrimSpace(value(before)) != strings.TrimSpace(value(after))
	}, preserve)
}

// preserveBefore 把只依赖旧配置的保留逻辑适配为通用保留函数。
func preserveBefore(preserve func(effective *config.Config, before config.Config)) hotReloadRestartPreserve {
	return func(effective *config.Config, before config.Config, _ config.Config) {
		preserve(effective, before)
	}
}

// preserveTaskRuntimeConfig 保留任务运行时配置，但允许周期任务列表在线刷新。
func preserveTaskRuntimeConfig(effective *config.Config, before config.Config, after config.Config) {
	periodic := append([]config.TaskPeriodicConfig(nil), after.Task.Periodic...)
	effective.Task = before.Task
	effective.Task.Periodic = periodic
}

// preserveRuntimeConfigSource 保留运行配置来源，但允许 DB 轮询间隔在线刷新。
func preserveRuntimeConfigSource(effective *config.Config, before config.Config, after config.Config) {
	pollInterval := after.RuntimeConfig.PollIntervalSeconds
	effective.RuntimeConfig = before.RuntimeConfig
	effective.RuntimeConfig.PollIntervalSeconds = pollInterval
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

// BuildReloadEffectiveConfig 生成本进程可以立即生效的配置快照。
// 只动态刷新运行期配置；基础设施和任务运行时变更记为待重启。
func BuildReloadEffectiveConfig(before, after config.Config) config.Config {
	effective := after
	for _, spec := range changedHotReloadRestartSpecs(before, after) {
		if spec.Preserve == nil {
			continue
		}
		spec.Preserve(&effective, before, after)
	}
	return effective
}

// DetectReloadRestartImpact 识别本次热加载中哪些配置虽然已加载，但仍需重启进程才能完全生效。
func DetectReloadRestartImpact(before, after config.Config) (bool, string) {
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
