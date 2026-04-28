package export

import (
	"context"
	"encoding/json"

	adminlogic "admin/internal/logic/admin"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// PluginName 是管理员导出任务插件名称，供 taskruntime 统一装配时保持唯一。
	PluginName = "admin_export"
)

// Runtime 描述管理员导出任务注册所需的最小运行时能力。
type Runtime interface {
	// ServiceContext 返回任务处理器共享的服务上下文。
	ServiceContext() *svc.ServiceContext
	// RegisterHandler 注册 Asynq 任务处理器。
	RegisterHandler(pattern string, handler asynq.Handler) error
}

// Setup 注册管理员列表异步导出任务处理器。
func Setup(runtime Runtime) error {
	if runtime == nil || runtime.ServiceContext() == nil {
		return nil
	}
	svcCtx := runtime.ServiceContext()
	// 注册 admin:export 任务类型：后台异步生成管理员列表 Excel，任务载荷来自管理端导出请求的筛选条件。
	return runtime.RegisterHandler(types.AdminExportTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload types.AdminExportTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析管理员导出任务载荷失败")
		}
		return adminlogic.NewAdminExportLogicWithContext(ctx, svcCtx).RunExportTask(&payload)
	}))
}
