package logic

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"admin_cron/internal/config"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
)

type testConfigReloader struct {
	triggered bool
	source    string
	err       error
}

type taskOverviewFakeQueue struct {
	tasksByState map[string][]types.TaskItem
	listCalls    []types.ListTaskItemsReq
}

func (f *taskOverviewFakeQueue) IsEnabled() bool { return true }

func (f *taskOverviewFakeQueue) EnqueueTask(context.Context, string, []byte, ...svc.TaskOption) error {
	return nil
}

func (f *taskOverviewFakeQueue) EnqueueCacheRefresh(context.Context, string, []string) error {
	return nil
}

func (f *taskOverviewFakeQueue) EnqueueWorkflowTrigger(context.Context, *types.TriggerTaskWorkflowReq) (*types.TaskWorkflowTriggerResp, error) {
	return nil, nil
}

func (f *taskOverviewFakeQueue) GetWorkflowStatus(context.Context, string) (*types.TaskWorkflowStatusResp, error) {
	return nil, nil
}

func (f *taskOverviewFakeQueue) EnqueueRegisteredTask(context.Context, *types.EnqueueTaskReq) (*types.TaskEnqueueResp, error) {
	return nil, nil
}

func (f *taskOverviewFakeQueue) ListRegisteredTaskTypes() []types.TaskTypeRegistryItem {
	return nil
}

func (f *taskOverviewFakeQueue) ListRegisteredWorkflows() []types.WorkflowRegistryItem {
	return nil
}

func (f *taskOverviewFakeQueue) ListTasks(_ context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error) {
	if req == nil {
		return &types.TaskListResp{Tasks: []types.TaskItem{}}, nil
	}
	f.listCalls = append(f.listCalls, *req)
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	items := append([]types.TaskItem(nil), f.tasksByState[req.State]...)
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if start >= len(items) {
		items = []types.TaskItem{}
	} else {
		if end > len(items) {
			end = len(items)
		}
		items = items[start:end]
	}
	return &types.TaskListResp{
		Queue:    req.Queue,
		State:    req.State,
		Page:     page,
		PageSize: pageSize,
		Total:    int64(len(f.tasksByState[req.State])),
		Tasks:    items,
	}, nil
}

func (f *taskOverviewFakeQueue) GetTaskInfo(context.Context, *types.GetTaskInfoReq) (*types.TaskItem, error) {
	return nil, nil
}

func (f *taskOverviewFakeQueue) RunTask(context.Context, *types.OperateTaskReq) error {
	return nil
}

func (f *taskOverviewFakeQueue) DeleteTask(context.Context, *types.OperateTaskReq) error {
	return nil
}

func (f *taskOverviewFakeQueue) ListQueues(context.Context) (*types.TaskQueueListResp, error) {
	pending := f.stateCount(taskStatePending)
	active := f.stateCount(taskStateActive)
	scheduled := f.stateCount(taskStateScheduled)
	retry := f.stateCount(taskStateRetry)
	archived := f.stateCount(taskStateArchived)
	completed := f.stateCount(taskStateCompleted)
	aggregating := f.stateCount(taskStateAggregating)
	return &types.TaskQueueListResp{
		Queues: []types.TaskQueueItem{{
			Name:        "default",
			Size:        pending + active + scheduled + retry + archived + completed + aggregating,
			Pending:     pending,
			Active:      active,
			Scheduled:   scheduled,
			Retry:       retry,
			Archived:    archived,
			Completed:   completed,
			Aggregating: aggregating,
		}},
	}, nil
}

func (f *taskOverviewFakeQueue) PauseQueue(context.Context, string) error  { return nil }
func (f *taskOverviewFakeQueue) ResumeQueue(context.Context, string) error { return nil }

func (f *taskOverviewFakeQueue) stateCount(state string) int {
	return len(f.tasksByState[state])
}

