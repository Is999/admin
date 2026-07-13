package task

import (
	"context"
	"encoding/json"
	"testing"

	"admin/internal/config"
	"admin/internal/jobs/archive"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"

	"github.com/hibiken/asynq"
)

// pluginRuntimeStub 记录归档插件注册结果，避免单元测试启动真实队列。
type pluginRuntimeStub struct {
	svcCtx    *svc.ServiceContext                      // svcCtx 表示插件读取的运行时依赖。
	handlers  map[string]asynq.Handler                 // handlers 保存已注册任务处理器。
	workflows map[string]*taskqueue.WorkflowDefinition // workflows 保存已注册工作流。
}

// ServiceContext 返回测试服务上下文。
func (r *pluginRuntimeStub) ServiceContext() *svc.ServiceContext {
	return r.svcCtx
}

// RegisterHandler 记录任务处理器。
func (r *pluginRuntimeStub) RegisterHandler(pattern string, handler asynq.Handler) error {
	if r.handlers == nil {
		r.handlers = make(map[string]asynq.Handler)
	}
	r.handlers[pattern] = handler
	return nil
}

// RegisterWorkflow 记录工作流定义。
func (r *pluginRuntimeStub) RegisterWorkflow(def *taskqueue.WorkflowDefinition) error {
	if r.workflows == nil {
		r.workflows = make(map[string]*taskqueue.WorkflowDefinition)
	}
	r.workflows[def.Name] = def
	return nil
}

// TestSetupHonorsArchiveSwitchAndRegistersWorkflow 验证归档开关控制插件注册，并锁定任务与工作流入口。
func TestSetupHonorsArchiveSwitchAndRegistersWorkflow(t *testing.T) {
	disabled := &pluginRuntimeStub{svcCtx: svc.NewServiceContext(config.Config{}, svc.Dependencies{})}
	if err := Setup(disabled); err != nil {
		t.Fatalf("Setup(disabled) error = %v", err)
	}
	if len(disabled.handlers) != 0 || len(disabled.workflows) != 0 {
		t.Fatalf("disabled archive registered handlers=%d workflows=%d", len(disabled.handlers), len(disabled.workflows))
	}

	enabled := &pluginRuntimeStub{svcCtx: svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{Enabled: true},
	}, svc.Dependencies{})}
	if err := Setup(enabled); err != nil {
		t.Fatalf("Setup(enabled) error = %v", err)
	}
	if enabled.handlers[archive.TaskTypeExecute] == nil {
		t.Fatalf("archive handler %q was not registered", archive.TaskTypeExecute)
	}
	workflow := enabled.workflows[archive.WorkflowNameRun]
	if workflow == nil {
		t.Fatalf("archive workflow %q was not registered", archive.WorkflowNameRun)
	}
	if workflow.Nodes["archive.execute"] == nil || workflow.Nodes["finalize"] == nil {
		t.Fatalf("archive workflow nodes are incomplete: %+v", workflow.Nodes)
	}
}

// TestArchiveWorkflowBuildPayloadNormalizesTargets 验证归档节点负载保留编排元数据并过滤空目标。
func TestArchiveWorkflowBuildPayloadNormalizesTargets(t *testing.T) {
	workflow := archiveWorkflow()
	node := workflow.Nodes["archive.execute"]
	body, err := node.BuildPayload(taskqueue.WorkflowStartSpec{
		WorkflowID: "workflow-1",
		Name:       workflow.Name,
		Targets:    []string{" user_log ", "", "admin_log"},
	}, node, 2, 4)
	if err != nil {
		t.Fatalf("BuildPayload() error = %v", err)
	}

	var payload archiveExecutePayload
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.WorkflowID != "workflow-1" || payload.ShardIndex != 2 || payload.ShardTotal != 4 {
		t.Fatalf("workflow metadata = %+v", payload.WorkflowTaskMeta)
	}
	if len(payload.Targets) != 2 || payload.Targets[0] != "user_log" || payload.Targets[1] != "admin_log" {
		t.Fatalf("normalized targets = %+v", payload.Targets)
	}
}

// TestArchiveHandlerRejectsMalformedPayload 验证非法任务负载在进入归档服务前即失败。
func TestArchiveHandlerRejectsMalformedPayload(t *testing.T) {
	runtime := &pluginRuntimeStub{svcCtx: svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{Enabled: true},
	}, svc.Dependencies{})}
	if err := Setup(runtime); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	handler := runtime.handlers[archive.TaskTypeExecute]
	if err := handler.ProcessTask(context.Background(), asynq.NewTask(archive.TaskTypeExecute, []byte("{"))); err == nil {
		t.Fatal("malformed archive payload should be rejected")
	}
}
