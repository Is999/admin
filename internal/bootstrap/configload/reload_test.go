package configload

import (
	"strings"
	"testing"

	"admin/internal/config"

	"github.com/zeromicro/go-zero/rest"
)

// enabledPeriodicConfig 构造测试中显式启用的周期任务配置。
func enabledPeriodicConfig(item config.TaskPeriodicConfig) config.TaskPeriodicConfig {
	enabled := true
	item.Enabled = &enabled
	return item
}

// TestDetectReloadRestartImpact 确保基础设施配置变更会明确标记“需重启才能完全生效”。
func TestDetectReloadRestartImpact(t *testing.T) {
	before := config.Config{
		RestConf: rest.RestConf{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Redis: config.RedisConfig{
			Addrs: []string{"127.0.0.1:6379"},
		},
	}
	after := before
	after.Redis = config.RedisConfig{
		Addrs: []string{"127.0.0.1:6380"},
	}

	restartRequired, restartReason := DetectReloadRestartImpact(before, after)
	if !restartRequired {
		t.Fatal("期望 Redis 配置变更后标记为需重启生效，实际为 false")
	}
	if !strings.Contains(restartReason, "redis") {
		t.Fatalf("期望重启原因包含 redis，实际为 %q", restartReason)
	}

	after = before
	after.Workflows.UserTag.Enabled = true
	restartRequired, restartReason = DetectReloadRestartImpact(before, after)
	if !restartRequired {
		t.Fatal("期望用户标签插件启停变更后标记为需重启生效，实际为 false")
	}
	if !strings.Contains(restartReason, "workflows.user_tag.enabled") {
		t.Fatalf("期望重启原因包含 workflows.user_tag.enabled，实际为 %q", restartReason)
	}

	after = before
	after.Task.Periodic = []config.TaskPeriodicConfig{
		enabledPeriodicConfig(config.TaskPeriodicConfig{Name: "runtime-periodic", Workflow: "runtime.workflow"}),
	}
	restartRequired, restartReason = DetectReloadRestartImpact(before, after)
	if restartRequired {
		t.Fatalf("单独变更 task.periodic 不应要求重启，实际原因为 %q", restartReason)
	}
}

// TestHotReloadRestartSpecsValid 确保热加载重启边界规格完整且顺序稳定。
func TestHotReloadRestartSpecsValid(t *testing.T) {
	specs := hotReloadRestartSpecs()
	wantReasons := []string{
		"app_id",
		"snowflake",
		"user.route_shard_count",
		"run_mode",
		restartReasonHTTPServer,
		"mode",
		"mysql",
		"site_mysql",
		"redis",
		"task.runtime",
		"runtime_config.source",
		"kafka",
		"observability",
		"workflows.user_tag.enabled",
	}
	if len(specs) != len(wantReasons) {
		t.Fatalf("热加载重启边界数量不符合预期: got=%d want=%d", len(specs), len(wantReasons))
	}
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if spec.Reason != wantReasons[index] {
			t.Fatalf("热加载重启边界顺序不符合预期: index=%d got=%s want=%s", index, spec.Reason, wantReasons[index])
		}
		if spec.Changed == nil {
			t.Fatalf("热加载重启边界缺少变化判断: %s", spec.Reason)
		}
		if spec.Preserve == nil {
			t.Fatalf("热加载重启边界缺少原值保留逻辑: %s", spec.Reason)
		}
		if _, ok := seen[spec.Reason]; ok {
			t.Fatalf("热加载重启边界重复: %s", spec.Reason)
		}
		seen[spec.Reason] = struct{}{}
	}
}