// TestBuildTaskItemSortValueUsesActivityTime 验证多队列聚合排序使用任务活动时间。
func TestBuildTaskItemSortValueUsesActivityTime(t *testing.T) {
	startedAt := time.Date(2026, 5, 23, 3, 15, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 23, 4, 30, 0, 0, time.UTC)
	nextProcessAt := time.Date(2026, 5, 23, 5, 0, 0, 0, time.UTC)

	completedValue := buildTaskItemSortValue(types.TaskItem{
		State:       taskStateCompleted,
		StartedAt:   startedAt.Format(time.RFC3339),
		CompletedAt: completedAt.Format(time.RFC3339),
	})
	if completedValue != completedAt.UnixMilli() {
		t.Fatalf("completed sort value = %d, want %d", completedValue, completedAt.UnixMilli())
	}

	scheduledValue := buildTaskItemSortValue(types.TaskItem{
		State:         taskStateScheduled,
		StartedAt:     startedAt.Format(time.RFC3339),
		NextProcessAt: nextProcessAt.Format(time.RFC3339),
	})
	if scheduledValue != nextProcessAt.UnixMilli() {
		t.Fatalf("scheduled sort value = %d, want %d", scheduledValue, nextProcessAt.UnixMilli())
	}
}

// TestListTasksOverviewAggregatesEmptyStateByTaskTime 验证空状态总览不会被旧 retry 任务挡住最新 completed 任务。
func TestListTasksOverviewAggregatesEmptyStateByTaskTime(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.Task = &taskOverviewFakeQueue{
		tasksByState: map[string][]types.TaskItem{
			taskStateRetry: {{
				ID:           "retry-old",
				Queue:        "default",
				State:        taskStateRetry,
				LastFailedAt: "2026-05-25T11:47:02Z",
			}},
			taskStateCompleted: {{
				ID:          "completed-new",
				Queue:       "default",
				State:       taskStateCompleted,
				CompletedAt: "2026-05-25T12:30:00Z",
			}},
		},
	}

	logicObj := NewTaskLogic(httptest.NewRequest("GET", "/api/tasks/overview?page=1&pageSize=20", nil), svcCtx)
	resp := logicObj.ListTasksOverview(&types.ListTaskItemsOverviewReq{Page: 1, PageSize: 20})
	if resp == nil || !resp.IsSuccess() {
		t.Fatalf("期望任务总览查询成功，实际: %+v", resp)
	}
	data, ok := resp.Data.(*types.TaskListOverviewResp)
	if !ok {
		t.Fatalf("响应类型不符合预期: %#v", resp.Data)
	}
	if data.Total != 2 || len(data.Tasks) != 2 {
		t.Fatalf("期望聚合 retry/completed 两类任务，实际 total=%d tasks=%d", data.Total, len(data.Tasks))
	}
	if data.EffectiveState != "" {
		t.Fatalf("空状态聚合不应返回单一 effectiveState，实际=%s", data.EffectiveState)
	}
	if data.Tasks[0].ID != "completed-new" {
		t.Fatalf("期望最新 completed 任务排第一，实际第一条=%+v", data.Tasks[0])
	}
}

// TestListTasksOverviewWithFiltersScansOncePerState 验证空状态带筛选时不会在聚合层重复扫描。
func TestListTasksOverviewWithFiltersScansOncePerState(t *testing.T) {
	fakeQueue := &taskOverviewFakeQueue{
		tasksByState: map[string][]types.TaskItem{
			taskStateRetry:     buildFakeTaskItems(taskStateRetry, 101),
			taskStateActive:    buildFakeTaskItems(taskStateActive, 101),
			taskStatePending:   buildFakeTaskItems(taskStatePending, 101),
			taskStateScheduled: buildFakeTaskItems(taskStateScheduled, 101),
			taskStateArchived:  buildFakeTaskItems(taskStateArchived, 101),
			taskStateCompleted: buildFakeTaskItems(taskStateCompleted, 101),
		},
	}
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.Task = fakeQueue

	logicObj := NewTaskLogic(httptest.NewRequest("GET", "/api/tasks/overview?taskName=workflow&page=1&pageSize=20", nil), svcCtx)
	resp := logicObj.ListTasksOverview(&types.ListTaskItemsOverviewReq{TaskName: "workflow", Page: 1, PageSize: 20})
	if resp == nil || !resp.IsSuccess() {
		t.Fatalf("期望任务总览筛选查询成功，实际: %+v", resp)
	}
	if len(fakeQueue.listCalls) != 6 {
		t.Fatalf("期望每个状态只查询一次，实际调用次数=%d calls=%+v", len(fakeQueue.listCalls), fakeQueue.listCalls)
	}
	for _, call := range fakeQueue.listCalls {
		if call.Page != 1 || call.PageSize != taskListScanPageSize*taskListScanMaxPages {
			t.Fatalf("期望筛选聚合使用单次大页受控查询，实际调用=%+v", call)
		}
	}
}

