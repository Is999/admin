package taskqueue

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/task/stats"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// enabledTaskPeriodicConfig 构造测试中显式启用的周期任务配置。
func enabledTaskPeriodicConfig(item config.TaskPeriodicConfig) config.TaskPeriodicConfig {
	enabled := true
	item.Enabled = &enabled
	return item
}

// TestTaskLogMessageIncludesTaskIdentity 验证任务生命周期日志正文带上任务身份字段。
func TestTaskLogMessageIncludesTaskIdentity(t *testing.T) {
	msg := taskLogMessage("任务 开始执行", &requestctx.Meta{
		TaskName:     "工作流触发:user_tag.delta.refresh",
		TaskType:     TypeWorkflowTrigger,
		TaskID:       "task-001",
		TaskQueue:    QueueMaintenance,
		WorkflowName: "user_tag.delta.refresh",
		WorkflowNode: "snapshot",
		WorkflowID:   "wf-001",
		Mode:         "delta",
		ShardIndex:   1,
		ShardTotal:   4,
		LatencyMS:    36,
	})
	for _, want := range []string{
		"任务 开始执行",
		"task_name=工作流触发:user_tag.delta.refresh",
		"task_type=workflow:trigger",
		"task_id=task-001",
		"queue=maintenance",
		"workflow=user_tag.delta.refresh",
		"node=snapshot",
		"workflow_id=wf-001",
		"mode=delta",
		"shard=1/4",
		"latency_ms=36",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("taskLogMessage() = %q, want contains %q", msg, want)
		}
	}
}

// TestNewReturnsNilWhenAppIDMissing 确保任务系统缺失 app_id 时失败闭合，不在运行时 panic。
func TestNewReturnsNilWhenAppIDMissing(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	manager := New(config.TaskQueueConfig{Enabled: true}, client)
	if manager != nil {
		t.Fatal("期望 app_id 缺失时任务管理器不启动")
	}
}

// TestTaskListTimeRangeUsesActivityTime 验证时间范围按任务活动时间过滤。
func TestTaskListTimeRangeUsesActivityTime(t *testing.T) {
	startedAt := time.Date(2026, 5, 23, 3, 15, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 23, 4, 30, 0, 0, time.UTC)
	nextProcessAt := time.Date(2026, 5, 23, 5, 30, 0, 0, time.UTC)
	rangeStart := time.Date(2026, 5, 23, 4, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 5, 23, 5, 0, 0, 0, time.UTC)
	timeRange, err := parseTaskListTimeRange(rangeStart.Format(time.RFC3339), rangeEnd.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("parseTaskListTimeRange() error = %v", err)
	}

	completedTask := types.TaskItem{
		State:       "completed",
		StartedAt:   startedAt.Format(time.RFC3339),
		CompletedAt: completedAt.Format(time.RFC3339),
	}
	if !timeRange.Contains(completedTask) {
		t.Fatalf("completed task should match by completedAt, item=%+v", completedTask)
	}

	scheduledTask := types.TaskItem{
		State:         "scheduled",
		StartedAt:     startedAt.Format(time.RFC3339),
		NextProcessAt: nextProcessAt.Format(time.RFC3339),
	}
	if timeRange.Contains(scheduledTask) {
		t.Fatalf("scheduled task should still match by nextProcessAt, item=%+v", scheduledTask)
	}
}

// TestRegisterWorkflowRejectsInvalidDAG 验证非法 DAG 定义会在注册阶段被拦截。
func TestRegisterWorkflowRejectsInvalidDAG(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	tests := []struct {
		name string              // name 表示测试场景名称。
		def  *WorkflowDefinition // def 表示测试字段。
	}{
		{
			name: "missing dependency",
			def: &WorkflowDefinition{
				Name: "missing-dep",
				Nodes: map[string]*WorkflowNodeDefinition{
					"root": {
						TaskType:     TypeWorkflowNoop,
						DependsOn:    []string{"missing"},
						BuildPayload: testNoopPayload,
					},
				},
			},
		},
		{
			name: "cycle",
			def: &WorkflowDefinition{
				Name: "cycle",
				Nodes: map[string]*WorkflowNodeDefinition{
					"a": {
						TaskType:     TypeWorkflowNoop,
						DependsOn:    []string{"b"},
						BuildPayload: testNoopPayload,
					},
					"b": {
						TaskType:     TypeWorkflowNoop,
						DependsOn:    []string{"a"},
						BuildPayload: testNoopPayload,
					},
				},
			},
		},
		{
			name: "no root",
			def: &WorkflowDefinition{
				Name: "no-root",
				Nodes: map[string]*WorkflowNodeDefinition{
					"a": {
						TaskType:     TypeWorkflowNoop,
						DependsOn:    []string{"b"},
						BuildPayload: testNoopPayload,
					},
					"b": {
						TaskType:     TypeWorkflowNoop,
						DependsOn:    []string{"a"},
						BuildPayload: testNoopPayload,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := manager.RegisterWorkflow(tt.def); err == nil {
				t.Fatalf("期望工作流 %s 返回定义校验错误", tt.name)
			}
		})
	}
}

// TestEnrichTaskContextSetsModeField 验证任务 payload 中的 mode 会进入统一日志链路字段。
func TestEnrichTaskContextSetsModeField(t *testing.T) {
	manager := &Manager{}
	payload := mustJSONBytes(map[string]any{
		"workflowId":   "wf-mode-1",
		"workflowName": "user_tag_delta",
		"workflowNode": "collect_scope",
		"mode":         "delta",
		"shardIndex":   2,
		"shardTotal":   8,
	})
	task := asynq.NewTask(TypeWorkflowNoop, payload)

	ctx := manager.enrichTaskContext(context.Background(), task)
	meta := requestctx.FromContext(ctx)
	if meta == nil {
		t.Fatal("期望任务上下文存在统一元数据")
	}
	if meta.Mode != "delta" {
		t.Fatalf("期望 mode=delta，实际为 %q", meta.Mode)
	}
	if meta.WorkflowNode != "collect_scope" || meta.ShardIndex != 2 || meta.ShardTotal != 8 {
		t.Fatalf("工作流链路字段不符合预期: %+v", meta)
	}
}

// TestRegisterWorkflowRejectsDuplicateName 验证 Manager 层不会静默覆盖同名工作流定义。
func TestRegisterWorkflowRejectsDuplicateName(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := testWorkflowDefinition("demo.duplicate.workflow")
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("首次注册工作流失败: %v", err)
	}
	if err := manager.RegisterWorkflow(def); err == nil {
		t.Fatal("期望重复注册工作流返回错误，实际为 nil")
	}
}

// TestRegisterHandlerRejectsDuplicatePattern 验证 Manager 层提前拦截重复处理器，避免 Asynq ServeMux panic。
func TestRegisterHandlerRejectsDuplicatePattern(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	handler := asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})
	if err := manager.RegisterHandler("demo:duplicate", handler); err != nil {
		t.Fatalf("首次注册处理器失败: %v", err)
	}
	if err := manager.RegisterHandler("demo:duplicate", handler); err == nil {
		t.Fatal("期望重复注册处理器返回错误，实际为 nil")
	}
}

// TestRegisterHandlerRecoversServeMuxPanic 验证底层 ServeMux 异常会被转成错误并回滚本地注册表。
func TestRegisterHandlerRecoversServeMuxPanic(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	handler := asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})
	manager.mux.Handle("demo:mux-only", handler)
	if err := manager.RegisterHandler("demo:mux-only", handler); err == nil {
		t.Fatal("期望 ServeMux 重复注册被转成错误，实际为 nil")
	}
	for _, item := range manager.ListRegisteredTaskTypes(context.Background()) {
		if item.TaskType == "demo:mux-only" {
			t.Fatalf("期望本地注册表已回滚，实际仍存在: %+v", item)
		}
	}
}

// TestRegistryDisplayUsesRequestLocale 验证前端注册表展示文案通过统一语言包按请求语言返回。
func TestRegistryDisplayUsesRequestLocale(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler(TypeCacheRefreshRequest, asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册测试任务处理器失败: %v", err)
	}
	if err := manager.RegisterWorkflow(&WorkflowDefinition{
		Name:         WorkflowNameCacheRefresh,
		Description:  "中文兜底说明",
		DefaultQueue: QueueMaintenance,
		Nodes: map[string]*WorkflowNodeDefinition{
			"refresh": {
				Name:     "refresh",
				TaskType: TypeCacheRefreshBatch,
				BuildPayload: func(WorkflowStartSpec, *WorkflowNodeDefinition, int, int) ([]byte, error) {
					return []byte(`{}`), nil
				},
			},
		},
	}); err != nil {
		t.Fatalf("注册测试工作流失败: %v", err)
	}

	ctx, _ := requestctx.New(context.Background())
	requestctx.SetLocale(ctx, i18n.LocaleENUS)

	var taskItem types.TaskTypeRegistryItem
	for _, item := range manager.ListRegisteredTaskTypes(ctx) {
		if item.TaskType == TypeCacheRefreshRequest {
			taskItem = item
			break
		}
	}
	if taskItem.Description != i18n.MessageByKey(i18n.MsgKeyTaskRegistryTypeCacheRefreshRequestDesc, i18n.LocaleENUS) {
		t.Fatalf("任务类型说明未按语言包返回: %+v", taskItem)
	}
	if strings.Contains(taskItem.Description, "缓存") || strings.Contains(taskItem.UsageHint, "适合") {
		t.Fatalf("任务类型注册表残留中文展示文案: %+v", taskItem)
	}

	var workflowItem types.WorkflowRegistryItem
	for _, item := range manager.ListRegisteredWorkflows(ctx) {
		if item.Name == WorkflowNameCacheRefresh {
			workflowItem = item
			break
		}
	}
	if workflowItem.Description != i18n.MessageByKey(i18n.MsgKeyTaskRegistryWorkflowCacheRefreshDesc, i18n.LocaleENUS) {
		t.Fatalf("工作流说明未按语言包返回: %+v", workflowItem)
	}
	if strings.Contains(workflowItem.Description, "中文") || strings.Contains(workflowItem.UsageHint, "适合") {
		t.Fatalf("工作流注册表残留中文展示文案: %+v", workflowItem)
	}
}

// TestPeriodicNextProcessAtForTaskItem 验证周期任务历史记录能按周期名称或工作流名称补齐下一次执行时间。
func TestPeriodicNextProcessAtForTaskItem(t *testing.T) {
	nextRuns := []periodicNextRun{
		{
			PeriodicName:  "daily-user-tag",
			WorkflowName:  "user_tag.full.rebuild",
			NextProcessAt: "2026-06-08T12:00:00Z",
		},
		{
			PeriodicName:  "archive-admin-log",
			WorkflowName:  "archive.run",
			NextProcessAt: "2026-06-08T13:00:00Z",
		},
	}

	item := types.TaskItem{
		Headers: map[string]string{
			HeaderPeriodicName: "daily-user-tag",
			headerTaskSource:   WorkflowSourcePeriodic,
		},
	}
	if got := periodicNextProcessAtForTaskItem(item, nextRuns); got != "2026-06-08T12:00:00Z" {
		t.Fatalf("期望按周期任务名称补齐下一次执行时间，实际=%q", got)
	}

	item = types.TaskItem{
		Headers: map[string]string{
			headerTaskSource:   WorkflowSourcePeriodic,
			headerWorkflowName: "archive.run",
		},
	}
	if got := periodicNextProcessAtForTaskItem(item, nextRuns); got != "2026-06-08T13:00:00Z" {
		t.Fatalf("期望按工作流名称兜底补齐下一次执行时间，实际=%q", got)
	}

	item = types.TaskItem{
		Payload: json.RawMessage(`{"source":"manual","workflowName":"archive.run"}`),
	}
	if got := periodicNextProcessAtForTaskItem(item, nextRuns); got != "" {
		t.Fatalf("非周期任务不应补齐下一次执行时间，实际=%q", got)
	}
}

