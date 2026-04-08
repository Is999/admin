package taskruntime

import (
	"context"
	"encoding/json"

	"admin_cron/internal/logic"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

// NewAdminExportPlugin 创建管理员列表异步导出任务插件。
func NewAdminExportPlugin() Plugin {
	return NewPluginFunc("admin_export", func(runtime *Runtime) error {
		if runtime == nil || runtime.ServiceContext() == nil {
			return nil
		}
		// 注册 admin:export 任务类型：后台异步生成管理员列表 Excel，任务载荷来自管理端导出请求的筛选条件。
		return runtime.RegisterHandler(types.AdminExportTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			var payload types.AdminExportTaskPayload
			if err := json.Unmarshal(task.Payload(), &payload); err != nil {
				return errors.Wrap(err, "解析管理员导出任务载荷失败")
			}
			return logic.NewAdminExportLogicWithContext(ctx, runtime.ServiceContext()).RunExportTask(&payload)
		}))
	})
}
