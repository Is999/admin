package taskqueue

import (
	"context"
	"encoding/json"
	"testing"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

// TestMultiSiteTaskIsolation 验证多站点共用同一个 Redis 时的任务隔离性。
// 核心目标是确保不同站点的任务 key、队列消费和工作流唯一键互不干扰。
// TestMultiSiteTaskIsolation 验证对应场景。
func TestMultiSiteTaskIsolation(t *testing.T) {
	server, client := newTestRedis(t)
	defer server.Close()
	defer client.Close()

	// 初始化两个站点的 Manager，共用同一个 Redis 实例。
	cfgA := config.TaskQueueConfig{
		Enabled: true,
		AppID:   "site_a_203",
		Queues:  map[string]int{QueueDefault: 1},
	}
	cfgB := config.TaskQueueConfig{
		Enabled: true,
		AppID:   "site_b_215",
		Queues:  map[string]int{QueueDefault: 1},
	}
	useRuntimeAppID(t, cfgA.AppID)
	mgrA := New(cfgA, client)
	useRuntimeAppID(t, cfgB.AppID)
	mgrB := New(cfgB, client)
	defer mgrA.Stop(context.Background())
	defer mgrB.Stop(context.Background())

	// 注册处理器，任务不会启动 worker，只用于通过注册校验。
	if err := mgrA.RegisterHandler("demo:isolation", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册站点 A 测试处理器失败: %v", err)
	}
	if err := mgrB.RegisterHandler("demo:isolation", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册站点 B 测试处理器失败: %v", err)
	}

	// 分别投递任务：站点 A 投递 2 条，站点 B 投递 1 条。
	useRuntimeAppID(t, cfgA.AppID)
	queueA := keys.TaskQueueName(QueueDefault)
	if _, err := mgrA.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{TaskType: "demo:isolation", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("站点 A 第 1 条任务投递失败: %v", err)
	}
	if _, err := mgrA.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{TaskType: "demo:isolation", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("站点 A 第 2 条任务投递失败: %v", err)
	}
	useRuntimeAppID(t, cfgB.AppID)
	queueB := keys.TaskQueueName(QueueDefault)
	if _, err := mgrB.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{TaskType: "demo:isolation", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("站点 B 任务投递失败: %v", err)
	}

	tasksA, err := mgrA.inspector.ListPendingTasks(queueA)
	if err != nil {
		t.Fatalf("读取站点 A 待执行任务失败: %v", err)
	}
	tasksB, err := mgrB.inspector.ListPendingTasks(queueB)
	if err != nil {
		t.Fatalf("读取站点 B 待执行任务失败: %v", err)
	}
	if len(tasksA) != 2 || len(tasksB) != 1 {
		t.Fatalf("多站点任务队列隔离失败，A=%d B=%d", len(tasksA), len(tasksB))
	}

	// 验证工作流唯一键不冲突：不同站点的相同业务、相同幂等键，不应该互相拦截。
	if err := mgrA.RegisterWorkflow(testWorkflowDefinition("wf.iso")); err != nil {
		t.Fatalf("站点 A 注册工作流失败: %v", err)
	}
	if err := mgrB.RegisterWorkflow(testWorkflowDefinition("wf.iso")); err != nil {
		t.Fatalf("站点 B 注册工作流失败: %v", err)
	}

	ttl := 60
	// 站点 A 触发 uniqueKey=same_daily_job。
	useRuntimeAppID(t, cfgA.AppID)
	_, errA1 := mgrA.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{Name: "wf.iso", UniqueKey: "same_daily_job", UniqueTTLSeconds: &ttl})
	if errA1 != nil {
		t.Fatalf("站点 A 首次触发工作流失败: %v", errA1)
	}

	// 站点 B 触发同名 uniqueKey，应成功且不与站点 A 冲突。
	useRuntimeAppID(t, cfgB.AppID)
	_, errB1 := mgrB.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{Name: "wf.iso", UniqueKey: "same_daily_job", UniqueTTLSeconds: &ttl})
	if errB1 != nil {
		t.Fatalf("站点 B 触发工作流失败，说明隔离失效: %v", errB1)
	}

	// 站点 A 再次触发同名 key，应被本站点自己的防重机制拦截。
	useRuntimeAppID(t, cfgA.AppID)
	_, errA2 := mgrA.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{Name: "wf.iso", UniqueKey: "same_daily_job", UniqueTTLSeconds: &ttl})
	if !errors.Is(errA2, ErrWorkflowAlreadyExists) {
		t.Fatalf("站点 A 应返回 ErrWorkflowAlreadyExists，实际为 %v", errA2)
	}
}
