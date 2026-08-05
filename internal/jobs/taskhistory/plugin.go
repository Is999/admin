package taskhistory

import (
	"admin/internal/svc"
	taskstore "admin/internal/task/history"
	taskqueue "admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
)

// PluginName 是任务终态历史异步落库插件名称。
const PluginName = "task_history"

// Runtime 描述终态历史插件装配所需的最小能力。
type Runtime interface {
	ServiceContext() *svc.ServiceContext
	Manager() *taskqueue.Manager
}

// Setup 绑定主库历史存储；收集器只会在 Worker 生命周期启动。
func Setup(runtime Runtime) error {
	if runtime == nil || runtime.ServiceContext() == nil || runtime.Manager() == nil {
		return nil
	}
	manager := runtime.Manager()
	if !manager.CurrentConfig().History.EnabledOrDefault() {
		return nil
	}
	svcCtx := runtime.ServiceContext()
	store := taskstore.New(
		manager.CurrentConfig().AppID,
		svcCtx.ReadDB(svc.DatabaseMain),
		svcCtx.WriteDB(svc.DatabaseMain),
	)
	if store == nil {
		return errors.Errorf("任务历史已启用，但主库或应用命名空间不可用")
	}
	manager.AttachHistorySink(store)
	return nil
}
