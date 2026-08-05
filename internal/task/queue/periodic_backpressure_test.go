package taskqueue

import (
	"context"
	"testing"

	keys "admin/common/rediskeys"

	"github.com/hibiken/asynq"
)

// TestPeriodicQueueBackpressureStopsAtLimit 验证积压达到上限时不再继续投递周期任务。
func TestPeriodicQueueBackpressureStopsAtLimit(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()
	cfg := manager.CurrentConfig()
	cfg.Scheduler.MaxQueueBacklog = 2
	manager.UpdateConfig(cfg)
	queueName := manager.namespacedQueueName(QueueDefault)
	if err := manager.redis.RPush(context.Background(), keys.TaskAsynqPendingKey(queueName), "task-1", "task-2").Err(); err != nil {
		t.Fatalf("写入测试积压失败: %v", err)
	}
	periodic := &asynq.PeriodicTaskConfig{Task: asynq.NewTask(TypeWorkflowTrigger, mustJSONBytes(WorkflowTriggerPayload{Queue: QueueDefault}))}
	ok, gotQueue, backlog, limit, err := manager.periodicQueueBackpressureOK(context.Background(), periodic)
	if err != nil {
		t.Fatalf("检查周期任务积压失败: %v", err)
	}
	if ok || gotQueue != queueName || backlog != 2 || limit != 2 {
		t.Fatalf("达到上限时应停止投递: ok=%t queue=%s backlog=%d limit=%d", ok, gotQueue, backlog, limit)
	}
	if err = manager.redis.LPop(context.Background(), keys.TaskAsynqPendingKey(queueName)).Err(); err != nil {
		t.Fatalf("移除测试积压失败: %v", err)
	}
	ok, _, backlog, _, err = manager.periodicQueueBackpressureOK(context.Background(), periodic)
	if err != nil || !ok || backlog != 1 {
		t.Fatalf("低于上限时应允许投递: ok=%t backlog=%d error=%v", ok, backlog, err)
	}
}