// TestEnqueueWorkflowTriggerReservesUniqueBeforeEnqueue 验证触发前会先预占工作流唯一键。
func TestEnqueueWorkflowTriggerReservesUniqueBeforeEnqueue(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("demo.workflow")); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	req := &types.TriggerTaskWorkflowReq{
		Name:      "demo.workflow",
		UniqueKey: "same",
	}
	resp, err := manager.EnqueueWorkflowTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("首次投递工作流触发任务失败: %v", err)
	}
	if resp.WorkflowID == "" {
		t.Fatal("期望回执里包含工作流 ID")
	}
	if _, err := manager.EnqueueWorkflowTrigger(context.Background(), req); !errors.Is(err, ErrWorkflowAlreadyExists) {
		t.Fatalf("期望返回 ErrWorkflowAlreadyExists，实际为 %v", err)
	}
	storedID, err := manager.redis.Get(context.Background(), manager.workflowUniqueKey(req.Name, req.UniqueKey)).Result()
	if err != nil {
		t.Fatalf("读取唯一键预占结果失败: %v", err)
	}
	if storedID != resp.WorkflowID {
		t.Fatalf("预占工作流 ID 不符合预期，期望=%s 实际=%s", resp.WorkflowID, storedID)
	}
}

// TestEnqueueWorkflowTriggerExtendsUniqueReservationForDelayedTrigger 验证延迟触发会延长唯一键保留时间。
func TestEnqueueWorkflowTriggerExtendsUniqueReservationForDelayedTrigger(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("demo.workflow")); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	processIn := 3600
	req := &types.TriggerTaskWorkflowReq{
		Name:             "demo.workflow",
		UniqueKey:        "delayed",
		ProcessInSeconds: &processIn,
	}
	resp, err := manager.EnqueueWorkflowTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("投递延迟工作流触发任务失败: %v", err)
	}
	if resp.WorkflowID == "" {
		t.Fatal("期望回执中包含工作流 ID")
	}

	ttl, err := manager.redis.TTL(context.Background(), manager.workflowUniqueKey(req.Name, req.UniqueKey)).Result()
	if err != nil {
		t.Fatalf("读取唯一键预占 TTL 失败: %v", err)
	}
	if ttl < time.Duration(processIn)*time.Second {
		t.Fatalf("期望唯一键预占 TTL 覆盖延迟执行时间，实际为 %v", ttl)
	}
}

// TestManagerUpdateConfigRefreshesSnapshot 验证更新配置后会同步刷新运行期快照。
func TestManagerUpdateConfigRefreshesSnapshot(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "1",
		DefaultQueue: "maintenance",
		Concurrency:  23,
		Queues: map[string]int{
			"critical": 9,
			"default":  4,
		},
		Scheduler: config.TaskQueueSchedulerConfig{
			LeaseKey: "task:scheduler:new",
		},
		Periodic: []config.TaskPeriodicConfig{
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{Name: "demo", Cron: "*/5 * * * *", Workflow: "cache.refresh"}),
		},
	})

	if got := manager.CurrentConfig().DefaultQueue; got != "maintenance" {
		t.Fatalf("期望默认队列已更新，实际为 %q", got)
	}
	if got := manager.concurrency(); got != 23 {
		t.Fatalf("期望并发数更新为 23，实际为 %d", got)
	}
	if got := manager.schedulerLeaseKey(); got != "app:1:task:scheduler:new" {
		t.Fatalf("期望调度租约键已更新，实际为 %q", got)
	}
	if got := len(manager.allPeriodicTasks()); got != 1 {
		t.Fatalf("期望更新后只有 1 个周期任务，实际为 %d", got)
	}
}

// TestManagerNamespacesTaskArtifactsByAppID 验证任务队列、工作流键和锁会按 app_id 做命名空间隔离。
func TestManagerNamespacesTaskArtifactsByAppID(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	useRuntimeAppID(t, "215")
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "215",
		DefaultQueue: QueueDefault,
		Queues: map[string]int{
			QueueDefault:     3,
			QueueMaintenance: 1,
		},
		Scheduler: config.TaskQueueSchedulerConfig{
			LeaseKey: "task:scheduler:leader",
		},
	})

	if got := manager.namespacedQueueName(QueueMaintenance); got != "app:215:maintenance" {
		t.Fatalf("期望 maintenance 队列带站点命名空间，实际为 %q", got)
	}
	if got := manager.namespacedQueueName("app:102:maintenance"); got != "" {
		t.Fatalf("期望其它站点完整队列名失败闭合，实际为 %q", got)
	}
	if got := manager.displayQueueName("app:215:maintenance"); got != QueueMaintenance {
		t.Fatalf("期望展示逻辑队列名 maintenance，实际为 %q", got)
	}
	if got := manager.schedulerLeaseKey(); got != "app:215:task:scheduler:leader" {
		t.Fatalf("期望调度租约键带站点命名空间，实际为 %q", got)
	}
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "215",
		DefaultQueue: QueueDefault,
		Queues: map[string]int{
			QueueDefault:     3,
			QueueMaintenance: 1,
		},
		Scheduler: config.TaskQueueSchedulerConfig{
			LeaseKey: "app:102:task:scheduler:leader",
		},
	})
	if got := manager.schedulerLeaseKey(); got != "" {
		t.Fatalf("期望其它站点调度租约键失败闭合，实际为 %q", got)
	}
	if got := manager.workflowMetaKey("wf-1"); got != "app:215:task:workflow:wf-1:meta" {
		t.Fatalf("期望工作流元数据键带站点命名空间，实际为 %q", got)
	}
	if got := manager.workflowUniqueKey("user_tag.delta.refresh", "daily"); got != "app:215:task:workflow:unique:user_tag.delta.refresh:daily" {
		t.Fatalf("期望工作流唯一键带站点命名空间，实际为 %q", got)
	}
	weights := manager.queueWeights()
	if weights["app:215:default"] != 3 || weights["app:215:maintenance"] != 1 {
		t.Fatalf("期望队列权重按站点命名空间隔离，实际为 %+v", weights)
	}
}

// TestManagerFailsClosedOnRuntimeAppIDMismatch 确保任务实例不会在错误运行态下写入其它站点命名空间。
func TestManagerFailsClosedOnRuntimeAppIDMismatch(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	useRuntimeAppID(t, "2")
	if manager.IsEnabled() {
		t.Fatal("运行态 app_id 与任务配置不一致时任务系统应失败关闭")
	}
	if got := manager.namespacedQueueName(QueueDefault); got != "" {
		t.Fatalf("mismatched namespacedQueueName() = %q, want empty", got)
	}
	if got := manager.workflowMetaKey("wf-1"); got != "" {
		t.Fatalf("mismatched workflowMetaKey() = %q, want empty", got)
	}
	if err := manager.EnqueueTask(context.Background(), TypeWorkflowNoop, []byte(`{}`)); !errors.Is(err, ErrTaskQueueDisabled) {
		t.Fatalf("mismatched EnqueueTask() error = %v, want ErrTaskQueueDisabled", err)
	}
}

// TestVisibleServerQueuesFiltersByAppID 验证共享 Redis 下 worker 快照只展示当前站点队列。
func TestVisibleServerQueuesFiltersByAppID(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	useRuntimeAppID(t, "2")
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "2",
	})

	queues, visible := manager.visibleServerQueues(map[string]int{
		"app:1:critical":    6,
		"app:1:default":     3,
		"app:2:default":     3,
		"app:2:maintenance": 1,
	})
	if !visible {
		t.Fatal("期望包含当前站点队列的 worker 可见，实际为不可见")
	}
	if len(queues) != 2 || queues[QueueDefault] != 3 || queues[QueueMaintenance] != 1 {
		t.Fatalf("期望只返回当前站点逻辑队列，实际为 %+v", queues)
	}

	_, visible = manager.visibleServerQueues(map[string]int{
		"app:1:critical": 6,
		"app:1:default":  3,
	})
	if visible {
		t.Fatal("仅监听其它站点队列的 worker 不应在当前站点可见")
	}
}

// TestEnqueueWorkflowTriggerRejectsUnknownWorkflowBeforeEnqueue 验证未知工作流会在入队前直接被拒绝。
func TestEnqueueWorkflowTriggerRejectsUnknownWorkflowBeforeEnqueue(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	_, err := manager.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{Name: "missing.workflow"})
	if !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("期望返回 ErrWorkflowNotFound，实际为 %v", err)
	}
}

// TestEnqueueWorkflowTriggerRejectsConflictingScheduleOptions 验证 processAt 与 processInSeconds 不能同时设置。
func TestEnqueueWorkflowTriggerRejectsConflictingScheduleOptions(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("demo.workflow")); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	_, err := manager.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{
		Name:             "demo.workflow",
		ProcessAt:        time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		ProcessInSeconds: new(30),
	})
	if err == nil {
		t.Fatal("期望返回调度参数冲突错误，实际为 nil")
	}
}

// TestStartWorkflowAllowsReservedUniqueLock 验证已由当前实例预占的唯一键允许继续启动工作流。
func TestStartWorkflowAllowsReservedUniqueLock(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := testWorkflowDefinition("reserved-unique")
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}

	workflowID := "wf-reserved"
	if locked, err := manager.reserveWorkflowUnique(context.Background(), def.Name, "uniq", workflowID, time.Minute); err != nil || !locked {
		t.Fatalf("预占工作流唯一键失败: locked=%v err=%v", locked, err)
	}

	gotID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		WorkflowID: workflowID,
		Name:       def.Name,
		UniqueKey:  "uniq",
		UniqueTTL:  time.Minute,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	if gotID != workflowID {
		t.Fatalf("期望工作流 ID 为 %s，实际为 %s", workflowID, gotID)
	}
}

// TestStartWorkflowRebuildsIncompleteMetadata 验证工作流半初始化元数据会被清理并重建。
func TestStartWorkflowRebuildsIncompleteMetadata(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := testWorkflowDefinition("demo.incomplete.workflow")
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	workflowID := "wf-incomplete"
	ctx := context.Background()
	if err := manager.redis.HSet(ctx, manager.workflowMetaKey(workflowID),
		"workflowId", workflowID,
		"workflowName", def.Name,
		"status", WorkflowStatusPending,
	).Err(); err != nil {
		t.Fatalf("准备半初始化工作流 meta 失败: %v", err)
	}

	gotID, err := manager.StartWorkflow(ctx, WorkflowStartSpec{
		WorkflowID: workflowID,
		Name:       def.Name,
	})
	if err != nil {
		t.Fatalf("重建半初始化工作流失败: %v", err)
	}
	if gotID != workflowID {
		t.Fatalf("期望工作流 ID 为 %s，实际为 %s", workflowID, gotID)
	}
	complete, err := manager.workflowMetadataComplete(ctx, workflowID, def)
	if err != nil {
		t.Fatalf("校验重建后的工作流元数据失败: %v", err)
	}
	if !complete {
		t.Fatal("期望工作流元数据已完整重建")
	}
}

// TestWorkflowProgressionCompletesDAG 验证工作流节点按依赖推进后会正确进入成功终态。
func TestWorkflowProgressionCompletesDAG(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.progress",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				TaskType:     TypeWorkflowNoop,
				BuildPayload: testNoopPayload,
			},
			"final": {
				TaskType:     TypeWorkflowNoop,
				DependsOn:    []string{"root"},
				BuildPayload: testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}

	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{Name: def.Name})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	if err := manager.markTaskSuccess(context.Background(), WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "root",
	}); err != nil {
		t.Fatalf("标记根节点成功失败: %v", err)
	}
	if err := manager.markTaskSuccess(context.Background(), WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "final",
	}); err != nil {
		t.Fatalf("标记终节点成功失败: %v", err)
	}

	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusSuccess {
		t.Fatalf("期望工作流状态为 success，实际为 %s", meta.Status)
	}
	if len(nodes) != 2 {
		t.Fatalf("期望存在 2 个工作流节点，实际为 %d", len(nodes))
	}
}

