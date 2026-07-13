package export

import (
	"context"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/hibiken/asynq"
)

// exportRuntimeStub 记录导出插件注册的任务处理器。
type exportRuntimeStub struct {
	svcCtx   *svc.ServiceContext      // svcCtx 表示插件读取的运行时依赖。
	handlers map[string]asynq.Handler // handlers 保存已注册任务处理器。
}

// ServiceContext 返回测试服务上下文。
func (r *exportRuntimeStub) ServiceContext() *svc.ServiceContext {
	return r.svcCtx
}

// RegisterHandler 记录任务处理器。
func (r *exportRuntimeStub) RegisterHandler(pattern string, handler asynq.Handler) error {
	if r.handlers == nil {
		r.handlers = make(map[string]asynq.Handler)
	}
	r.handlers[pattern] = handler
	return nil
}

// TestSetupRegistersEveryExportHandler 验证导出和清理任务均由插件统一注册。
func TestSetupRegistersEveryExportHandler(t *testing.T) {
	runtime := &exportRuntimeStub{svcCtx: svc.NewServiceContext(config.Config{}, svc.Dependencies{})}
	if err := Setup(runtime); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	wantTypes := []string{
		types.AdminExportTaskType,
		types.UserExportTaskType,
		types.AdminExportCleanupTaskType,
		types.UserExportCleanupTaskType,
		types.SysConfigExcelBackupCleanupTaskType,
	}
	if len(runtime.handlers) != len(wantTypes) {
		t.Fatalf("registered handlers = %d, want %d", len(runtime.handlers), len(wantTypes))
	}
	for _, taskType := range wantTypes {
		if runtime.handlers[taskType] == nil {
			t.Errorf("handler %q was not registered", taskType)
		}
	}
}

// TestExportHandlersRejectMalformedPayload 验证所有导出入口在调用业务逻辑前拒绝非法 JSON。
func TestExportHandlersRejectMalformedPayload(t *testing.T) {
	runtime := &exportRuntimeStub{svcCtx: svc.NewServiceContext(config.Config{}, svc.Dependencies{})}
	if err := Setup(runtime); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	for taskType, handler := range runtime.handlers {
		t.Run(taskType, func(t *testing.T) {
			task := asynq.NewTask(taskType, []byte("{"))
			if err := handler.ProcessTask(context.Background(), task); err == nil {
				t.Fatal("malformed export payload should be rejected")
			}
		})
	}
}
