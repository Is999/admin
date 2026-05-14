package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"admin/internal/jobs/archive"
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
)

const (
	// PluginName 是通用归档任务插件名称，供 taskruntime 包装注册时保持唯一。
	PluginName = "archive"
)

// archiveExecutePayload 描述单个归档执行节点的任务负载。
type archiveExecutePayload struct {
	taskqueue.WorkflowTaskMeta          // 工作流调度注入的任务上下文
	Targets                    []string `json:"targets,omitempty"` // 当前工作流要求处理的归档目标列表
}

// Runtime 描述归档任务注册所需的最小运行时能力。
// 该接口让 archive/task 包承载任务细节，同时避免反向依赖 taskruntime 造成包循环。
type Runtime interface {
	// ServiceContext 返回任务处理器共享的服务上下文。
	ServiceContext() *svc.ServiceContext
	// RegisterHandler 注册 Asynq 任务处理器。
	RegisterHandler(pattern string, handler asynq.Handler) error
	// RegisterWorkflow 注册归档工作流定义。
	RegisterWorkflow(def *taskqueue.WorkflowDefinition) error
}

// Setup 注册通用归档任务 handler 和工作流定义。
// 后续新增 archive 定时工作流、独立维护任务或节点处理器，统一在 internal/jobs/archive/task 下扩展。
func Setup(runtime Runtime) error {
	if runtime == nil || runtime.ServiceContext() == nil {
		return nil
	}
	// 归档能力由 archive.enabled 控制；关闭时不注册 handler/workflow，避免后台任务误触发热表搬迁。
	if !runtime.ServiceContext().CurrentConfig().Archive.Enabled {
		return nil
	}
	// 注册 archive:execute 任务类型：执行热表归档、删除和历史表治理。
	if err := runtime.RegisterHandler(archive.TaskTypeExecute, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload archiveExecutePayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return errors.Wrap(err, "解析归档任务载荷失败")
		}
		// workerID 固定带上工作流实例、节点和分片信息，便于 checkpoint 追踪当前持有者。
		workerID := fmt.Sprintf("%s:%s:%d/%d", payload.WorkflowID, payload.WorkflowNode, payload.ShardIndex, payload.ShardTotal)
		return archive.NewService(runtime.ServiceContext(), archive.WithControlDatabase(svc.DatabaseMain)).RunTargets(ctx, payload.Targets, workerID)
	})); err != nil {
		return errors.Tag(err)
	}
	return runtime.RegisterWorkflow(archiveWorkflow())
}

// archiveWorkflow 定义归档工作流；真正的多 worker 并行由多个分片节点共同抢占区间实现。
func archiveWorkflow() *taskqueue.WorkflowDefinition {
	return &taskqueue.WorkflowDefinition{
		Name:         archive.WorkflowNameRun,
		Description:  "通用归档工作流，负责按 watermark 与 checkpoint 执行冷热数据搬迁",
		DefaultQueue: taskqueue.QueueMaintenance,
		Nodes: map[string]*taskqueue.WorkflowNodeDefinition{
			"archive.execute": {
				Name:             "archive.execute",
				TaskType:         archive.TaskTypeExecute,
				Queue:            taskqueue.QueueMaintenance,
				MaxRetry:         3,
				Timeout:          2 * time.Hour,
				SupportsSharding: true,
				BuildPayload: func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
					// 归档执行节点直接复用工作流 targets，后续由归档服务决定具体跑哪些任务。
					return json.Marshal(archiveExecutePayload{
						WorkflowTaskMeta: taskqueue.WorkflowTaskMeta{
							WorkflowID:   spec.WorkflowID,
							WorkflowName: spec.Name,
							WorkflowNode: node.Name,
							ShardIndex:   shardIndex,
							ShardTotal:   shardTotal,
						},
						Targets: trimTargets(spec.Targets),
					})
				},
			},
			"finalize": {
				Name:      "finalize",
				TaskType:  taskqueue.TypeWorkflowNoop,
				Queue:     taskqueue.QueueMaintenance,
				DependsOn: []string{"archive.execute"},
				Timeout:   30 * time.Second,
				BuildPayload: func(spec taskqueue.WorkflowStartSpec, node *taskqueue.WorkflowNodeDefinition, shardIndex, shardTotal int) ([]byte, error) {
					// finalize 节点仅承担工作流收尾职责，不再执行额外业务逻辑。
					return json.Marshal(taskqueue.NoopPayload{
						WorkflowTaskMeta: taskqueue.WorkflowTaskMeta{
							WorkflowID:   spec.WorkflowID,
							WorkflowName: spec.Name,
							WorkflowNode: node.Name,
							ShardIndex:   shardIndex,
							ShardTotal:   shardTotal,
						},
						Note: "archive workflow finished",
					})
				},
			},
		},
	}
}

// trimTargets 归一化工作流目标列表，去掉空白项，避免后续归档服务重复判断无效目标。
func trimTargets(targets []string) []string {
	items := make([]string, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		items = append(items, target)
	}
	return items
}