// TestRecordTaskRuntimeFinishUsesDetachedContext 验证任务超时后仍能写入完成耗时快照。
func TestRecordTaskRuntimeFinishUsesDetachedContext(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	ctx, _ = requestctx.New(ctx)
	requestctx.SetTask(ctx, "task-timeout", "demo:timeout", QueueMaintenance)
	cancel()

	manager.recordTaskRuntimeFinish(ctx, "task-timeout", time.Now().Add(-time.Second), errors.New("boom"), nil)

	values, err := manager.redis.HGetAll(context.Background(), manager.taskRuntimeKey("task-timeout")).Result()
	if err != nil {
		t.Fatalf("读取任务耗时快照失败: %v", err)
	}
	if values["status"] != "failed" {
		t.Fatalf("期望超时后完成快照仍写入 failed，实际为 %q values=%+v", values["status"], values)
	}
	if values["startedAt"] == "" || toInt64(values["startedAtMs"]) <= 0 {
		t.Fatalf("期望完成快照兜底写入开始时间，实际 values=%+v", values)
	}
	if values["finishedAt"] == "" || toInt64(values["durationMs"]) <= 0 {
		t.Fatalf("期望完成时间和耗时已写入，实际 values=%+v", values)
	}
	if !strings.Contains(values["lastErr"], "boom") {
		t.Fatalf("期望失败原因写入 lastErr，实际 values=%+v", values)
	}
}

// TestMarkTaskFailureUsesDetachedContext 验证业务 ctx 已取消时仍能把工作流节点标记为失败。
func TestMarkTaskFailureUsesDetachedContext(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name:         "dag.detached.failure",
		DefaultQueue: QueueMaintenance,
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:         "root",
				TaskType:     TypeWorkflowNoop,
				BuildPayload: testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		Queue:      QueueMaintenance,
		ShardTotal: 1,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx, _ = requestctx.New(ctx)
	requestctx.SetWorkflow(ctx, workflowID, def.Name, "root", 0, 1)
	cancel()

	markErr := manager.markTaskFailureWithFinalContext(ctx, WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "root",
		ShardIndex:   0,
		ShardTotal:   1,
	}, errors.New("boom"))
	if markErr != nil {
		t.Fatalf("超时 ctx 下标记工作流失败失败: %v", markErr)
	}

	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusFailed || !strings.Contains(meta.ErrorMessage, "boom") {
		t.Fatalf("期望工作流失败状态已写入，实际 meta=%+v", meta)
	}
	if len(nodes) != 1 || nodes[0].Status != NodeStatusFailed || nodes[0].Failed != 1 || !strings.Contains(nodes[0].ErrorMessage, "boom") {
		t.Fatalf("期望节点失败状态已写入，实际 nodes=%+v", nodes)
	}
}

// TestEffectiveRetryNodeCanBlockWorkflowRetryOverride 验证非幂等节点可以禁止全局重试覆盖。
func TestEffectiveRetryNodeCanBlockWorkflowRetryOverride(t *testing.T) {
	got := effectiveRetry(&WorkflowNodeDefinition{MaxRetry: -1}, WorkflowStartSpec{RetryOverride: new(3)})
	if got != 0 {
		t.Fatalf("期望非幂等节点强制关闭重试，实际为 %d", got)
	}
}

// TestWorkflowSpecByIDRestoresSchedulingOverrides 验证工作流推进后仍能继承触发时的重试和超时覆盖。
func TestWorkflowSpecByIDRestoresSchedulingOverrides(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := testWorkflowDefinition("demo.restore.overrides")
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	retry := 0
	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:            def.Name,
		PeriodicName:    "daily-demo",
		RetryOverride:   &retry,
		TimeoutOverride: 37 * time.Second,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}

	got, err := manager.workflowSpecByID(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("还原工作流启动参数失败: %v", err)
	}
	if got.RetryOverride == nil || *got.RetryOverride != retry {
		t.Fatalf("期望保留 retryOverride=%d，实际为 %#v", retry, got.RetryOverride)
	}
	if got.TimeoutOverride != 37*time.Second {
		t.Fatalf("期望保留 timeoutOverride=37s，实际为 %v", got.TimeoutOverride)
	}
	if got.PeriodicName != "daily-demo" {
		t.Fatalf("期望保留周期任务名称，实际为 %q", got.PeriodicName)
	}
}

// TestWorkflowShardedSuccessDedupesDuplicateShardAck 验证同一分片重复回写成功时不会把节点成功数重复累加。
func TestWorkflowShardedSuccessDedupesDuplicateShardAck(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.sharded.dedupe",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				BuildPayload:     testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}

	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		ShardTotal: 2,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}

	successMeta := WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "root",
		ShardTotal:   2,
	}
	if err := manager.markTaskSuccess(context.Background(), successMeta); err != nil {
		t.Fatalf("首次标记分片 0 成功失败: %v", err)
	}
	nodeDone, err := manager.redis.HGet(context.Background(), manager.workflowNodeKey(workflowID, "root"), workflowNodeInstanceField(0)).Result()
	if err != nil {
		t.Fatalf("读取节点 hash 内分片去重字段失败: %v", err)
	}
	if nodeDone != "succeeded" {
		t.Fatalf("期望分片终态标记记录成功结果，实际为 %s", nodeDone)
	}
	instanceDoneKeys, err := manager.redis.Exists(context.Background(), keys.TaskWorkflowNodeInstanceKey(workflowID, "root", 0)).Result()
	if err != nil {
		t.Fatalf("读取分片实例散列 key 失败: %v", err)
	}
	if instanceDoneKeys != 0 {
		t.Fatalf("期望不再创建分片实例散列 key，实际数量为 %d", instanceDoneKeys)
	}
	if err := manager.markTaskSuccess(context.Background(), successMeta); err != nil {
		t.Fatalf("重复标记分片 0 成功失败: %v", err)
	}

	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusRunning {
		t.Fatalf("期望重复回写后工作流仍处于 running，实际为 %s", meta.Status)
	}
	if len(nodes) != 1 || nodes[0].Succeeded != 1 {
		t.Fatalf("期望重复回写不重复累加成功数，实际节点状态为 %+v", nodes)
	}

	successMeta.ShardIndex = 1
	if err := manager.markTaskSuccess(context.Background(), successMeta); err != nil {
		t.Fatalf("标记分片 1 成功失败: %v", err)
	}

	meta, nodes, err = manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流最终状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusSuccess {
		t.Fatalf("期望工作流最终成功，实际为 %s", meta.Status)
	}
	if len(nodes) != 1 || nodes[0].Succeeded != 2 || nodes[0].Status != NodeStatusSuccess {
		t.Fatalf("期望节点按 2 个分片成功完成，实际为 %+v", nodes)
	}
}

// TestWorkflowShardedStatsVisibleInStatus 验证分片处理量会写入工作流状态并按节点聚合。
func TestWorkflowShardedStatsVisibleInStatus(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.sharded.stats.status",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				BuildPayload:     testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}

	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		ShardTotal: 2,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}

	shardSnapshots := []*taskstats.Snapshot{
		{
			Name:       "root.0",
			TotalCount: 3,
			ReadCount:  3,
			Details: []taskstats.Detail{
				{Action: taskstats.ActionRead, Name: "source.rows", Count: 3, Times: 1},
			},
		},
		{
			Name:        "root.1",
			TotalCount:  5,
			UpdateCount: 5,
			Details: []taskstats.Detail{
				{Action: taskstats.ActionUpdate, Name: "target.rows", Count: 5, Times: 1},
			},
		},
	}
	for shard, snapshot := range shardSnapshots {
		meta := WorkflowTaskMeta{
			WorkflowID:   workflowID,
			WorkflowName: def.Name,
			WorkflowNode: "root",
			ShardIndex:   shard,
			ShardTotal:   2,
		}
		if err := manager.recordWorkflowTaskStats(context.Background(), meta, snapshot); err != nil {
			t.Fatalf("记录分片 %d 处理量失败: %v", shard, err)
		}
		if err := manager.markTaskSuccess(context.Background(), meta); err != nil {
			t.Fatalf("标记分片 %d 成功失败: %v", shard, err)
		}
	}

	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusSuccess {
		t.Fatalf("期望工作流最终成功，实际为 %s", meta.Status)
	}
	if len(nodes) != 1 {
		t.Fatalf("期望只有 root 节点，实际为 %+v", nodes)
	}
	node := nodes[0]
	if node.Progress == nil || node.Progress.Total != 2 || node.Progress.Succeeded != 2 || node.Progress.Percent != 100 {
		t.Fatalf("期望节点进度按 2 个成功分片聚合，实际为 %+v", node.Progress)
	}
	if node.ExecutionTrace == nil || node.ExecutionTrace.TotalCount != 8 || node.ExecutionTrace.ReadCount != 3 || node.ExecutionTrace.UpdateCount != 5 {
		t.Fatalf("期望节点处理量按分片聚合，实际为 %+v", node.ExecutionTrace)
	}
	if len(node.ShardTraces) != 2 {
		t.Fatalf("期望返回 2 个分片处理量明细，实际为 %+v", node.ShardTraces)
	}
	for shard, trace := range node.ShardTraces {
		if trace.ShardIndex != shard || trace.ShardTotal != 2 || trace.Status != NodeStatusSuccess {
			t.Fatalf("期望分片 %d 终态和索引正确，实际为 %+v", shard, trace)
		}
		if trace.Progress == nil || trace.Progress.Succeeded != 1 || trace.Progress.Percent != 100 {
			t.Fatalf("期望分片 %d 进度成功完成，实际为 %+v", shard, trace.Progress)
		}
		if trace.ExecutionTrace == nil || trace.ExecutionTrace.TotalCount != shardSnapshots[shard].TotalCount {
			t.Fatalf("期望分片 %d 保留原始处理量，实际为 %+v", shard, trace.ExecutionTrace)
		}
	}
}

// TestWorkflowShardFailureThenSuccessRepairsCounters 验证同一分片失败后成功回写会纠正失败计数并推进工作流。
func TestWorkflowShardFailureThenSuccessRepairsCounters(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.sharded.failure.success.repair",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				BuildPayload:     testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		ShardTotal: 1,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "root",
		ShardIndex:   0,
		ShardTotal:   1,
	}
	failureRecorded, err := manager.markTaskFailure(context.Background(), meta, errors.New("first failed"))
	if err != nil {
		t.Fatalf("标记分片失败失败: %v", err)
	}
	if !failureRecorded {
		t.Fatal("首次分片失败应记录终态")
	}
	if err := manager.markTaskSuccess(context.Background(), meta); err != nil {
		t.Fatalf("失败后标记分片成功失败: %v", err)
	}
	wf, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if wf.Status != WorkflowStatusSuccess || len(nodes) != 1 || nodes[0].Status != NodeStatusSuccess || nodes[0].Succeeded != 1 || nodes[0].Failed != 0 {
		t.Fatalf("期望失败计数被成功回写纠正，实际 wf=%+v nodes=%+v", wf, nodes)
	}
}

// TestWorkflowShardSuccessIgnoresLateFailureAck 验证成功分片遇到迟到失败回执时不会把节点和工作流回滚为失败。
func TestWorkflowShardSuccessIgnoresLateFailureAck(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.sharded.success.late.failure",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				BuildPayload:     testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		ShardTotal: 1,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   workflowID,
		WorkflowName: def.Name,
		WorkflowNode: "root",
		ShardIndex:   0,
		ShardTotal:   1,
	}
	if err := manager.markTaskSuccess(context.Background(), meta); err != nil {
		t.Fatalf("标记分片成功失败: %v", err)
	}
	failureRecorded, err := manager.markTaskFailure(context.Background(), meta, errors.New("late failed"))
	if err != nil {
		t.Fatalf("迟到失败回写不应返回错误: %v", err)
	}
	if failureRecorded {
		t.Fatal("成功后的迟到失败不应重复记录终态")
	}
	wf, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if wf.Status != WorkflowStatusSuccess || len(nodes) != 1 || nodes[0].Status != NodeStatusSuccess || nodes[0].Succeeded != 1 || nodes[0].Failed != 0 {
		t.Fatalf("期望迟到失败不回滚成功终态，实际 wf=%+v nodes=%+v", wf, nodes)
	}
}

