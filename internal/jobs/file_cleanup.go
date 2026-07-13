package jobs

import (
	"context"
	"encoding/json"

	filelogic "admin/internal/logic/file"
	taskruntime "admin/internal/task/runtime"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// FileUploadCleanupPluginName 表示上传对象清理任务插件名称。
	FileUploadCleanupPluginName = "file_upload_cleanup"
)

// FileUploadCleanupPlugin 创建上传对象清理任务插件。
func FileUploadCleanupPlugin() taskruntime.Plugin {
	return taskruntime.NewPluginFunc(FileUploadCleanupPluginName, func(runtime *taskruntime.Runtime) error {
		if runtime == nil || runtime.ServiceContext() == nil {
			return nil
		}
		svcCtx := runtime.ServiceContext()
		return runtime.RegisterHandler(types.FileUploadCleanupTaskType, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			var payload types.FileUploadCleanupTaskPayload
			if err := json.Unmarshal(task.Payload(), &payload); err != nil {
				return errors.Wrap(err, "解析上传对象清理任务载荷失败")
			}
			return filelogic.NewFileTransferLogicWithContext(ctx, svcCtx).CleanupUploadedObject(&payload)
		}))
	})
}
