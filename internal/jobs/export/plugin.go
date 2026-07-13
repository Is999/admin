package export

import (
	"context"
	"encoding/json"

	adminlogic "admin/internal/logic/admin"
	userlogic "admin/internal/logic/user"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// PluginName 是 Excel 导出任务插件名称，供 taskruntime 统一装配时保持唯一。
	PluginName = "excel_export"
)

// Runtime 描述 Excel 导出任务注册所需的最小运行时能力。
type Runtime interface {
	// ServiceContext 返回任务处理器共享的服务上下文。
	ServiceContext() *svc.ServiceContext
	// RegisterHandler 注册 Asynq 任务处理器。
	RegisterHandler(pattern string, handler asynq.Handler) error
}

// Setup 注册列表异步导出任务处理器。
func Setup(runtime Runtime) error {
	if runtime == nil || runtime.ServiceContext() == nil {
		return nil
	}
	svcCtx := runtime.ServiceContext()
	if err := runtime.RegisterHandler(types.AdminExportTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload types.AdminExportTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析管理员导出任务载荷失败")
		}
		return adminlogic.NewAdminExportLogicWithContext(ctx, svcCtx).RunExportTask(&payload)
	})); err != nil {
		return errors.Tag(err)
	}
	if err := runtime.RegisterHandler(types.UserExportTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload types.UserExportTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析前台用户导出任务载荷失败")
		}
		return userlogic.NewLogicWithContext(ctx, svcCtx).RunExportTask(&payload)
	})); err != nil {
		return errors.Tag(err)
	}
	if err := runtime.RegisterHandler(types.AdminExportCleanupTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload types.AdminExportCleanupTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析管理员导出文件清理任务载荷失败")
		}
		return adminlogic.NewAdminExportLogicWithContext(ctx, svcCtx).CleanupExportFile(&payload)
	})); err != nil {
		return errors.Tag(err)
	}
	return runtime.RegisterHandler(types.UserExportCleanupTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload types.UserExportCleanupTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析前台用户导出文件清理任务载荷失败")
		}
		return userlogic.NewLogicWithContext(ctx, svcCtx).CleanupExportFiles(&payload)
	}))
}