// TestWorkflowArchivedRerunRepairsMultipleFailedShards 验证多个归档失败分片可逐个修复。
func TestWorkflowArchivedRerunRepairsMultipleFailedShards(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.sharded.rerun.repair",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				BuildPayload:     testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		Name:       def.Name,
		ShardTotal: 2,
	})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	for shard := 0; shard < 2; shard++ {
		failureRecorded, err := manager.markTaskFailure(context.Background(), WorkflowTaskMeta{
			WorkflowID:   workflowID,
			WorkflowName: def.Name,
			WorkflowNode: "root",
			ShardIndex:   shard,
			ShardTotal:   2,
		}, errors.Errorf("boom shard=%d", shard))
		if err != nil {
			t.Fatalf("标记分片 %d 失败失败: %v", shard, err)
		}
		if !failureRecorded {
			t.Fatalf("首次标记分片 %d 失败应记录终态", shard)
		}
	}
	for shard := 0; shard < 2; shard++ {
		payload, err := testNoopPayload(WorkflowStartSpec{WorkflowID: workflowID, Name: def.Name}, def.Nodes["root"], shard, 2)
		if err != nil {
			t.Fatalf("构造分片 %d 负载失败: %v", shard, err)
		}
		info := &asynq.TaskInfo{
			Payload: payload,
			Headers: map[string]string{
				headerWorkflowID:   workflowID,
				headerWorkflowName: def.Name,
				headerWorkflowNode: "root",
				headerShardIndex:   strconv.Itoa(shard),
				headerShardTotal:   "2",
			},
		}
		if err := manager.prepareWorkflowArchivedTaskRerun(context.Background(), info); err != nil {
			t.Fatalf("修复分片 %d 归档重跑状态失败: %v", shard, err)
		}
	}
	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取修复后工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusRunning {
		t.Fatalf("期望工作流恢复 running，实际 meta=%+v", meta)
	}
	if len(nodes) != 1 || nodes[0].Failed != 0 || nodes[0].Status != NodeStatusRunning {
		t.Fatalf("期望所有失败分片计数被撤销，实际 nodes=%+v", nodes)
	}
	for shard := 0; shard < 2; shard++ {
		if err := manager.markTaskSuccess(context.Background(), WorkflowTaskMeta{
			WorkflowID:   workflowID,
			WorkflowName: def.Name,
			WorkflowNode: "root",
			ShardIndex:   shard,
			ShardTotal:   2,
		}); err != nil {
			t.Fatalf("重跑成功回写分片 %d 失败: %v", shard, err)
		}
	}
	meta, nodes, err = manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取重跑完成工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusSuccess || len(nodes) != 1 || nodes[0].Status != NodeStatusSuccess || nodes[0].Succeeded != 2 || nodes[0].Failed != 0 {
		t.Fatalf("期望多失败分片重跑后推进成功，实际 meta=%+v nodes=%+v", meta, nodes)
	}
}

// TestWorkflowGraySkipRootStillSchedulesChild 验证根节点因灰度全跳过时，DAG 仍会继续推进下游节点。
func TestWorkflowGraySkipRootStillSchedulesChild(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	def := &WorkflowDefinition{
		Name: "dag.gray.skip",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:             "root",
				TaskType:         TypeWorkflowNoop,
				SupportsSharding: true,
				SupportsGray:     true,
				BuildPayload:     testNoopPayload,
			},
			"child": {
				Name:         "child",
				TaskType:     TypeWorkflowNoop,
				DependsOn:    []string{"root"},
				BuildPayload: testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}

	workflowID := findWorkflowIDWithoutGrayShard(t, "root", 4, 1)
	gotID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{
		WorkflowID:  workflowID,
		Name:        def.Name,
		ShardTotal:  4,
		GrayPercent: 1,
	})
	if err != nil {
		t.Fatalf("启动灰度工作流失败: %v", err)
	}
	if gotID != workflowID {
		t.Fatalf("期望工作流 ID 保持不变，实际为 %s", gotID)
	}

	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusRunning {
		t.Fatalf("期望工作流仍在等待子节点执行，实际为 %s", meta.Status)
	}
	if len(nodes) != 2 {
		t.Fatalf("期望存在 2 个节点状态，实际为 %d", len(nodes))
	}
	if nodes[0].Name != "child" || nodes[0].Status != NodeStatusRunning || nodes[0].Expected != 1 {
		t.Fatalf("期望 child 节点已被推进执行，实际为 %+v", nodes[0])
	}
	if nodes[1].Name != "root" || nodes[1].Status != NodeStatusSkipped || nodes[1].Skipped != 4 {
		t.Fatalf("期望 root 节点按灰度整体跳过，实际为 %+v", nodes[1])
	}
}

// TestTaskRetryMovesTaskToRetryQueue 验证任务失败重试后会进入重试队列。
func TestTaskRetryMovesTaskToRetryQueue(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	var called atomic.Int32
	if err := manager.RegisterHandler("demo:retry", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		called.Add(1)
		return errors.New("boom")
	})); err != nil {
		t.Fatalf("注册重试测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	if err := manager.EnqueueTask(context.Background(), "demo:retry", []byte(`{}`), svc.WithTaskRetry(1)); err != nil {
		t.Fatalf("投递任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		return called.Load() >= 1
	})
	waitForCondition(t, 5*time.Second, func() bool {
		info, err := manager.inspector.GetQueueInfo(manager.namespacedQueueName(QueueDefault))
		return err == nil && info.Retry >= 1
	})
}

// TestTaskRetryDoesNotSetTaskHashTTL 验证仍会重试的失败任务不会被项目层设置 task hash TTL。
func TestTaskRetryDoesNotSetTaskHashTTL(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:retry-ttl", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return errors.New("retry later")
	})); err != nil {
		t.Fatalf("注册重试 task hash 测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:retry-ttl",
		Payload:  json.RawMessage(`{"demo":true}`),
	})
	if err != nil {
		t.Fatalf("投递重试 task hash 测试任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetTaskInfo(manager.namespacedQueueName(QueueDefault), resp.TaskID)
		return infoErr == nil && info.State == asynq.TaskStateRetry
	})

	ttl := manager.redis.TTL(context.Background(), keys.TaskAsynqTaskHashKey(manager.namespacedQueueName(QueueDefault), resp.TaskID)).Val()
	if ttl != -1 {
		t.Fatalf("重试任务不应设置项目 task hash TTL，实际 TTL=%s", ttl)
	}
}

// TestFinalFailureHookOnlyRunsForArchivedTask 验证中间重试失败不触发通知，进入 archived 终态后才触发通知钩子。
func TestFinalFailureHookOnlyRunsForArchivedTask(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	var retryCalled atomic.Int32
	var hookCalls atomic.Int32
	type hookSnapshot struct {
		taskType   string // taskType 表示测试字段。
		workflowID string // workflowID 表示测试字段。
		mode       string // mode 表示测试字段。
		errText    string // errText 表示测试字段。
	}
	hookCh := make(chan hookSnapshot, 2)
	if err := manager.RegisterFinalFailureHook(func(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error) error {
		hookCalls.Add(1)
		snapshot := hookSnapshot{
			workflowID: strings.TrimSpace(meta.WorkflowID),
		}
		if task != nil {
			snapshot.taskType = task.Type()
		}
		if ctxMeta := requestctx.FromContext(ctx); ctxMeta != nil {
			snapshot.mode = strings.TrimSpace(ctxMeta.Mode)
		}
		if runErr != nil {
			snapshot.errText = runErr.Error()
		}
		hookCh <- snapshot
		return nil
	}); err != nil {
		t.Fatalf("注册终态失败钩子失败: %v", err)
	}
	if err := manager.RegisterHandler("demo:retry-hook", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		retryCalled.Add(1)
		return errors.New("retry later")
	})); err != nil {
		t.Fatalf("注册重试钩子测试处理器失败: %v", err)
	}
	if err := manager.RegisterHandler("demo:archive-hook", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return errors.Wrap(asynq.SkipRetry, "archive now")
	})); err != nil {
		t.Fatalf("注册归档钩子测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	retry := 1
	if _, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:retry-hook",
		Retry:    &retry,
		Payload:  json.RawMessage(`{"demo":true}`),
	}); err != nil {
		t.Fatalf("投递重试钩子测试任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetQueueInfo(manager.namespacedQueueName(QueueDefault))
		return retryCalled.Load() >= 1 && infoErr == nil && info.Retry >= 1
	})
	if got := hookCalls.Load(); got != 0 {
		t.Fatalf("重试中的失败不应触发终态失败钩子，实际触发 %d 次", got)
	}

	if _, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:archive-hook",
		Payload: json.RawMessage(`{
			"workflowId":"wf-hook",
			"workflowName":"workflow.demo",
			"workflowNode":"root",
			"mode":"full",
			"shardIndex":1,
			"shardTotal":2
		}`),
	}); err != nil {
		t.Fatalf("投递归档钩子测试任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		return hookCalls.Load() == 1
	})

	got := <-hookCh
	if got.taskType != "demo:archive-hook" || got.workflowID != "wf-hook" || got.mode != "full" || !strings.Contains(got.errText, asynq.SkipRetry.Error()) {
		t.Fatalf("终态失败钩子收到的任务上下文不符合预期: %+v", got)
	}
}

// TestWorkflowMarkSuccessFailureDoesNotSetTaskHashTTL 验证工作流成功回写失败时不会设置项目 task hash TTL。
func TestWorkflowMarkSuccessFailureDoesNotSetTaskHashTTL(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := manager.CurrentConfig()
	cfg.CompletedRetentionSeconds = 60
	manager.UpdateConfig(cfg)

	if err := manager.RegisterHandler("demo:workflow-mark-success-fail", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册工作流成功回写失败测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:workflow-mark-success-fail",
		Payload: json.RawMessage(`{
			"workflowId":"wf-mark-success-fail",
			"workflowName":"missing.workflow",
			"workflowNode":"root",
			"shardTotal":1
		}`),
	})
	if err != nil {
		t.Fatalf("投递工作流成功回写失败测试任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetTaskInfo(manager.namespacedQueueName(QueueDefault), resp.TaskID)
		return infoErr == nil && info.State == asynq.TaskStateRetry
	})

	ttl := manager.redis.TTL(context.Background(), keys.TaskAsynqTaskHashKey(manager.namespacedQueueName(QueueDefault), resp.TaskID)).Val()
	if ttl != -1 {
		t.Fatalf("成功回写失败的重试任务不应设置项目 task hash TTL，实际 TTL=%s", ttl)
	}
}

// TestArchivedTaskKeepsRuntimeSnapshotTTL 验证 archived 任务只保留业务运行快照 TTL，不干预 Asynq task hash。
func TestArchivedTaskKeepsRuntimeSnapshotTTL(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := manager.CurrentConfig()
	cfg.CompletedRetentionSeconds = 60
	cfg.ArchivedRetentionSeconds = 120
	manager.UpdateConfig(cfg)

	if err := manager.RegisterHandler("demo:archive-ttl", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return asynq.SkipRetry
	})); err != nil {
		t.Fatalf("注册归档运行快照测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:archive-ttl",
		Payload:  json.RawMessage(`{"demo":true}`),
	})
	if err != nil {
		t.Fatalf("投递归档运行快照测试任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetTaskInfo(manager.namespacedQueueName(QueueDefault), resp.TaskID)
		return infoErr == nil && info.State == asynq.TaskStateArchived
	})
	item, err := manager.GetTaskInfo(context.Background(), &types.GetTaskInfoReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	})
	if err != nil {
		t.Fatalf("查询归档任务详情失败: %v", err)
	}
	if item.StartedAt == "" || item.DurationMS <= 0 {
		t.Fatalf("归档任务应保留开始时间和耗时，实际 item=%+v", item)
	}

	ttl := manager.redis.TTL(context.Background(), keys.TaskAsynqTaskHashKey(manager.namespacedQueueName(QueueDefault), resp.TaskID)).Val()
	if ttl != -1 {
		t.Fatalf("归档终态任务不应设置项目 task hash TTL，实际 TTL=%s", ttl)
	}
	runtimeTTL := manager.redis.TTL(context.Background(), manager.taskRuntimeKey(resp.TaskID)).Val()
	expectedRuntimeTTL := manager.archivedRetention() + time.Hour
	if runtimeTTL <= manager.archivedRetention() || runtimeTTL > expectedRuntimeTTL+time.Second {
		t.Fatalf("归档终态任务 runtime TTL = %s，期望约为 %s", runtimeTTL, expectedRuntimeTTL)
	}
	if err = manager.Stop(context.Background()); err != nil {
		t.Fatalf("停止 worker 失败: %v", err)
	}
	if err = manager.RunTask(context.Background(), &types.OperateTaskReq{Queue: QueueDefault, TaskID: resp.TaskID}); err != nil {
		t.Fatalf("立即执行归档任务失败: %v", err)
	}
	ttl = manager.redis.TTL(context.Background(), keys.TaskAsynqTaskHashKey(manager.namespacedQueueName(QueueDefault), resp.TaskID)).Val()
	if ttl != -1 {
		t.Fatalf("归档任务重跑后仍不应设置项目 task hash TTL，实际 TTL=%s", ttl)
	}
}