func buildFakeTaskItems(state string, count int) []types.TaskItem {
	items := make([]types.TaskItem, 0, count)
	for i := 0; i < count; i++ {
		taskTime := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second)
		items = append(items, types.TaskItem{
			ID:    state + "-" + taskTime.Format("150405"),
			Queue: "default",
			State: state,
		})
	}
	return items
}

// TestListTasksOverviewTimeRangeAggregatesExecutedStates 验证空状态时间范围只查询执行态，避免混入未来计划任务。
func TestListTasksOverviewTimeRangeAggregatesExecutedStates(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.Task = &taskOverviewFakeQueue{
		tasksByState: map[string][]types.TaskItem{
			taskStateScheduled: {{
				ID:            "scheduled-future",
				Queue:         "default",
				State:         taskStateScheduled,
				NextProcessAt: "2026-05-25T13:00:00Z",
			}},
			taskStateCompleted: {{
				ID:          "completed-new",
				Queue:       "default",
				State:       taskStateCompleted,
				CompletedAt: "2026-05-25T12:30:00Z",
			}},
		},
	}

	logicObj := NewTaskLogic(httptest.NewRequest("GET", "/api/tasks/overview?page=1&pageSize=20", nil), svcCtx)
	resp := logicObj.ListTasksOverview(&types.ListTaskItemsOverviewReq{
		StartTime: "2026-05-25T12:00:00Z",
		EndTime:   "2026-05-25T14:00:00Z",
		Page:      1,
		PageSize:  20,
	})
	if resp == nil || !resp.IsSuccess() {
		t.Fatalf("期望任务总览时间范围查询成功，实际: %+v", resp)
	}
	data, ok := resp.Data.(*types.TaskListOverviewResp)
	if !ok {
		t.Fatalf("响应类型不符合预期: %#v", resp.Data)
	}
	if data.Total != 1 || len(data.Tasks) != 1 || data.Tasks[0].ID != "completed-new" {
		t.Fatalf("期望空状态时间范围只返回执行态任务，实际 total=%d tasks=%+v", data.Total, data.Tasks)
	}
}

// ReloadConfig 模拟配置热加载执行器。
func (r *testConfigReloader) ReloadConfig(ctx context.Context, source string) error {
	_ = ctx
	r.triggered = true
	r.source = source
	return r.err
}

// TestGetConfigReloadStatus 确保热加载状态接口会返回当前服务上下文中的状态快照。
func TestGetConfigReloadStatus(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.UpdateHotReloadStatus(svc.HotReloadStatus{
		Enabled:                true,
		Watching:               true,
		ConfigFile:             "/etc/config.yaml",
		CheckIntervalSeconds:   5,
		ConfigVersion:          "ver-1",
		ConfigSummary:          "mode=all task=true periodic=2 user_tag=true hot_reload=true kafka=true clickhouse=true archive=false",
		LastStatus:             "success",
		LastMessage:            "配置热加载成功",
		LastTriggerSource:      "watcher",
		LastFailureCategory:    "",
		LastCheckedAt:          time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		LastReloadAt:           time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC),
		LastSuccessAt:          time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC),
		ReloadCount:            3,
		SuppressedFailureCount: 2,
	})

	logicObj := NewTaskLogic(httptest.NewRequest("GET", "/api/tasks/config-reload", nil), svcCtx)
	resp := logicObj.GetConfigReloadStatus()
	if resp == nil || resp.Data == nil {
		t.Fatal("期望返回热加载状态响应数据")
	}
	data, ok := resp.Data.(*types.TaskConfigReloadStatusResp)
	if !ok {
		t.Fatalf("响应类型不符合预期: %#v", resp.Data)
	}
	if !data.Enabled || !data.Watching {
		t.Fatalf("启用状态或监听状态不符合预期: %+v", data)
	}
	if data.ConfigVersion != "ver-1" || data.ReloadCount != 3 || data.SuppressedFailureCount != 2 {
		t.Fatalf("热加载状态返回值不符合预期: %+v", data)
	}
	if data.ConfigSummary == "" || data.LastTriggerSource != "watcher" {
		t.Fatalf("期望响应中包含配置摘要和触发来源: %+v", data)
	}
	if data.LastCheckedAt == "" || data.LastSuccessAt == "" {
		t.Fatalf("期望响应中的时间字段已格式化: %+v", data)
	}
}

