//go:build integration

package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// integrationTaskRedisAddrsEnv 指定任务报告真实 Redis 单机或 Cluster 节点地址。
	integrationTaskRedisAddrsEnv = "INTEGRATION_REDIS_ADDRS"
	// integrationTaskRedisUsernameEnv 指定真实 Redis ACL 用户名。
	integrationTaskRedisUsernameEnv = "INTEGRATION_REDIS_USERNAME"
	// integrationTaskRedisPasswordEnv 指定真实 Redis 密码。
	integrationTaskRedisPasswordEnv = "INTEGRATION_REDIS_PASSWORD"
)

// TestTaskReportOnRealRedis 验证真实 Redis 上的终态分页、取消传播和新增原子脚本。
func TestTaskReportOnRealRedis(t *testing.T) {
	client := openIntegrationTaskRedis(t)
	appID := fmt.Sprintf("task-report-integration-%d", time.Now().UnixNano())
	previous := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	t.Cleanup(func() { cleanupIntegrationTaskKeys(t, client, appID) })

	manager := New(config.TaskQueueConfig{
		Enabled:                 true,
		AppID:                   appID,
		DefaultQueue:            QueueDefault,
		DefaultRetry:            1,
		DefaultTimeoutSeconds:   5,
		TaskCheckSeconds:        1,
		DelayedTaskCheckSeconds: 1,
		Queues: map[string]int{
			QueueDefault: 1,
		},
	}, client)
	if manager == nil {
		t.Fatal("真实 Redis 任务管理器初始化失败")
	}
	if err := manager.RegisterHandler("integration:task-report", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册真实 Redis 测试任务失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := verifyIntegrationTaskScripts(ctx, manager); err != nil {
		t.Fatalf("验证任务原子脚本失败: %v", err)
	}
	windowStart := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	enqueued, err := manager.EnqueueRegisteredTask(ctx, &types.EnqueueTaskReq{
		TaskType: "integration:task-report",
		Payload:  json.RawMessage(`{"source":"integration"}`),
	})
	if err != nil {
		t.Fatalf("投递真实 Redis 测试任务失败: %v", err)
	}
	if err = manager.StartWorker(); err != nil {
		t.Fatalf("启动真实 Redis 测试 Worker 失败: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	internalQueue := manager.namespacedQueueName(QueueDefault)
	if cluster, ok := client.(*redis.ClusterClient); ok {
		verifyIntegrationTaskClusterSlots(t, ctx, cluster, internalQueue, enqueued.TaskID)
	}
	for {
		info, infoErr := manager.inspector.GetTaskInfo(internalQueue, enqueued.TaskID)
		if infoErr == nil && info.State == asynq.TaskStateCompleted {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("等待真实 Redis 测试任务完成超时: %v", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = manager.Stop(ctx); err != nil {
		t.Fatalf("停止真实 Redis 测试 Worker 失败: %v", err)
	}

	resp, usage, err := manager.ListReportTasks(ctx, &types.ListTaskItemsReq{
		Queue:     QueueDefault,
		State:     asynq.TaskStateCompleted.String(),
		StartTime: windowStart.Format(time.RFC3339),
		EndTime:   time.Now().Add(time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339),
	}, 0, taskReportListBatchSize, 8<<20)
	if err != nil {
		t.Fatalf("读取真实 Redis 日报终态页失败: %v", err)
	}
	if resp.Total != 1 || len(resp.Tasks) != 1 || resp.Tasks[0].ID != enqueued.TaskID || usage.ByteLimited {
		t.Fatalf("真实 Redis 日报终态页异常: resp=%+v usage=%+v", resp, usage)
	}

	canceledCtx, cancelRead := context.WithCancel(context.Background())
	cancelRead()
	if _, err = manager.listReportNativePage(canceledCtx, internalQueue, asynq.TaskStateCompleted.String(), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("真实 Redis 日报取消 error=%v, want context.Canceled", err)
	}
}

// verifyIntegrationTaskClusterSlots 验证日报原生 Lua 的状态 key 与任务 hash 位于同一 Redis slot。
func verifyIntegrationTaskClusterSlots(t *testing.T, ctx context.Context, cluster *redis.ClusterClient, queue, taskID string) {
	t.Helper()
	stateKey, err := keys.TaskAsynqStateZSetKey(queue, asynq.TaskStateCompleted.String())
	if err != nil {
		t.Fatalf("生成 Redis Cluster completed key 失败: %v", err)
	}
	taskKey := keys.TaskAsynqTaskHashKey(queue, taskID)
	stateSlot, err := cluster.Do(ctx, "CLUSTER", "KEYSLOT", stateKey).Int()
	if err != nil {
		t.Fatalf("读取 Redis Cluster completed slot 失败: %v", err)
	}
	taskSlot, err := cluster.Do(ctx, "CLUSTER", "KEYSLOT", taskKey).Int()
	if err != nil {
		t.Fatalf("读取 Redis Cluster task slot 失败: %v", err)
	}
	if stateSlot != taskSlot {
		t.Fatalf("日报原生 Lua 跨 slot: state=%d task=%d", stateSlot, taskSlot)
	}
}

// verifyIntegrationTaskScripts 验证 owner 保护和 attempt 保护脚本在真实 Redis 上生效。
func verifyIntegrationTaskScripts(ctx context.Context, manager *Manager) error {
	const (
		workflowName = "integration.workflow"
		uniqueKey    = "daily"
		owner        = "owner-a"
		taskID       = "runtime-script"
		attemptToken = "attempt-a"
	)
	locked, err := manager.ensureWorkflowUnique(ctx, workflowName, uniqueKey, owner, time.Minute)
	if err != nil {
		return errors.Wrap(err, "首个工作流 owner 未取得唯一键")
	}
	if !locked {
		return errors.New("首个工作流 owner 未取得唯一键")
	}
	locked, err = manager.ensureWorkflowUnique(ctx, workflowName, uniqueKey, "owner-b", time.Minute)
	if err != nil {
		return errors.Wrap(err, "校验第二个工作流 owner 失败")
	}
	if locked {
		return errors.New("第二个工作流 owner 不应覆盖唯一键")
	}
	queue := manager.namespacedQueueName(QueueDefault)
	runtimeKey := manager.taskRuntimeKey(queue, taskID)
	if err = manager.redis.HSet(ctx, runtimeKey, "attemptToken", attemptToken, "lastErr", "stale").Err(); err != nil {
		return errors.Wrap(err, "准备任务运行快照失败")
	}
	written, err := manager.finishTaskRuntime(ctx, queue, taskID, attemptToken, map[string]any{"status": "success"}, time.Minute)
	if err != nil {
		return errors.Wrap(err, "当前 attempt 未写入任务终态")
	}
	if !written {
		return errors.New("当前 attempt 未写入任务终态")
	}
	values, err := manager.redis.HGetAll(ctx, runtimeKey).Result()
	if err != nil {
		return errors.Wrap(err, "读取任务运行终态失败")
	}
	if values["status"] != "success" || values["lastErr"] != "" {
		return errors.Errorf("任务运行终态字段异常: %+v", values)
	}
	return nil
}

// openIntegrationTaskRedis 连接集成测试声明的真实 Redis 单机或 Cluster。
func openIntegrationTaskRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	rawAddrs := strings.Split(strings.TrimSpace(os.Getenv(integrationTaskRedisAddrsEnv)), ",")
	addrs := make([]string, 0, len(rawAddrs))
	for _, rawAddr := range rawAddrs {
		if addr := strings.TrimSpace(rawAddr); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) == 0 {
		t.Skipf("%s 未配置，跳过真实 Redis 集成测试", integrationTaskRedisAddrsEnv)
	}
	username := strings.TrimSpace(os.Getenv(integrationTaskRedisUsernameEnv))
	password := os.Getenv(integrationTaskRedisPasswordEnv)
	var client redis.UniversalClient
	if len(addrs) == 1 {
		client = redis.NewClient(&redis.Options{Addr: addrs[0], Username: username, Password: password, ContextTimeoutEnabled: true})
	} else {
		client = redis.NewClusterClient(&redis.ClusterOptions{Addrs: addrs, Username: username, Password: password, ContextTimeoutEnabled: true})
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("连接真实 Redis 失败 addrs=%v error=%v", addrs, err)
	}
	return client
}

// cleanupIntegrationTaskKeys 有界清理当前随机 app_id 产生的真实 Redis 测试数据。
func cleanupIntegrationTaskKeys(t *testing.T, client redis.UniversalClient, appID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.SRem(ctx, "asynq:queues", keys.TaskQueueName(QueueDefault)).Err(); err != nil {
		t.Errorf("清理真实 Redis 测试队列登记失败: %v", err)
	}
	pattern := "*" + appID + "*"
	cleanNode := func(ctx context.Context, node *redis.Client) error {
		var cursor uint64
		for scanned := 0; scanned < 1000; {
			scannedKeys, next, err := node.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			scanned += len(scannedKeys)
			if len(scannedKeys) > 0 {
				if err = node.Unlink(ctx, scannedKeys...).Err(); err != nil {
					return err
				}
			}
			cursor = next
			if cursor == 0 {
				return nil
			}
		}
		return errors.New("真实 Redis 测试 key 清理超过 1000 条上限")
	}
	if cluster, ok := client.(*redis.ClusterClient); ok {
		if err := cluster.ForEachMaster(ctx, cleanNode); err != nil {
			t.Errorf("清理真实 Redis Cluster 测试 key 失败: %v", err)
		}
		return
	}
	single, ok := client.(*redis.Client)
	if !ok {
		t.Errorf("不支持的真实 Redis 客户端类型 %T", client)
		return
	}
	if err := cleanNode(ctx, single); err != nil {
		t.Errorf("清理真实 Redis 测试 key 失败: %v", err)
	}
}
