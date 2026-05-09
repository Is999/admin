package task

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/jobs/usertag/types"
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// TestReleaseUserTagWorkflowLeaseOnFinalFailureReleasesFullOwner 验证 full 节点终态失败后会精确释放当前 workflow owner。
func TestReleaseUserTagWorkflowLeaseOnFinalFailureReleasesFullOwner(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "215"}, svc.Dependencies{Rds: client})
	ctx := context.Background()
	leaseKey := keys.UserTagWorkflowLeaseRedisKey()
	if err := client.Set(ctx, leaseKey, "wf-full|full", time.Hour).Err(); err != nil {
		t.Fatalf("seed full lease failed: %v", err)
	}
	payload, err := json.Marshal(types.WorkflowPayload{WorkflowID: "wf-full", Mode: types.ModeFull})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	task := asynq.NewTask(TaskTypeUserTagEvaluateTags, payload)

	if err = releaseUserTagWorkflowLeaseOnFinalFailure(ctx, svcCtx, task, taskqueue.WorkflowTaskMeta{WorkflowName: WorkflowNameUserTagFull}); err != nil {
		t.Fatalf("release full lease on final failure failed: %v", err)
	}
	exists, err := client.Exists(ctx, leaseKey).Result()
	if err != nil {
		t.Fatalf("check lease exists failed: %v", err)
	}
	if exists != 0 {
		t.Fatalf("full lease should be released, exists=%d", exists)
	}
}

// TestReleaseUserTagWorkflowLeaseOnFinalFailureSkipsNonFull 验证 delta 终态失败不会释放正在运行的 full owner。
func TestReleaseUserTagWorkflowLeaseOnFinalFailureSkipsNonFull(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "215"}, svc.Dependencies{Rds: client})
	ctx := context.Background()
	leaseKey := keys.UserTagWorkflowLeaseRedisKey()
	if err := client.Set(ctx, leaseKey, "wf-full|full", time.Hour).Err(); err != nil {
		t.Fatalf("seed full lease failed: %v", err)
	}
	payload, err := json.Marshal(types.WorkflowPayload{WorkflowID: "wf-delta", Mode: types.ModeDelta})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	task := asynq.NewTask(TaskTypeUserTagCollectScope, payload)

	if err = releaseUserTagWorkflowLeaseOnFinalFailure(ctx, svcCtx, task, taskqueue.WorkflowTaskMeta{WorkflowName: WorkflowNameUserTagDelta}); err != nil {
		t.Fatalf("non-full final failure cleanup should be no-op: %v", err)
	}
	current, err := client.Get(ctx, leaseKey).Result()
	if err != nil {
		t.Fatalf("read lease owner failed: %v", err)
	}
	if current != "wf-full|full" {
		t.Fatalf("delta cleanup should keep full lease, got=%s", current)
	}
}

// TestUserTagStageHandlerSpecsCoverMainSkeletonNodes 验证插件注册清单覆盖主 DAG 骨架节点。
func TestUserTagStageHandlerSpecsCoverMainSkeletonNodes(t *testing.T) {
	specs := userTagStageHandlerSpecs()
	got := make(map[string]string, len(specs))
	for _, spec := range specs {
		got[spec.Node] = spec.TaskType
	}
	want := map[string]string{
		types.NodePrepare:        TaskTypeUserTagPrepare,
		types.NodeCollectScope:   TaskTypeUserTagCollectScope,
		types.NodeEvaluateTags:   TaskTypeUserTagEvaluateTags,
		types.NodeResolveChanges: TaskTypeUserTagResolveChanges,
		types.NodePersistResults: TaskTypeUserTagPersistResults,
		types.NodeFinalize:       TaskTypeUserTagFinalize,
		types.NodeDispatchHooks:  TaskTypeUserTagDispatchHooks,
	}
	for node, taskType := range want {
		if got[node] != taskType {
			t.Fatalf("node=%s taskType=%s want=%s specs=%+v", node, got[node], taskType, specs)
		}
	}
}

// TestUserTagEventOutboxRetryScanOptionsScansAllShards 验证异常扫描任务不会只锁定 0 号 outbox 分片。
func TestUserTagEventOutboxRetryScanOptionsScansAllShards(t *testing.T) {
	opts, err := userTagEventOutboxRetryScanOptions(Defaults{ShardTotal: 10, BatchSize: 100, EventBatchSize: 50, WorkerCount: 2, EventHookEnabled: true})
	if err != nil {
		t.Fatalf("userTagEventOutboxRetryScanOptions() error = %v", err)
	}
	if opts.ShardTotal != 1 {
		t.Fatalf("retry scan should run as single unsharded worker, ShardTotal=%d", opts.ShardTotal)
	}
	if !opts.EventHookEnabled || opts.BatchSize != 50 {
		t.Fatalf("unexpected retry scan options: %+v", opts)
	}
}