// TestRunTaskRepairsArchivedWorkflowFailureState 验证归档失败节点重跑后 DAG 可继续。
func TestRunTaskRepairsArchivedWorkflowFailureState(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	var rootCalls atomic.Int32
	var childCalls atomic.Int32
	if err := manager.RegisterHandler("demo:rerun-root", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		if rootCalls.Add(1) == 1 {
			return errors.New("root boom")
		}
		return nil
	})); err != nil {
		t.Fatalf("注册根节点处理器失败: %v", err)
	}
	if err := manager.RegisterHandler("demo:rerun-child", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		childCalls.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("注册下游节点处理器失败: %v", err)
	}
	def := &WorkflowDefinition{
		Name: "demo.archived.rerun",
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				Name:         "root",
				TaskType:     "demo:rerun-root",
				BuildPayload: testNoopPayload,
			},
			"child": {
				Name:         "child",
				TaskType:     "demo:rerun-child",
				DependsOn:    []string{"root"},
				BuildPayload: testNoopPayload,
			},
		},
	}
	if err := manager.RegisterWorkflow(def); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	workflowID, err := manager.StartWorkflow(context.Background(), WorkflowStartSpec{Name: def.Name})
	if err != nil {
		t.Fatalf("启动工作流失败: %v", err)
	}
	var archivedTaskID string
	waitForCondition(t, 5*time.Second, func() bool {
		resp, listErr := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
			Queue:    QueueDefault,
			State:    "archived",
			Page:     1,
			PageSize: 10,
		})
		if listErr != nil || resp == nil || len(resp.Tasks) == 0 {
			return false
		}
		for _, item := range resp.Tasks {
			if item.WorkflowID == workflowID && item.TaskType == "demo:rerun-root" {
				archivedTaskID = item.ID
				return true
			}
		}
		return false
	})
	if err = manager.RunTask(context.Background(), &types.OperateTaskReq{Queue: QueueDefault, TaskID: archivedTaskID}); err != nil {
		t.Fatalf("立即执行归档任务失败: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		return rootCalls.Load() >= 2 && childCalls.Load() >= 1
	})
	waitForCondition(t, 5*time.Second, func() bool {
		meta, _, readErr := manager.readWorkflowStatus(context.Background(), workflowID)
		return readErr == nil && meta.Status == WorkflowStatusSuccess
	})
	meta, nodes, err := manager.readWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("读取工作流状态失败: %v", err)
	}
	if meta.Status != WorkflowStatusSuccess {
		t.Fatalf("期望工作流重跑后成功，实际 meta=%+v nodes=%+v", meta, nodes)
	}
}

// TestCompletedTaskWritesResult 验证成功任务会把统一结果摘要写回 completed 记录。
func TestCompletedTaskWritesResult(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:result", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册结果测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:result",
		Payload:  json.RawMessage(`{"demo":true}`),
	})
	if err != nil {
		t.Fatalf("投递结果测试任务失败: %v", err)
	}

	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetQueueInfo(manager.namespacedQueueName(QueueDefault))
		return infoErr == nil && info.Completed >= 1
	})

	item, err := manager.GetTaskInfo(context.Background(), &types.GetTaskInfoReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	})
	if err != nil {
		t.Fatalf("查询任务详情失败: %v", err)
	}
	if item.State != "completed" {
		t.Fatalf("期望任务已完成，实际状态为 %s", item.State)
	}
	if len(item.Result) == 0 {
		t.Fatal("期望已完成任务写回结果摘要，实际为空")
	}

	var result TaskExecutionResult
	if err = json.Unmarshal(item.Result, &result); err != nil {
		t.Fatalf("解析任务结果摘要失败: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("期望结果状态为 success，实际为 %+v", result)
	}
	if result.TaskID != resp.TaskID {
		t.Fatalf("期望结果中的 task_id 与回执一致，期望=%s 实际=%s", resp.TaskID, result.TaskID)
	}
	if result.TaskName == "" {
		t.Fatalf("期望结果中包含任务名称，实际为 %+v", result)
	}
	if result.StartedAt == "" {
		t.Fatalf("期望结果中包含开始时间，实际为 %+v", result)
	}
	if _, parseErr := time.Parse(time.RFC3339, result.StartedAt); parseErr != nil {
		t.Fatalf("结果开始时间格式非法: %v", parseErr)
	}
	if item.StartedAt == "" || item.DurationMS <= 0 {
		t.Fatalf("成功任务应保留开始时间和耗时，实际 item=%+v", item)
	}
	taskTTL := manager.redis.TTL(context.Background(), keys.TaskAsynqTaskHashKey(manager.namespacedQueueName(QueueDefault), resp.TaskID)).Val()
	if taskTTL != -1 {
		t.Fatalf("成功终态任务不应设置项目 task hash TTL，实际 TTL=%s", taskTTL)
	}
	runtimeTTL := manager.redis.TTL(context.Background(), manager.taskRuntimeKey(resp.TaskID)).Val()
	expectedRuntimeTTL := manager.completedRetention() + time.Hour
	if runtimeTTL <= manager.completedRetention() || runtimeTTL > expectedRuntimeTTL+time.Second {
		t.Fatalf("成功终态任务 runtime TTL = %s，期望约为 %s", runtimeTTL, expectedRuntimeTTL)
	}
	if err = manager.redis.Del(context.Background(), manager.taskRuntimeKey(resp.TaskID)).Err(); err != nil {
		t.Fatalf("删除 runtime 兜底测试 key 失败: %v", err)
	}
	fallbackItem, err := manager.GetTaskInfo(context.Background(), &types.GetTaskInfoReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	})
	if err != nil {
		t.Fatalf("查询 runtime 缺失后的任务详情失败: %v", err)
	}
	if fallbackItem.StartedAt != result.StartedAt {
		t.Fatalf("runtime 缺失时应从结果兜底开始时间，got=%q want=%q", fallbackItem.StartedAt, result.StartedAt)
	}
}

// TestCompletedTaskHashUsesNamespacedQueue 验证多站点命名空间下只检查真实 Asynq 队列 task hash。
func TestCompletedTaskHashUsesNamespacedQueue(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := manager.CurrentConfig()
	cfg.AppID = "17"
	cfg.CompletedRetentionSeconds = 60
	useRuntimeAppID(t, cfg.AppID)
	manager.UpdateConfig(cfg)

	if err := manager.RegisterHandler("demo:namespaced-complete-ttl", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册命名空间 task hash 测试处理器失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}
	defer func() { _ = manager.Stop(context.Background()) }()

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:namespaced-complete-ttl",
		Payload:  json.RawMessage(`{"demo":true}`),
	})
	if err != nil {
		t.Fatalf("投递命名空间 task hash 测试任务失败: %v", err)
	}
	internalQueue := manager.namespacedQueueName(QueueDefault)
	waitForCondition(t, 5*time.Second, func() bool {
		info, infoErr := manager.inspector.GetTaskInfo(internalQueue, resp.TaskID)
		return infoErr == nil && info.State == asynq.TaskStateCompleted
	})

	internalKey := keys.TaskAsynqTaskHashKey(internalQueue, resp.TaskID)
	if exists := manager.redis.Exists(context.Background(), internalKey).Val(); exists != 1 {
		t.Fatalf("期望真实队列任务 hash 存在，exists=%d", exists)
	}
	ttl := manager.redis.TTL(context.Background(), internalKey).Val()
	if ttl != -1 {
		t.Fatalf("命名空间成功任务不应设置项目 task hash TTL，实际 TTL=%s", ttl)
	}
	if exists := manager.redis.Exists(context.Background(), keys.TaskAsynqTaskHashKey(QueueDefault, resp.TaskID)).Val(); exists != 0 {
		t.Fatalf("不应写入逻辑队列任务 hash，exists=%d", exists)
	}
}

// TestListCompletedTasksReturnsErrorForOrphanedZSetMember 验证历史孤儿 completed 成员会显式暴露。
func TestListCompletedTasksReturnsErrorForOrphanedZSetMember(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	ctx := context.Background()
	internalQueue := manager.namespacedQueueName(QueueDefault)
	if err := manager.redis.SAdd(ctx, "asynq:queues", internalQueue).Err(); err != nil {
		t.Fatalf("写入 Asynq 队列索引失败: %v", err)
	}
	completedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, "completed")
	if err != nil {
		t.Fatalf("生成 completed key 失败: %v", err)
	}
	if err = manager.redis.ZAdd(ctx, completedKey, redis.Z{
		Score:  float64(time.Now().Add(time.Hour).Unix()),
		Member: "orphan-completed-task",
	}).Err(); err != nil {
		t.Fatalf("写入孤儿 completed 成员失败: %v", err)
	}

	resp, err := manager.ListTasks(ctx, &types.ListTaskItemsReq{
		Queue:    QueueDefault,
		State:    "completed",
		Page:     1,
		PageSize: 20,
	})
	if err == nil {
		t.Fatalf("查询包含孤儿成员的 completed 列表应失败，实际 resp=%+v", resp)
	}
	if !errors.Is(err, asynq.ErrTaskNotFound) {
		t.Fatalf("期望暴露任务详情缺失错误，实际: %v", err)
	}
}

// TestRegisterGroupAggregatorDispatchesByGroup 验证分组聚合器会按 group 分发任务集合。
func TestRegisterGroupAggregatorDispatchesByGroup(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	expected := asynq.NewTask("demo:batch", []byte(`{"ok":true}`))
	if err := manager.RegisterGroupAggregator("demo.group", func(tasks []*asynq.Task) *asynq.Task {
		if len(tasks) != 1 || tasks[0].Type() != "demo:item" {
			t.Fatalf("分组任务集合不符合预期: %+v", tasks)
		}
		return expected
	}); err != nil {
		t.Fatalf("注册分组聚合器失败: %v", err)
	}
	got := manager.aggregateTasks(manager.namespacedGroup("demo.group"), []*asynq.Task{
		asynq.NewTask("demo:item", []byte(`{}`)),
	})
	if got == nil || got.Type() != expected.Type() {
		t.Fatalf("期望聚合后任务类型为 %s，实际为 %#v", expected.Type(), got)
	}
}

// TestRegisterPeriodicTaskRejectsConfigDuplicate 验证对应场景。
func TestRegisterPeriodicTaskRejectsConfigDuplicate(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
		Name:     "dup-periodic",
		Cron:     "*/5 * * * *",
		Workflow: WorkflowNameCacheRefresh,
		Queue:    QueueMaintenance,
	})
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled:  true,
		AppID:    "1",
		Periodic: []config.TaskPeriodicConfig{cfg},
	})

	if err := manager.RegisterPeriodicTask(cfg); err == nil {
		t.Fatal("期望返回重复周期任务错误，实际为 nil")
	}
}

// TestAllPeriodicTasksDedupesConfigAndPluginTasks 验证对应场景。
func TestAllPeriodicTasksDedupesConfigAndPluginTasks(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
		Name:     "same-periodic",
		Cron:     "*/5 * * * *",
		Workflow: WorkflowNameCacheRefresh,
		Queue:    QueueMaintenance,
	})
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled:  true,
		AppID:    "1",
		Periodic: []config.TaskPeriodicConfig{cfg},
	})
	manager.periodic = append(manager.periodic, cfg)

	items := manager.allPeriodicTasks()
	if len(items) != 1 {
		t.Fatalf("期望去重后仅保留 1 个周期任务，实际为 %d", len(items))
	}
	if got := manager.periodicTaskCount(); got != 1 {
		t.Fatalf("期望周期任务计数去重后为 1，实际为 %d", got)
	}
}