// TestGetConfigReloadItemsMasksSensitiveValues 确保运行态配置项查询只返回已脱敏的敏感配置。
func TestGetConfigReloadItemsMasksSensitiveValues(t *testing.T) {
	// periodicEnabled 用于构造非 nil 的周期任务启用开关。
	periodicEnabled := true
	cfg := config.Config{
		AppID:     "site-1",
		AppKey:    "app-key-secret-value",
		JwtSecret: "jwt-super-secret-value",
		ConfigFiles: config.ConfigFilesConfig{
			Runtime: "config.d/runtime.yaml",
		},
		MySQL: config.MySQLConfig{
			WriteDataSource: "root:secret@tcp(10.10.1.2:3306)/admin?charset=utf8mb4",
			MaxOpenConns:    32,
			Debug:           true,
		},
		Redis: config.RedisConfig{
			Addrs:    []string{"10.10.2.3:6379"},
			AddrMap:  map[string]string{"10.10.2.3:6379": "10.10.2.4:6379"},
			Password: "redis-secret-value",
			DB:       3,
		},
		Kafka: config.KafkaConfig{
			Enabled: true,
			Brokers: []string{"kafka-1.internal:9092"},
			Topics:  config.KafkaTopicsConfig{UserTag: "user-tag-topic"},
		},
		Security: config.SecurityConfig{
			SecretKey: config.SecuritySecretKeyConfig{
				KeyVersion:          "v202501",
				AESKey:              "1234567890123456",
				AESIV:               "abcdef1234567890",
				RSAPrivateKeyServer: "-----BEGIN PRIVATE KEY-----secret-----END PRIVATE KEY-----",
			},
		},
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:        "admin_log",
					Enabled:     true,
					Database:    "admin",
					TableName:   "admin_log",
					HotKeepDays: 32,
				},
			},
		},
		Task: config.TaskQueueConfig{
			DefaultQueue: "maintenance",
			Periodic: []config.TaskPeriodicConfig{
				{
					Enabled:    &periodicEnabled,
					Name:       "archive-admin-log-hourly",
					Cron:       "5 * * * *",
					Workflow:   "archive.run",
					Queue:      "maintenance",
					Targets:    []string{"admin_log"},
					ShardTotal: 2,
				},
			},
		},
		Workflows: config.WorkflowsConfig{
			UserTag: config.UserTagConfig{
				Enabled:           true,
				DefaultShardTotal: 10,
			},
		},
	}
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{})
	svcCtx.UpdateHotReloadStatus(svc.HotReloadStatus{
		ConfigFile:           "/etc/admin-cron/config.yaml",
		ConfigVersion:        "ver-runtime",
		LastStatus:           "success",
		LastTriggerSource:    "watcher",
		RestartRequired:      true,
		RestartReason:        "以下配置已变更，需重启进程才能完全生效: workflows.user_tag.enabled",
		LastReloadAt:         time.Unix(1_700_000_000, 0),
		LastSuccessAt:        time.Unix(1_700_000_001, 0),
		CheckIntervalSeconds: 5,
	})
	logicObj := NewTaskLogic(httptest.NewRequest("GET", "/api/tasks/config-reload/items", nil), svcCtx)

	mysqlData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword:  "mysql.write_data_source",
		Page:     1,
		PageSize: 20,
	}))
	if mysqlData.Total != 1 || len(mysqlData.Items) != 1 {
		t.Fatalf("期望只命中 MySQL 写库 DSN，实际: %+v", mysqlData)
	}
	if mysqlData.TotalItems == 0 || mysqlData.SensitiveTotal == 0 || mysqlData.SnapshotYAML == "" {
		t.Fatalf("期望返回完整配置项统计和脱敏 YAML 快照: %+v", mysqlData)
	}
	if mysqlData.Source.Source != "runtime_snapshot" {
		t.Fatalf("期望配置项来源为运行态快照: %+v", mysqlData.Source)
	}
	if mysqlData.Source.RuntimeFile != "/etc/admin-cron/config.d/runtime.yaml" {
		t.Fatalf("期望返回按主配置解析后的 runtime.yaml 路径: %+v", mysqlData.Source)
	}
	if !mysqlData.Source.RestartRequired || !strings.Contains(mysqlData.Source.RestartReason, "workflows.user_tag.enabled") {
		t.Fatalf("期望配置项来源携带重启原因，实际: %+v", mysqlData.Source)
	}
	mysqlItem := mysqlData.Items[0]
	if !mysqlItem.Sensitive || !strings.Contains(mysqlItem.Value, "****") {
		t.Fatalf("MySQL DSN 应脱敏展示，实际: %+v", mysqlItem)
	}
	for _, raw := range []string{"root:secret", "10.10.1.2", cfg.MySQL.WriteDataSource} {
		if strings.Contains(mysqlItem.Value, raw) {
			t.Fatalf("MySQL DSN 泄露原始敏感值 %q: %+v", raw, mysqlItem)
		}
		if strings.Contains(mysqlData.SnapshotYAML, raw) {
			t.Fatalf("脱敏 YAML 快照泄露原始敏感值 %q:\n%s", raw, mysqlData.SnapshotYAML)
		}
		if strings.Contains(mysqlData.RuntimeYAML, raw) {
			t.Fatalf("runtime.yaml 视图泄露原始敏感值 %q:\n%s", raw, mysqlData.RuntimeYAML)
		}
	}
	if !strings.Contains(mysqlData.SnapshotYAML, "mysql:") || !strings.Contains(mysqlData.SnapshotYAML, "write_data_source:") {
		t.Fatalf("脱敏 YAML 快照应保留配置结构，实际:\n%s", mysqlData.SnapshotYAML)
	}
	for _, expectedKey := range []string{"aes_key:", "aes_iv:", "rsa_private_key_server:"} {
		if !strings.Contains(mysqlData.SnapshotYAML, expectedKey) {
			t.Fatalf("脱敏 YAML 快照应保留原始配置键 %q，实际:\n%s", expectedKey, mysqlData.SnapshotYAML)
		}
	}
	for _, maskedKey := range []string{"a****y:", "a****v:", "rsa_****rver:"} {
		if strings.Contains(mysqlData.SnapshotYAML, maskedKey) {
			t.Fatalf("脱敏 YAML 快照不应脱敏配置键 %q，实际:\n%s", maskedKey, mysqlData.SnapshotYAML)
		}
	}
	for _, rawSecret := range []string{"1234567890123456", "abcdef1234567890", "BEGIN PRIVATE KEY"} {
		if strings.Contains(mysqlData.SnapshotYAML, rawSecret) {
			t.Fatalf("脱敏 YAML 快照泄露秘钥原始值 %q:\n%s", rawSecret, mysqlData.SnapshotYAML)
		}
	}
	for _, expected := range []string{"archive_jobs:", "task_periodic:", "workflows:", "archive-admin-log-hourly", "admin_log"} {
		if !strings.Contains(mysqlData.RuntimeYAML, expected) {
			t.Fatalf("runtime.yaml 视图应保留运行期外部配置结构 %q，实际:\n%s", expected, mysqlData.RuntimeYAML)
		}
	}

	redisData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword:  "redis.addr_map",
		Page:     1,
		PageSize: 20,
	}))
	if redisData.Total == 0 {
		t.Fatal("期望 Redis 地址映射配置可被查询")
	}
	for _, item := range redisData.Items {
		if !item.Sensitive {
			t.Fatalf("Redis 地址映射应标记为敏感项: %+v", item)
		}
		for _, raw := range []string{"10.10.2.3", "10.10.2.4"} {
			if strings.Contains(item.Value, raw) {
				t.Fatalf("Redis 地址映射不应在展示值中泄露原始地址 %q: %+v", raw, item)
			}
		}
	}
	if !strings.Contains(redisData.SnapshotYAML, "10.10.2.3:6379") {
		t.Fatalf("Redis 地址映射 key 应原样展示，实际:\n%s", redisData.SnapshotYAML)
	}

	secretData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword: "redis-secret-value",
	}))
	if secretData.Total != 0 {
		t.Fatalf("敏感原文不能参与搜索命中，实际: %+v", secretData)
	}

	brokerData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword: "kafka.brokers",
	}))
	var brokerItem *types.TaskConfigItem
	for i := range brokerData.Items {
		if brokerData.Items[i].Path == "kafka.brokers[0]" {
			brokerItem = &brokerData.Items[i]
			break
		}
	}
	if brokerItem == nil || !brokerItem.Sensitive || strings.Contains(brokerItem.Value, "kafka-1.internal") {
		t.Fatalf("Kafka broker 地址应脱敏展示: %+v", brokerData)
	}

	topicData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword: "kafka.topics.user_tag",
	}))
	if topicData.Total != 1 || topicData.Items[0].Sensitive || topicData.Items[0].Value != "user-tag-topic" {
		t.Fatalf("Kafka Topic 属于非敏感业务配置，应原样展示: %+v", topicData)
	}
	if !strings.Contains(topicData.SnapshotYAML, "user-tag-topic") {
		t.Fatalf("非敏感 Topic 应在 YAML 快照中原样展示:\n%s", topicData.SnapshotYAML)
	}

	queueData := mustConfigItemQueryResp(t, logicObj.GetConfigReloadItems(&types.TaskConfigItemQueryReq{
		Keyword: "task.default_queue",
	}))
	if queueData.Total != 1 || queueData.Items[0].Sensitive || queueData.Items[0].Value != "maintenance" {
		t.Fatalf("任务默认队列属于非敏感配置，应原样展示: %+v", queueData)
	}
}