// TestBuildReloadEffectiveConfigPreservesRestartOnlyFields 确保待重启字段保留原值，运行期字段仍可刷新。
func TestBuildReloadEffectiveConfigPreservesRestartOnlyFields(t *testing.T) {
	before := config.Config{
		RestConf: rest.RestConf{
			Host: "0.0.0.0",
			Port: 8080,
		},
		RunMode: 5,
		AppKey:  "old-key",
		Snowflake: config.SnowflakeConfig{
			WorkerID: int64Ptr(512),
		},
		User: config.UserConfig{
			RouteShardCount: 1,
			ExportSplitRows: 500000,
		},
		Redis: config.RedisConfig{
			Addrs: []string{"127.0.0.1:6379"},
		},
		Task: config.TaskQueueConfig{
			Enabled:      true,
			DefaultQueue: "old-queue",
			Periodic: []config.TaskPeriodicConfig{
				enabledPeriodicConfig(config.TaskPeriodicConfig{Name: "before-periodic", Workflow: "before.workflow"}),
			},
		},
		Workflows: config.WorkflowsConfig{
			UserTag: config.UserTagConfig{
				Enabled:           false,
				DefaultShardTotal: 8,
			},
		},
	}
	after := before
	after.RestConf.Port = 9090
	after.RunMode = 7
	after.AppKey = "new-key"
	after.Snowflake.WorkerID = int64Ptr(513)
	after.User.RouteShardCount = 2
	after.User.ExportSplitRows = 250000
	after.Redis = config.RedisConfig{Addrs: []string{"127.0.0.1:6380"}}
	after.Task.DefaultQueue = "new-queue"
	after.Task.Periodic = []config.TaskPeriodicConfig{
		enabledPeriodicConfig(config.TaskPeriodicConfig{Name: "new-periodic", Workflow: "new.workflow"}),
	}
	after.Workflows.UserTag.Enabled = true
	after.Workflows.UserTag.DefaultShardTotal = 16

	effective := BuildReloadEffectiveConfig(before, after)
	if effective.RestConf.Port != before.RestConf.Port || effective.RunMode != before.RunMode {
		t.Fatalf("期望 HTTP 与运行模式保持原值，实际 port=%d run_mode=%d", effective.RestConf.Port, effective.RunMode)
	}
	if effective.Redis.Addrs[0] != before.Redis.Addrs[0] {
		t.Fatalf("期望 Redis 保持原值，实际为 %+v", effective.Redis)
	}
	if effective.Snowflake.WorkerID == nil || *effective.Snowflake.WorkerID != *before.Snowflake.WorkerID {
		t.Fatalf("期望雪花 worker_id 保持原值，实际为 %+v", effective.Snowflake)
	}
	if effective.User.RouteShardCount != before.User.RouteShardCount {
		t.Fatalf("期望用户写入分表路由保持原值，实际为 %+v", effective.User)
	}
	if effective.User.ExportSplitRows != after.User.ExportSplitRows {
		t.Fatalf("期望用户导出拆分阈值刷新为新值，实际为 %+v", effective.User)
	}
	if effective.Task.DefaultQueue != before.Task.DefaultQueue {
		t.Fatalf("期望任务运行时配置保持原值，实际 default_queue=%s", effective.Task.DefaultQueue)
	}
	if len(effective.Task.Periodic) != 1 || effective.Task.Periodic[0].Name != "new-periodic" {
		t.Fatalf("期望周期任务列表刷新为新值，实际为 %+v", effective.Task.Periodic)
	}
	if effective.Workflows.UserTag.Enabled != before.Workflows.UserTag.Enabled {
		t.Fatalf("期望用户标签插件启停保持原值，实际为 %t", effective.Workflows.UserTag.Enabled)
	}
	if effective.Workflows.UserTag.DefaultShardTotal != after.Workflows.UserTag.DefaultShardTotal {
		t.Fatalf("期望用户标签运行参数刷新为新值，实际为 %d", effective.Workflows.UserTag.DefaultShardTotal)
	}
	if effective.AppKey != after.AppKey {
		t.Fatalf("期望普通运行期配置刷新为新值，实际 app_key=%s", effective.AppKey)
	}
}