// TestAllPeriodicTasksDefaultsMissingEnabledToEnabled 验证周期任务缺省 enabled 时默认参与调度。
func TestAllPeriodicTasksDefaultsMissingEnabledToEnabled(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	disabled := false
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Periodic: []config.TaskPeriodicConfig{
			{
				Enabled:  &disabled,
				Name:     "disabled-periodic",
				Cron:     "*/5 * * * *",
				Workflow: WorkflowNameCacheRefresh,
				Queue:    QueueMaintenance,
			},
			{
				Name:     "missing-enabled-periodic",
				Cron:     "*/10 * * * *",
				Workflow: WorkflowNameCacheRefresh,
				Queue:    QueueMaintenance,
			},
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "enabled-periodic",
				Cron:     "*/15 * * * *",
				Workflow: WorkflowNameCacheRefresh,
				Queue:    QueueMaintenance,
			}),
		},
	})

	items := manager.allPeriodicTasks()
	if len(items) != 2 {
		t.Fatalf("期望保留缺省启用和显式启用的周期任务，实际为 %d", len(items))
	}
	if items[0].Name != "missing-enabled-periodic" || items[1].Name != "enabled-periodic" {
		t.Fatalf("保留的周期任务 = %+v", items)
	}
}

// TestPeriodicConfigsSkipsInvalidAndKeepsValid 验证周期任务配置异常只跳过当前任务，不影响其它合法任务。
func TestPeriodicConfigsSkipsInvalidAndKeepsValid(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("valid.workflow")); err != nil {
		t.Fatalf("注册合法工作流失败: %v", err)
	}
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Periodic: []config.TaskPeriodicConfig{
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "missing-workflow",
				Cron:     "*/5 * * * *",
				Workflow: "missing.workflow",
			}),
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "invalid-cron",
				Cron:     "bad cron",
				Workflow: "valid.workflow",
			}),
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:             "valid-periodic",
				Cron:             "*/5 * * * *",
				Workflow:         "valid.workflow",
				Queue:            QueueMaintenance,
				UniqueKey:        "valid-periodic",
				UniqueTTLSeconds: 60,
			}),
		},
	})

	configs, err := manager.periodicConfigs()
	if err != nil {
		t.Fatalf("生成周期任务配置失败: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("期望只保留 1 个合法周期任务，实际为 %d", len(configs))
	}
	if got := configs[0].Cronspec; got != "*/5 * * * *" {
		t.Fatalf("期望保留合法 cron，实际为 %s", got)
	}
}

// TestPeriodicConfigsSupportsSecondLevelSchedule 验证周期任务支持秒级 cron 和 every_seconds 固定间隔。
func TestPeriodicConfigsSupportsSecondLevelSchedule(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("second.workflow")); err != nil {
		t.Fatalf("注册秒级工作流失败: %v", err)
	}
	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Periodic: []config.TaskPeriodicConfig{
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "six-field-cron",
				Cron:     "*/10 * * * * *",
				Workflow: "second.workflow",
			}),
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:         "every-seconds",
				EverySeconds: 5,
				Workflow:     "second.workflow",
			}),
		},
	})

	configs, err := manager.periodicConfigs()
	if err != nil {
		t.Fatalf("生成秒级周期任务配置失败: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("期望生成 2 个秒级周期任务，实际为 %d", len(configs))
	}
	got := map[string]struct{}{}
	for _, cfg := range configs {
		got[cfg.Cronspec] = struct{}{}
	}
	if _, ok := got["*/10 * * * * *"]; !ok {
		t.Fatalf("期望支持 6 段秒级 cron，实际为 %+v", got)
	}
	if _, ok := got["@every 5s"]; !ok {
		t.Fatalf("期望支持 every_seconds 转换为 @every 5s，实际为 %+v", got)
	}
}

// TestPeriodicTaskCronspecRejectsTooSmallEverySeconds 验证过小固定间隔会被拒绝，避免误配置造成队列压力。
func TestPeriodicTaskCronspecRejectsTooSmallEverySeconds(t *testing.T) {
	if _, err := periodicTaskCronspec(config.TaskPeriodicConfig{EverySeconds: minPeriodicEverySeconds - 1}); err == nil {
		t.Fatal("期望 every_seconds 小于最小值时返回错误")
	}
	got, err := periodicTaskCronspec(config.TaskPeriodicConfig{EverySeconds: minPeriodicEverySeconds})
	if err != nil {
		t.Fatalf("期望最小 every_seconds 合法，实际错误: %v", err)
	}
	if got != "@every 5s" {
		t.Fatalf("Cronspec = %s, want @every 5s", got)
	}
}

// TestQueueInfoBacklogIgnoresArchivedAndActive 验证调度背压只统计等待类积压，不把历史归档任务计入当前压力。
func TestQueueInfoBacklogIgnoresArchivedAndActive(t *testing.T) {
	info := &asynq.QueueInfo{
		Pending:     3,
		Active:      10,
		Scheduled:   4,
		Retry:       5,
		Archived:    100,
		Completed:   200,
		Aggregating: 6,
	}
	if got := queueInfoBacklog(info); got != 18 {
		t.Fatalf("queueInfoBacklog = %d, want 18", got)
	}
}

// TestPeriodicCronParserNextTimes 验证 5 段分钟 cron、6 段秒级 cron 和 @every 的实际下一次触发时间。
func TestPeriodicCronParserNextTimes(t *testing.T) {
	base := time.Date(2026, 5, 9, 10, 0, 3, 0, time.Local)
	tests := []struct {
		name string    // name 表示测试场景名称。
		spec string    // spec 表示测试字段。
		want time.Time // want 表示期望结果。
	}{
		{
			name: "five-field-minute-cron",
			spec: "*/5 * * * *",
			want: time.Date(2026, 5, 9, 10, 5, 0, 0, time.Local),
		},
		{
			name: "six-field-second-cron",
			spec: "*/10 * * * * *",
			want: time.Date(2026, 5, 9, 10, 0, 10, 0, time.Local),
		},
		{
			name: "every-seconds",
			spec: "@every 5s",
			want: time.Date(2026, 5, 9, 10, 0, 8, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := periodicCronParser.Parse(tt.spec)
			if err != nil {
				t.Fatalf("解析周期表达式失败: %v", err)
			}
			if got := schedule.Next(base); !got.Equal(tt.want) {
				t.Fatalf("下一次触发时间不符合预期: got=%s want=%s", got.Format(time.RFC3339), tt.want.Format(time.RFC3339))
			}
		})
	}
}

// TestSchedulerEnabledRespectsConfigSwitch 确保 scheduler.enabled=false 时不会因为存在周期任务配置而仍被判定为开启。
func TestSchedulerEnabledRespectsConfigSwitch(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Scheduler: config.TaskQueueSchedulerConfig{
			Enabled: false,
		},
		Periodic: []config.TaskPeriodicConfig{
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "disabled-scheduler-periodic",
				Cron:     "*/5 * * * *",
				Workflow: "first.workflow",
			}),
		},
	})

	if manager.schedulerEnabled() {
		t.Fatal("期望 scheduler.enabled=false 时不启动调度器，实际返回 true")
	}
	status := manager.schedulerStatusSnapshot(context.Background())
	if status == nil {
		t.Fatal("期望调度器状态快照存在，实际为 nil")
	}
	if status.Enabled {
		t.Fatal("期望调度器状态中的 enabled=false，实际为 true")
	}
	if status.PeriodicTaskCount != 1 {
		t.Fatalf("期望周期任务数量为 1，实际为 %d", status.PeriodicTaskCount)
	}
}

// TestSchedulerEnabledRequiresConfigSwitch 确保未配置 scheduler.enabled 时不会隐式启动调度器。
func TestSchedulerEnabledRequiresConfigSwitch(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Periodic: []config.TaskPeriodicConfig{
			enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
				Name:     "default-enabled-periodic",
				Cron:     "*/5 * * * *",
				Workflow: "first.workflow",
			}),
		},
	})

	if manager.schedulerEnabled() {
		t.Fatal("期望未配置 scheduler.enabled 时不启动调度器，实际返回 true")
	}
	status := manager.schedulerStatusSnapshot(context.Background())
	if status == nil {
		t.Fatal("期望调度器状态快照存在，实际为 nil")
	}
	if status.Enabled {
		t.Fatal("期望调度器状态中的 enabled=false，实际为 true")
	}
	if status.PeriodicTaskCount != 1 {
		t.Fatalf("期望周期任务数量为 1，实际为 %d", status.PeriodicTaskCount)
	}
}

// TestListQueuesReturnsSchedulerStatus 确保任务队列概览接口会附带调度器运行状态。
func TestListQueuesReturnsSchedulerStatus(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	manager.UpdateConfig(config.TaskQueueConfig{
		Enabled: true,
		AppID:   "1",
		Queues: map[string]int{
			QueueDefault: 1,
		},
		Scheduler: config.TaskQueueSchedulerConfig{
			Enabled:                  true,
			SyncIntervalSeconds:      30,
			HeartbeatIntervalSeconds: 12,
		},
	})

	resp, err := manager.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("查询任务队列概览失败: %v", err)
	}
	if resp.Scheduler == nil {
		t.Fatal("期望返回调度器状态，实际为 nil")
	}
	if !resp.Scheduler.Enabled {
		t.Fatal("期望调度器状态 enabled=true，实际为 false")
	}
	if resp.Scheduler.SyncIntervalSeconds != 30 {
		t.Fatalf("期望同步间隔为 30 秒，实际为 %d", resp.Scheduler.SyncIntervalSeconds)
	}
	if resp.Scheduler.HeartbeatIntervalSeconds != 12 {
		t.Fatalf("期望心跳间隔为 12 秒，实际为 %d", resp.Scheduler.HeartbeatIntervalSeconds)
	}
}

// TestListQueuesReturnsErrorForOrphanedCompletedMember 验证队列概览不吞掉 Asynq 巡检错误。
func TestListQueuesReturnsErrorForOrphanedCompletedMember(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	ctx := context.Background()
	internalQueue := manager.namespacedQueueName(QueueDefault)
	if err := manager.redis.SAdd(ctx, "asynq:queues", internalQueue).Err(); err != nil {
		t.Fatalf("写入 Asynq 队列索引失败: %v", err)
	}
	completedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, "completed")
	if err != nil {
		t.Fatalf("生成 completed key 失败: %v", err)
	}
	if err = manager.redis.ZAdd(ctx, completedKey, redis.Z{
		Score:  float64(time.Now().Add(time.Hour).Unix()),
		Member: "orphan-completed-task",
	}).Err(); err != nil {
		t.Fatalf("写入孤儿 completed 成员失败: %v", err)
	}

	resp, err := manager.ListQueues(ctx)
	if err == nil {
		t.Fatalf("查询包含孤儿成员的队列概览应失败，实际 resp=%+v", resp)
	}
}

// TestEnqueueRegisteredTaskRejectsUnknownType 验证对应场景。
func TestEnqueueRegisteredTaskRejectsUnknownType(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	_, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "unknown:type",
		Payload:  json.RawMessage(`{"demo":true}`),
	})
	if !errors.Is(err, ErrTaskTypeNotFound) {
		t.Fatalf("期望返回 ErrTaskTypeNotFound，实际为 %v", err)
	}
}

// TestEnqueueRegisteredTaskSupportsScheduling 验证对应场景。
func TestEnqueueRegisteredTaskSupportsScheduling(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:scheduled", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册调度测试处理器失败: %v", err)
	}
	processAt := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:scheduled",
		Payload:   json.RawMessage(`{"demo":true}`),
		ProcessAt: processAt,
	})
	if err != nil {
		t.Fatalf("投递已注册任务失败: %v", err)
	}
	if resp.TaskID == "" || resp.TaskType != "demo:scheduled" {
		t.Fatalf("入队回执不符合预期: %+v", resp)
	}
	if resp.ProcessAt == "" {
		t.Fatalf("期望回执中包含调度执行时间: %+v", resp)
	}
}

// TestListTasksAndGetTaskInfo 验证对应场景。
func TestListTasksAndGetTaskInfo(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:list", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册列表测试处理器失败: %v", err)
	}
	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:list",
		Payload:   json.RawMessage(`{"demo":true}`),
		ProcessAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递任务失败: %v", err)
	}

	listResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueDefault,
		State:    "scheduled",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("查询任务列表失败: %v", err)
	}
	if len(listResp.Tasks) == 0 {
		t.Fatal("期望任务列表中存在已调度任务")
	}

	item, err := manager.GetTaskInfo(context.Background(), &types.GetTaskInfoReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	})
	if err != nil {
		t.Fatalf("获取任务详情失败: %v", err)
	}
	if item.ID != resp.TaskID || item.TaskType != "demo:list" || item.State == "" {
		t.Fatalf("任务详情不符合预期: %+v", item)
	}
}

