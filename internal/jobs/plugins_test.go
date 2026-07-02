package jobs

import (
	"context"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/jobs/taskreport"
	usertagtask "admin/internal/jobs/usertag/task"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	taskruntime "admin/internal/task/runtime"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// TestTaskReportPluginRegistersDailySummaryWorkflow 验证任务运行日报插件会注册日报工作流。
func TestTaskReportPluginRegistersDailySummaryWorkflow(t *testing.T) {
	runtime, manager, cleanup := newTestRuntime(t, config.Config{})
	defer cleanup()

	if err := runtime.RegisterPlugins(TaskReportPlugin()); err != nil {
		t.Fatalf("注册任务运行日报插件失败: %v", err)
	}
	if !workflowRegistered(manager, taskreport.WorkflowNameDailySummary) {
		t.Fatalf("未注册任务运行日报工作流 %s，当前清单=%+v", taskreport.WorkflowNameDailySummary, manager.ListRegisteredWorkflows(context.Background()))
	}
	if !taskTypeRegistered(manager, taskreport.TaskTypeDailySummary) {
		t.Fatalf("未注册任务运行日报 handler %s，当前清单=%+v", taskreport.TaskTypeDailySummary, manager.ListRegisteredTaskTypes(context.Background()))
	}
}

// TestUserTagPluginRegistersMaintenanceWorkflows 验证用户标签插件会注册独立维护工作流。
func TestUserTagPluginRegistersMaintenanceWorkflows(t *testing.T) {
	svcCtx := svc.NewServiceContext(userTagEnabledConfig(), svc.Dependencies{})
	_, manager, cleanup := newTestRuntime(t, userTagEnabledConfig())
	defer cleanup()

	if _, err := taskruntime.Register(svcCtx, manager, UserTagPlugin()); err != nil {
		t.Fatalf("注册用户标签插件失败: %v", err)
	}
	registered := manager.ListRegisteredWorkflows(context.Background())
	want := map[string]bool{
		usertagtask.WorkflowNameUserTagEventOutboxRetryScan: false,
		usertagtask.WorkflowNameUserTagRuntimeCleanup:       false,
	}
	for _, item := range registered {
		if _, ok := want[item.Name]; ok {
			want[item.Name] = true
		}
	}
	for workflowName, ok := range want {
		if !ok {
			t.Fatalf("未注册用户标签维护工作流 %s，当前清单=%+v", workflowName, registered)
		}
	}
}

// TestTaskMetadataPluginRegistersDailySummaryMeta 验证任务展示元数据由 jobs 清单注册。
func TestTaskMetadataPluginRegistersDailySummaryMeta(t *testing.T) {
	runtime, manager, cleanup := newTestRuntime(t, config.Config{})
	defer cleanup()

	if err := runtime.RegisterPlugins(TaskMetadataPlugin()); err != nil {
		t.Fatalf("注册任务展示元数据插件失败: %v", err)
	}
	if err := manager.RegisterHandler(taskreport.TaskTypeDailySummary, asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return nil
	})); err != nil {
		t.Fatalf("注册测试任务处理器失败: %v", err)
	}
	item, ok := registeredTaskTypeItem(manager, taskreport.TaskTypeDailySummary)
	if !ok {
		t.Fatalf("未找到任务类型 %s，当前清单=%+v", taskreport.TaskTypeDailySummary, manager.ListRegisteredTaskTypes(context.Background()))
	}
	if !strings.Contains(item.PayloadExample, "windowStart") || item.Description == "" {
		t.Fatalf("任务运行日报展示元数据不完整: %+v", item)
	}
}

func newTestRuntime(t *testing.T, cfg config.Config) (*taskruntime.Runtime, *taskqueue.Manager, func()) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	taskCfg := cfg.Task
	taskCfg.Enabled = true
	taskCfg.AppID = "1"
	taskCfg.DefaultQueue = taskqueue.QueueDefault
	taskCfg.Queues = map[string]int{
		taskqueue.QueueDefault:     1,
		taskqueue.QueueMaintenance: 1,
	}
	cfg.Task = taskCfg
	manager := taskqueue.New(taskCfg, client)
	runtime := taskruntime.NewRuntime(svc.NewServiceContext(cfg, svc.Dependencies{}), manager)
	cleanup := func() {
		_ = manager.Stop(context.Background())
		_ = client.Close()
		server.Close()
	}
	return runtime, manager, cleanup
}

func userTagEnabledConfig() config.Config {
	return config.Config{
		Workflows: config.WorkflowsConfig{
			UserTag: config.UserTagConfig{Enabled: true},
		},
	}
}

func taskTypeRegistered(manager *taskqueue.Manager, taskType string) bool {
	_, ok := registeredTaskTypeItem(manager, taskType)
	return ok
}

func workflowRegistered(manager *taskqueue.Manager, workflowName string) bool {
	for _, item := range manager.ListRegisteredWorkflows(context.Background()) {
		if item.Name == workflowName {
			return true
		}
	}
	return false
}

func registeredTaskTypeItem(manager *taskqueue.Manager, taskType string) (types.TaskTypeRegistryItem, bool) {
	for _, candidate := range manager.ListRegisteredTaskTypes(context.Background()) {
		if candidate.TaskType != taskType {
			continue
		}
		return candidate, true
	}
	return types.TaskTypeRegistryItem{}, false
}