// TestRunConfigReload 确保手动触发配置重载会调用执行器并返回最新状态快照。
func TestRunConfigReload(t *testing.T) {
	reloader := &testConfigReloader{}
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.ConfigReload = reloader
	svcCtx.UpdateHotReloadStatus(svc.HotReloadStatus{
		Enabled:              true,
		Watching:             true,
		ConfigFile:           "/etc/config.yaml",
		CheckIntervalSeconds: 5,
		ConfigVersion:        "ver-2",
		ConfigSummary:        "mode=all task=true periodic=1 user_tag=true hot_reload=true kafka=true clickhouse=true archive=true",
		LastStatus:           "success",
		LastMessage:          "配置热加载成功",
		LastTriggerSource:    "manual_api",
	})

	logicObj := NewTaskLogic(httptest.NewRequest("POST", "/api/tasks/config-reload", nil), svcCtx)
	resp := logicObj.RunConfigReload()
	if resp == nil || resp.Data == nil {
		t.Fatal("期望返回手动热加载响应数据")
	}
	if !reloader.triggered || reloader.source != "manual_api" {
		t.Fatalf("期望以 manual_api 触发重载器，实际为 %+v", reloader)
	}
	data, ok := resp.Data.(*types.TaskConfigReloadStatusResp)
	if !ok {
		t.Fatalf("响应类型不符合预期: %#v", resp.Data)
	}
	if data.ConfigVersion != "ver-2" || data.LastTriggerSource != "manual_api" {
		t.Fatalf("手动热加载响应不符合预期: %+v", data)
	}
}

// mustConfigItemQueryResp 断言配置项查询成功并返回强类型数据。
func mustConfigItemQueryResp(t *testing.T, resp *types.BizResult) *types.TaskConfigItemQueryResp {
	t.Helper()
	if resp == nil || !resp.IsSuccess() || resp.Data == nil {
		t.Fatalf("期望配置项查询成功，实际: %+v", resp)
	}
	data, ok := resp.Data.(*types.TaskConfigItemQueryResp)
	if !ok {
		t.Fatalf("配置项响应类型不符合预期: %#v", resp.Data)
	}
	return data
}