// TestListTasksScheduledTimeDescAndRange 验证任务列表按计划执行时间倒序展示，并支持时间段筛选定时任务。
func TestListTasksScheduledTimeDescAndRange(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:scheduled-sort", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册定时排序测试处理器失败: %v", err)
	}
	now := time.Now().UTC()
	earlyAt := now.Add(10 * time.Minute).Truncate(time.Second)
	middleAt := now.Add(20 * time.Minute).Truncate(time.Second)
	lateAt := now.Add(30 * time.Minute).Truncate(time.Second)
	earlyResp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:scheduled-sort",
		Payload:   json.RawMessage(`{"name":"early"}`),
		ProcessAt: earlyAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递较早定时任务失败: %v", err)
	}
	middleResp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:scheduled-sort",
		Payload:   json.RawMessage(`{"name":"middle"}`),
		ProcessAt: middleAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递中间定时任务失败: %v", err)
	}
	lateResp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:scheduled-sort",
		Payload:   json.RawMessage(`{"name":"late"}`),
		ProcessAt: lateAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递较晚定时任务失败: %v", err)
	}

	listResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueDefault,
		State:    "scheduled",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("查询定时任务列表失败: %v", err)
	}
	gotIDs := []string{listResp.Tasks[0].ID, listResp.Tasks[1].ID, listResp.Tasks[2].ID}
	wantIDs := []string{lateResp.TaskID, middleResp.TaskID, earlyResp.TaskID}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("定时任务排序不符合预期: got=%v want=%v", gotIDs, wantIDs)
	}

	rangeResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:     QueueDefault,
		State:     "scheduled",
		StartTime: middleAt.Add(-time.Second).Format(time.RFC3339),
		EndTime:   middleAt.Add(time.Second).Format(time.RFC3339),
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("按时间段查询定时任务失败: %v", err)
	}
	if rangeResp.Total != 1 || len(rangeResp.Tasks) != 1 || rangeResp.Tasks[0].ID != middleResp.TaskID {
		t.Fatalf("时间段过滤不符合预期: total=%d tasks=%+v", rangeResp.Total, rangeResp.Tasks)
	}
}

// TestListTasksFiltersByTaskName 验证任务列表支持按任务名称关键字筛选周期/工作流任务。
func TestListTasksFiltersByTaskName(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("demo.periodic.workflow")); err != nil {
		t.Fatalf("注册工作流失败: %v", err)
	}
	resp, err := manager.EnqueueWorkflowTrigger(context.Background(), &types.TriggerTaskWorkflowReq{
		Name:      "demo.periodic.workflow",
		Queue:     QueueMaintenance,
		ProcessAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递工作流触发任务失败: %v", err)
	}

	listResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueMaintenance,
		State:    "scheduled",
		TaskName: "periodic.workflow",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("按任务名称查询任务列表失败: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Tasks) != 1 {
		t.Fatalf("期望按任务名称命中 1 条任务，实际 total=%d tasks=%d", listResp.Total, len(listResp.Tasks))
	}
	if listResp.Tasks[0].ID != resp.TaskID {
		t.Fatalf("按任务名称命中的任务不符合预期: got=%s want=%s", listResp.Tasks[0].ID, resp.TaskID)
	}

	missResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueMaintenance,
		State:    "scheduled",
		TaskName: "not-exists",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("按不存在任务名称查询失败: %v", err)
	}
	if missResp.Total != 0 || len(missResp.Tasks) != 0 {
		t.Fatalf("期望不存在任务名称无结果，实际 total=%d tasks=%d", missResp.Total, len(missResp.Tasks))
	}
}

// TestListTasksFiltersByTaskID 验证任务列表支持按任务 ID 关键字筛选。
func TestListTasksFiltersByTaskID(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:task-id-filter", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册任务 ID 筛选测试处理器失败: %v", err)
	}
	firstResp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:task-id-filter",
		Payload:   json.RawMessage(`{"name":"first"}`),
		ProcessAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递第一个任务失败: %v", err)
	}
	if _, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:task-id-filter",
		Payload:   json.RawMessage(`{"name":"second"}`),
		ProcessAt: time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("投递第二个任务失败: %v", err)
	}

	listResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueDefault,
		State:    "scheduled",
		TaskID:   firstResp.TaskID[:8],
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("按任务 ID 查询任务列表失败: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Tasks) != 1 {
		t.Fatalf("期望按任务 ID 命中 1 条任务，实际 total=%d tasks=%d", listResp.Total, len(listResp.Tasks))
	}
	if listResp.Tasks[0].ID != firstResp.TaskID {
		t.Fatalf("按任务 ID 命中的任务不符合预期: got=%s want=%s", listResp.Tasks[0].ID, firstResp.TaskID)
	}
}

// TestListTasksFiltersPeriodicTaskByWorkflowName 验证周期入口任务可以用 workflow 名称反查。
func TestListTasksFiltersPeriodicTaskByWorkflowName(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterWorkflow(testWorkflowDefinition("user_tag.delta.refresh")); err != nil {
		t.Fatalf("注册周期工作流失败: %v", err)
	}
	manager.mu.Lock()
	manager.periodic = []config.TaskPeriodicConfig{
		enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
			Name:       "user-tag-delta-daily",
			Cron:       "15 3 * * *",
			Workflow:   "user_tag.delta.refresh",
			Queue:      QueueMaintenance,
			ShardTotal: 10,
		}),
	}
	manager.mu.Unlock()
	configs, err := manager.periodicConfigs()
	if err != nil {
		t.Fatalf("生成周期任务配置失败: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("期望生成 1 个周期任务配置，实际为 %d", len(configs))
	}
	opts := append([]asynq.Option{}, configs[0].Opts...)
	opts = append(opts, asynq.ProcessAt(time.Now().Add(time.Minute)))
	info, err := manager.client.EnqueueContext(context.Background(), configs[0].Task, opts...)
	if err != nil {
		t.Fatalf("投递周期入口任务失败: %v", err)
	}

	listResp, err := manager.ListTasks(context.Background(), &types.ListTaskItemsReq{
		Queue:    QueueMaintenance,
		State:    "scheduled",
		TaskName: "user_tag.delta.refresh",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("按 workflow 名称查询周期任务失败: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Tasks) != 1 {
		t.Fatalf("期望按 workflow 名称命中 1 条周期任务，实际 total=%d tasks=%d", listResp.Total, len(listResp.Tasks))
	}
	if listResp.Tasks[0].ID != info.ID {
		t.Fatalf("按 workflow 名称命中的周期任务不符合预期: got=%s want=%s", listResp.Tasks[0].ID, info.ID)
	}
}

// TestValidatePeriodicTaskDefinitionsToleratesMissingWorkflow 验证启动期不会因当前版本不认识的周期任务阻断进程。
func TestValidatePeriodicTaskDefinitionsToleratesMissingWorkflow(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := manager.CurrentConfig()
	cfg.Periodic = []config.TaskPeriodicConfig{
		enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
			Name:     "missing-periodic-workflow",
			Cron:     "*/10 * * * *",
			Workflow: "missing.workflow",
		}),
	}
	manager.UpdateConfig(cfg)

	err := manager.ValidatePeriodicTaskDefinitions()
	if err != nil {
		t.Fatalf("当前版本不认识的周期任务不应阻断启动: %v", err)
	}

	if registerErr := manager.RegisterWorkflow(testWorkflowDefinition("missing.workflow")); registerErr != nil {
		t.Fatalf("注册补齐工作流失败: %v", registerErr)
	}
	if err = manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("补齐工作流后不应再失败: %v", err)
	}
}

// TestPeriodicConfigInvalidHookThrottlesRepeatedAlert 验证同一条周期配置异常不会在同步循环中重复告警。
func TestPeriodicConfigInvalidHookThrottlesRepeatedAlert(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()
	if periodicConfigAlertTTL != 5*time.Minute {
		t.Fatalf("周期配置异常告警限频窗口 = %s, want 5m", periodicConfigAlertTTL)
	}

	cfg := manager.CurrentConfig()
	cfg.Periodic = []config.TaskPeriodicConfig{
		enabledTaskPeriodicConfig(config.TaskPeriodicConfig{
			Name:      "missing-periodic-workflow",
			Cron:      "*/10 * * * *",
			Workflow:  "missing.workflow",
			Queue:     QueueDefault,
			UniqueKey: "periodic:missing.workflow",
		}),
	}
	manager.UpdateConfig(cfg)

	var calls atomic.Int32
	var got PeriodicConfigInvalidReport
	if err := manager.RegisterPeriodicConfigInvalidHook(func(ctx context.Context, report PeriodicConfigInvalidReport) error {
		_ = ctx
		calls.Add(1)
		got = report
		return nil
	}); err != nil {
		t.Fatalf("注册周期配置异常钩子失败: %v", err)
	}

	if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("校验周期任务定义失败: %v", err)
	}
	if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("重复校验周期任务定义失败: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("同一异常应只触发 1 次告警钩子，实际=%d", calls.Load())
	}
	if got.Task.Name != "missing-periodic-workflow" || got.Reason == "" {
		t.Fatalf("告警报告不符合预期: %+v", got)
	}
	if got.TriggerCount != 1 {
		t.Fatalf("首次告警触发次数 = %d, want 1", got.TriggerCount)
	}

	key := periodicConfigInvalidAlertKey(0, cfg.Periodic[0], got.Reason)
	manager.mu.Lock()
	state := manager.periodicConfigAlertedMap[key]
	state.LastSentAt = time.Now().Add(-periodicConfigAlertTTL - time.Second)
	manager.periodicConfigAlertedMap[key] = state
	manager.mu.Unlock()

	if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("限频窗口后重复校验周期任务定义失败: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("限频窗口后应再次触发告警钩子，实际=%d", calls.Load())
	}
	if got.TriggerCount != 2 {
		t.Fatalf("窗口后告警触发次数 = %d, want 2", got.TriggerCount)
	}
}

// TestTaskRuntimeAlertHookThrottlesRepeatedAlert 验证任务系统运行异常按同一指纹限频告警。
func TestTaskRuntimeAlertHookThrottlesRepeatedAlert(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()
	if taskRuntimeAlertTTL != 5*time.Minute {
		t.Fatalf("任务运行异常告警限频窗口 = %s, want 5m", taskRuntimeAlertTTL)
	}

	alert := TaskRuntimeAlert{
		Kind:      taskRuntimeAlertKindPeriodicEnqueueFailed,
		Title:     "【P1 周期任务入队失败】",
		Component: "scheduler",
		Operation: "enqueue_periodic_task",
		TaskName:  "周期任务触发:demo",
		TaskType:  TypeWorkflowTrigger,
		Cron:      "*/2 * * * *",
		TaskQueue: QueueDefault,
		UniqueKey: "periodic:demo",
		Reason:    "redis timeout",
	}

	var calls atomic.Int32
	var got TaskRuntimeAlert
	if err := manager.RegisterRuntimeAlertHook(func(ctx context.Context, report TaskRuntimeAlert) error {
		_ = ctx
		calls.Add(1)
		got = report
		return nil
	}); err != nil {
		t.Fatalf("注册任务运行异常钩子失败: %v", err)
	}

	manager.notifyTaskRuntimeAlert(context.Background(), alert)
	alert.Reason = "redis timeout again"
	manager.notifyTaskRuntimeAlert(context.Background(), alert)
	if calls.Load() != 1 {
		t.Fatalf("同一运行异常应只触发 1 次告警钩子，实际=%d", calls.Load())
	}
	if got.TriggerCount != 1 || got.Reason != "redis timeout" {
		t.Fatalf("首次运行异常告警不符合预期: %+v", got)
	}

	key := taskRuntimeAlertKey(alert)
	manager.mu.Lock()
	state := manager.runtimeAlertedMap[key]
	state.LastSentAt = time.Now().Add(-taskRuntimeAlertTTL - time.Second)
	manager.runtimeAlertedMap[key] = state
	manager.mu.Unlock()

	manager.notifyTaskRuntimeAlert(context.Background(), alert)
	if calls.Load() != 2 {
		t.Fatalf("限频窗口后应再次触发运行异常钩子，实际=%d", calls.Load())
	}
	if got.TriggerCount != 2 || got.Reason != "redis timeout again" {
		t.Fatalf("窗口后运行异常告警不符合预期: %+v", got)
	}
}

