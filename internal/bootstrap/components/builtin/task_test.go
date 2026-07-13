package builtin

import (
	"context"
	"strings"
	"testing"

	"admin/common/runtimecfg"
	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/runmode"
	"admin/internal/config"
	taskqueue "admin/internal/task/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestTaskRuntimeRollsBackWorkerWhenSchedulerFails 验证组合启动失败时不会遗留已运行的 Worker。
func TestTaskRuntimeRollsBackWorkerWhenSchedulerFails(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	previousRuntime := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "1"})
	defer runtimecfg.Restore(previousRuntime)

	enabled := true
	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "1",
		DefaultQueue: taskqueue.QueueDefault,
		Queues: map[string]int{
			taskqueue.QueueDefault: 1,
		},
		Scheduler: config.TaskQueueSchedulerConfig{
			Enabled:  true,
			LeaseKey: "app:other:task:scheduler:leader",
		},
		Periodic: []config.TaskPeriodicConfig{
			{
				Enabled:  &enabled,
				Name:     "rollback-worker",
				Cron:     "*/5 * * * *",
				Workflow: taskqueue.WorkflowNameCacheRefresh,
			},
		},
	}, client)
	if manager == nil {
		t.Fatal("任务管理器创建失败")
	}
	defer manager.Stop(context.Background())

	state := core.NewState(config.Config{}, runmode.Worker|runmode.Scheduler, nil, nil, nil, nil)
	if err := registerTaskRuntimeLifecycle(state, manager); err != nil {
		t.Fatalf("注册任务生命周期失败: %v", err)
	}
	hooks := core.Snapshot(state).StartHooks
	if len(hooks) != 1 {
		t.Fatalf("任务启动钩子数量错误: %d", len(hooks))
	}
	if err := hooks[0].Run(context.Background()); err == nil || !strings.Contains(err.Error(), "租约键无效") {
		t.Fatalf("调度器非法租约键应导致启动失败: %v", err)
	}
	if err := manager.Ready(context.Background(), true, false); err == nil || !strings.Contains(err.Error(), "Worker 未运行") {
		t.Fatalf("调度器启动失败后 Worker 必须已回滚: %v", err)
	}
}