// TestTaskRuntimeAlertThrottleUsesSendWindow 验证业务上报历史发生时间不会绕过当前发送窗口限频。
func TestTaskRuntimeAlertThrottleUsesSendWindow(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	var calls atomic.Int32
	if err := manager.RegisterRuntimeAlertHook(func(ctx context.Context, report TaskRuntimeAlert) error {
		_ = ctx
		_ = report
		calls.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("注册任务运行异常钩子失败: %v", err)
	}

	alert := TaskRuntimeAlert{
		Kind:       taskRuntimeAlertKindPeriodicEnqueueFailed,
		Component:  "scheduler",
		Operation:  "enqueue_periodic_task",
		TaskName:   "周期任务触发:demo",
		TaskType:   TypeWorkflowTrigger,
		TaskQueue:  QueueDefault,
		UniqueKey:  "periodic:demo",
		Reason:     "redis timeout",
		OccurredAt: time.Now().Add(-time.Hour),
	}
	manager.notifyTaskRuntimeAlert(context.Background(), alert)
	alert.OccurredAt = time.Time{}
	alert.Reason = "redis timeout again"
	manager.notifyTaskRuntimeAlert(context.Background(), alert)
	if calls.Load() != 1 {
		t.Fatalf("历史发生时间不应绕过当前发送窗口限频，实际告警次数=%d", calls.Load())
	}
}

// TestRunTaskMovesScheduledTaskToPending 验证对应场景。
func TestRunTaskMovesScheduledTaskToPending(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:run", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册立即执行测试处理器失败: %v", err)
	}
	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:run",
		Payload:   json.RawMessage(`{"demo":true}`),
		ProcessAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递任务失败: %v", err)
	}
	if err := manager.RunTask(context.Background(), &types.OperateTaskReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	}); err != nil {
		t.Fatalf("立即执行任务失败: %v", err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		info, err := manager.inspector.GetTaskInfo(manager.namespacedQueueName(QueueDefault), resp.TaskID)
		return err == nil && info.State == asynq.TaskStatePending
	})
}

// TestDeleteTaskRemovesScheduledTask 验证对应场景。
func TestDeleteTaskRemovesScheduledTask(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	if err := manager.RegisterHandler("demo:delete", asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatalf("注册删除测试处理器失败: %v", err)
	}
	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType:  "demo:delete",
		Payload:   json.RawMessage(`{"demo":true}`),
		ProcessAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("投递任务失败: %v", err)
	}
	if err := manager.DeleteTask(context.Background(), &types.OperateTaskReq{
		Queue:  QueueDefault,
		TaskID: resp.TaskID,
	}); err != nil {
		t.Fatalf("删除任务失败: %v", err)
	}
	if _, err := manager.inspector.GetTaskInfo(manager.namespacedQueueName(QueueDefault), resp.TaskID); !errors.Is(err, asynq.ErrTaskNotFound) {
		t.Fatalf("期望删除后任务不存在，实际为 %v", err)
	}
}

// TestTaskRetentionDefaultsWhenUnset 验证未配置任务保留时间时仍有安全默认值，避免上线缺字段导致任务系统异常。
func TestTaskRetentionDefaultsWhenUnset(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	cfg := manager.CurrentConfig()
	if cfg.CompletedRetentionSeconds != 0 || cfg.ArchivedRetentionSeconds != 0 {
		t.Fatalf("测试配置应模拟未配置保留时间，实际 completed=%d archived=%d", cfg.CompletedRetentionSeconds, cfg.ArchivedRetentionSeconds)
	}
	if got, want := manager.completedRetention(), 24*time.Hour; got != want {
		t.Fatalf("未配置 completed_retention_seconds 时默认值 = %s，期望 %s", got, want)
	}
	if got, want := manager.workflowRetention(), manager.completedRetention(); got != want {
		t.Fatalf("工作流保留时间应复用 completed_retention_seconds，实际 %s，期望 %s", got, want)
	}
	if got, want := manager.archivedRetention(), time.Duration(taskArchivedRetentionDefaultSeconds)*time.Second; got != want {
		t.Fatalf("未配置 archived_retention_seconds 时默认值 = %s，期望 %s", got, want)
	}
	cfg.CompletedRetentionSeconds = 90
	manager.UpdateConfig(cfg)
	if got, want := manager.workflowRetention(), 90*time.Second; got != want {
		t.Fatalf("工作流保留时间未跟随 completed_retention_seconds，实际 %s，期望 %s", got, want)
	}
}

// TestDeleteExpiredArchivedTasksRemovesTaskCache 验证归档失败任务过期后会同步清理 zset、hash 和唯一键。
func TestDeleteExpiredArchivedTasksRemovesTaskCache(t *testing.T) {
	manager, cleanup := newTestManager(t)
	defer cleanup()

	ctx := context.Background()
	internalQueue := manager.namespacedQueueName(QueueMaintenance)
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, "archived")
	if err != nil {
		t.Fatalf("生成 archived key 失败: %v", err)
	}
	oldTaskIDs := []string{"old-archived-task-1", "old-archived-task-2", "old-archived-task-3"}
	newTaskID := "new-archived-task"
	uniqueKey := keys.TaskAsynqUniqueKey(internalQueue, "demo:archived", []byte("test"))
	newTaskKey := keys.TaskAsynqTaskHashKey(internalQueue, newTaskID)
	if err = manager.redis.ZAdd(ctx, archivedKey,
		redis.Z{Score: 100, Member: oldTaskIDs[0]},
		redis.Z{Score: 110, Member: oldTaskIDs[1]},
		redis.Z{Score: 120, Member: oldTaskIDs[2]},
		redis.Z{Score: 300, Member: newTaskID},
	).Err(); err != nil {
		t.Fatalf("写入 archived zset 失败: %v", err)
	}
	for index, oldTaskID := range oldTaskIDs {
		fields := []any{"state", "archived"}
		if index == 0 {
			fields = append(fields, "unique_key", uniqueKey)
		}
		if err = manager.redis.HSet(ctx, keys.TaskAsynqTaskHashKey(internalQueue, oldTaskID), fields...).Err(); err != nil {
			t.Fatalf("写入过期任务 hash 失败: %v", err)
		}
	}
	if err = manager.redis.HSet(ctx, newTaskKey, "state", "archived").Err(); err != nil {
		t.Fatalf("写入新任务 hash 失败: %v", err)
	}
	if err = manager.redis.Set(ctx, uniqueKey, oldTaskIDs[0], time.Hour).Err(); err != nil {
		t.Fatalf("写入唯一键失败: %v", err)
	}

	deleted, err := manager.cleanupExpiredArchivedQueue(ctx, internalQueue, 200, 2, 2)
	if err != nil {
		t.Fatalf("清理过期归档任务失败: %v", err)
	}
	if deleted != int64(len(oldTaskIDs)) {
		t.Fatalf("清理数量 = %d，期望 %d", deleted, len(oldTaskIDs))
	}
	oldTaskKeys := []string{uniqueKey}
	for _, oldTaskID := range oldTaskIDs {
		oldTaskKeys = append(oldTaskKeys, keys.TaskAsynqTaskHashKey(internalQueue, oldTaskID))
	}
	if exists := manager.redis.Exists(ctx, oldTaskKeys...).Val(); exists != 0 {
		t.Fatalf("过期任务 hash 或唯一键仍存在，exists=%d", exists)
	}
	for _, oldTaskID := range oldTaskIDs {
		if _, err = manager.redis.ZScore(ctx, archivedKey, oldTaskID).Result(); !errors.Is(err, redis.Nil) {
			t.Fatalf("过期任务 zset 成员仍存在，task_id=%s err=%v", oldTaskID, err)
		}
	}
	if exists := manager.redis.Exists(ctx, newTaskKey).Val(); exists != 1 {
		t.Fatalf("新任务 hash 应保留，exists=%d", exists)
	}
	if _, err = manager.redis.ZScore(ctx, archivedKey, newTaskID).Result(); err != nil {
		t.Fatalf("新任务 zset 成员应保留，err=%v", err)
	}
}

// TestLeaderRunnerAvoidsDuplicateSchedulerAndFailsOver 验证对应场景。
func TestLeaderRunnerAvoidsDuplicateSchedulerAndFailsOver(t *testing.T) {
	server, client := newTestRedis(t)
	defer server.Close()
	defer client.Close()

	var acquired1 atomic.Int32
	var acquired2 atomic.Int32
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	runner1 := NewLeaderRunner(client, "task:test:leader", 300*time.Millisecond, 100*time.Millisecond, func(context.Context) (func(), error) {
		acquired1.Add(1)
		return func() {}, nil
	})
	go runner1.Start(ctx1)
	waitForCondition(t, 2*time.Second, func() bool { return acquired1.Load() == 1 })

	runner2 := NewLeaderRunner(client, "task:test:leader", 300*time.Millisecond, 100*time.Millisecond, func(context.Context) (func(), error) {
		acquired2.Add(1)
		return func() {}, nil
	})
	go runner2.Start(ctx2)

	time.Sleep(250 * time.Millisecond)
	if acquired2.Load() != 0 {
		t.Fatalf("期望首个 leader 存活期间第二个 leader 无法抢锁，实际为 %d", acquired2.Load())
	}

	cancel1()
	waitForCondition(t, 2*time.Second, func() bool { return acquired2.Load() == 1 })
}

// newTestManager 构造测试依赖。
func newTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	server, client := newTestRedis(t)
	cfg := config.TaskQueueConfig{
		Enabled:                 true,
		AppID:                   "1",
		DefaultQueue:            QueueDefault,
		DefaultRetry:            1,
		DefaultTimeoutSeconds:   1,
		DefaultUniqueTTLSeconds: 60,
		DelayedTaskCheckSeconds: 1,
		TaskCheckSeconds:        1,
		Queues: map[string]int{
			QueueDefault:     1,
			QueueMaintenance: 1,
		},
	}
	useRuntimeAppID(t, cfg.AppID)
	manager := New(cfg, client)
	if manager == nil {
		t.Fatal("期望成功创建测试任务管理器")
	}
	cleanup := func() {
		_ = manager.Stop(context.Background())
		_ = client.Close()
		server.Close()
	}
	return manager, cleanup
}

// useRuntimeAppID 表示测试辅助逻辑。
func useRuntimeAppID(t *testing.T, appID string) {
	t.Helper()
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
}

// newTestRedis 构造测试依赖。
func newTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	return server, client
}

// testNoopPayload 表示测试辅助逻辑。
func testNoopPayload(spec WorkflowStartSpec, node *WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
	return mustJSONBytes(NoopPayload{
		WorkflowTaskMeta: WorkflowTaskMeta{
			WorkflowID:   spec.WorkflowID,
			WorkflowName: spec.Name,
			WorkflowNode: node.Name,
			ShardIndex:   shardIndex,
			ShardTotal:   shardTotal,
		},
		Note: "test",
	}), nil
}

// testWorkflowDefinition 表示测试辅助逻辑。
func testWorkflowDefinition(name string) *WorkflowDefinition {
	return &WorkflowDefinition{
		Name: name,
		Nodes: map[string]*WorkflowNodeDefinition{
			"root": {
				TaskType:     TypeWorkflowNoop,
				BuildPayload: testNoopPayload,
			},
		},
	}
}

// findWorkflowIDWithoutGrayShard 表示测试辅助逻辑。
func findWorkflowIDWithoutGrayShard(t *testing.T, nodeName string, shardTotal int, grayPercent int) string {
	t.Helper()
	for i := 0; i < 1000; i++ {
		workflowID := "wf-gray-skip-" + time.Now().Add(time.Duration(i)*time.Nanosecond).Format("150405.000000000")
		if len(selectedShardIndexes(workflowID, nodeName, shardTotal, grayPercent, true)) == 0 {
			return workflowID
		}
	}
	t.Fatalf("未找到满足灰度全跳过条件的工作流 ID: node=%s shardTotal=%d grayPercent=%d", nodeName, shardTotal, grayPercent)
	return ""
}

// waitForCondition 表示测试辅助逻辑。
func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("超时前条件仍未满足")
}
