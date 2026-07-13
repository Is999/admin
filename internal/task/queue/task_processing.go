package taskqueue

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"admin/helper"
	"admin/internal/infra/loggerx"
	"admin/internal/requestctx"
	taskstats "admin/internal/task/stats"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// aggregateTasks 根据任务分组查找聚合器，并把同组短任务合并成批任务。
func (m *Manager) aggregateTasks(group string, tasks []*asynq.Task) *asynq.Task {
	m.mu.RLock()
	aggregator := m.aggregates[strings.TrimSpace(group)]
	m.mu.RUnlock()
	if aggregator == nil {
		return nil
	}
	return aggregator(tasks)
}

// handleTaskError 统一记录任务失败日志，并在终态失败时回写工作流节点状态。
func (m *Manager) handleTaskError(ctx context.Context, task *asynq.Task, err error) {
	// ErrorHandler 拿到的上下文不保证包含业务中间件补齐后的链路字段，
	// 这里统一从任务头重建一次请求元数据，确保失败日志也能稳定带出 trace/workflow 信息。
	ctx = m.enrichTaskContext(ctx, task)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	taskID, _ := asynq.GetTaskID(ctx)
	queue, _ := asynq.GetQueueName(ctx)
	meta := workflowTaskMetaFromTask(task)
	if meta.WorkflowID == "" && task != nil && task.Type() == TypeWorkflowTrigger {
		meta.WorkflowID = taskID
	}
	fields := []logx.LogField{
		logx.Field("retried", retried),
		logx.Field("max_retry", maxRetry),
	}
	fields = append(fields, taskLogFields(task)...)
	fields = append(fields, taskStatsLogFields(m.readTaskRuntime(ctx, queue, taskID).ExecutionTrace)...)
	loggerx.Errorw(ctx, "任务处理失败", err, fields...)
	if taskWillArchive(ctx, err) {
		var markErr error
		if errors.Is(err, ErrWorkflowDispatch) {
			markErr = m.failWorkflowDispatchWithFinalContext(ctx, task, meta, err)
		} else {
			markErr = m.markTaskFailureWithFinalContext(ctx, meta, err)
		}
		if markErr != nil {
			loggerx.Errorw(ctx, "任务失败状态回写失败", markErr, fields...)
		}
		m.runTaskFinalFailureHooks(ctx, task, meta, err, fields)
	}
}

// failWorkflowDispatchWithFinalContext 在编排技术重试耗尽后终止实例并释放其唯一键。
func (m *Manager) failWorkflowDispatchWithFinalContext(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error) error {
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	return m.failWorkflowDispatch(writeCtx, task, meta, runErr)
}

// markTaskFailureWithFinalContext 使用短后台 ctx 回写工作流失败状态。
// 该方法只负责终态兜底写 Redis，保留原业务 ctx 的链路字段，但不继承其取消信号。
func (m *Manager) markTaskFailureWithFinalContext(ctx context.Context, meta WorkflowTaskMeta, err error) error {
	writeCtx, cancel := m.taskFinalWriteContext(ctx)
	defer cancel()
	_, markErr := m.markTaskFailure(writeCtx, meta, err)
	return errors.Tag(markErr)
}

// runTaskFinalFailureHooks 在任务终态失败后触发业务清理钩子。
// 这里使用短后台 ctx，避免业务 ctx 超时后租约释放、失败兜底状态等收尾动作继续复用已取消上下文。
func (m *Manager) runTaskFinalFailureHooks(ctx context.Context, task *asynq.Task, meta WorkflowTaskMeta, runErr error, fields []logx.LogField) {
	m.mu.RLock()
	hooks := append([]TaskFinalFailureHook(nil), m.finalFailureHooks...)
	m.mu.RUnlock()
	if len(hooks) == 0 {
		return
	}
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hookCtx, cancel := m.taskFinalWriteContext(ctx)
		if err := hook(hookCtx, task, meta, runErr); err != nil {
			loggerx.Errorw(hookCtx, "任务终态失败清理钩子执行失败", err, fields...)
		}
		cancel()
	}
}

// enrichTaskContext 从 Asynq 任务头和运行时上下文中重建统一日志字段。
// 任务失败回调和正常处理链都复用该方法，保证 trace、task、workflow、locale、user 信息输出一致。
func (m *Manager) enrichTaskContext(ctx context.Context, task *asynq.Task) context.Context {
	// 兜底补一个基础 context，避免极端场景传入 nil 导致后续链路字段无法写入。
	if ctx == nil {
		ctx = context.Background()
	}
	// 统一初始化请求元数据，保证后续日志字段都写入同一份上下文容器。
	ctx, _ = requestctx.New(ctx)
	if task == nil {
		return loggerx.BindContext(ctx)
	}

	// 先把任务头中的远端 trace 上下文提取出来，便于失败日志也能挂回原链路。
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(task.Headers()))
	queue, _ := asynq.GetQueueName(ctx)
	taskID, _ := asynq.GetTaskID(ctx)
	requestctx.SetTask(ctx, taskID, task.Type(), queue)
	requestctx.SetTaskName(ctx, taskNameFromTask(task))
	requestctx.SetRequest(ctx, "TASK", task.Type(), "")
	requestctx.SetLocale(ctx, task.Headers()[headerLocale])
	if uid, convErr := strconv.Atoi(task.Headers()[headerUserID]); convErr == nil {
		requestctx.SetUser(ctx, uid, task.Headers()[headerUserName], "")
	}
	if wfMeta := workflowTaskMetaFromTask(task); wfMeta.WorkflowID != "" {
		requestctx.SetWorkflow(ctx, wfMeta.WorkflowID, wfMeta.WorkflowName, wfMeta.WorkflowNode, wfMeta.ShardIndex, wfMeta.ShardTotal)
	}
	if mode := taskPayloadString(task, "mode"); mode != "" {
		requestctx.SetMode(ctx, mode)
	}

	// 如果当前上下文已经存在 span，则优先回写当前 span；否则回写从任务头透传过来的父级 span。
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		requestctx.SetTrace(ctx, spanCtx.TraceID().String(), spanCtx.SpanID().String())
	}
	return loggerx.BindContext(ctx)
}

// traceAndLogMiddleware 为所有任务处理链统一补充 trace、请求上下文和结构化日志。
func (m *Manager) traceAndLogMiddleware() asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			// 先把任务上下文基础字段补齐，保证后续成功/失败日志都复用同一套元数据。
			ctx = m.enrichTaskContext(ctx, task)
			ctx, _ = taskstats.WithTracker(ctx, taskNameFromTask(task))
			meta := requestctx.FromContext(ctx)

			// 开始 consumer span，并把 trace_id/span_id 回写到上下文供日志复用。
			ctx, span := m.tracer.Start(ctx, task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
			queue, _ := asynq.GetQueueName(ctx)
			begin := time.Now()
			taskID := ""
			if meta != nil {
				taskID = meta.TaskID
			}
			attemptToken := m.recordTaskRuntimeStart(ctx, queue, taskID, begin)
			sc := span.SpanContext()
			requestctx.SetTrace(ctx, sc.TraceID().String(), sc.SpanID().String())
			ctx = loggerx.BindContext(ctx)
			loggerx.Infow(ctx, taskLogMessage("任务 开始执行", requestctx.FromContext(ctx)), taskLogFields(task)...)

			// 已成功分片的技术重试只继续编排，禁止重复执行业务 handler。
			businessRan := false
			skipBusiness, err := m.workflowTaskSettled(ctx, task)
			if err == nil && !skipBusiness {
				businessRan = true
				err = next.ProcessTask(ctx, task)
			}
			if err != nil && businessRan {
				err = m.applyWorkflowBusinessRetryLimit(ctx, task, err)
			}
			requestctx.SetLatency(ctx, time.Since(begin))
			statsSnapshot := taskstats.SnapshotFromContext(ctx)
			statsWriteCtx, statsWriteCancel := m.taskFinalWriteContext(ctx)
			statsErr := m.recordWorkflowTaskStats(statsWriteCtx, workflowTaskMetaFromTask(task), statsSnapshot)
			statsWriteCancel()
			if statsErr != nil {
				loggerx.Errorw(ctx, "工作流节点处理量记录失败", statsErr, taskLogFields(task)...)
			}
			if err == nil {
				// 成功则回写工作流节点状态，推动 DAG 继续执行后继节点。
				if markErr := m.markTaskSuccess(ctx, workflowTaskMetaFromTask(task)); markErr != nil {
					err = errors.Join(ErrWorkflowDispatch, markErr)
				}
			}
			m.recordTaskRuntimeFinish(ctx, queue, taskID, attemptToken, begin, err, statsSnapshot)
			recordTaskExecutionMetrics(queue, task.Type(), err, time.Since(begin), statsSnapshot)
			if err != nil {
				requestctx.SetError(ctx, err, err.Error())
				requestctx.SetResponse(ctx, 500, 2, err.Error(), err.Error())
			} else {
				requestctx.SetResponse(ctx, 200, 1, "成功", "")
			}
			fields := []logx.LogField{logx.Field("latency_ms", requestctx.FromContext(ctx).LatencyMS)}
			fields = append(fields, taskLogFields(task)...)
			fields = append(fields, taskStatsLogFields(statsSnapshot)...)
			if err != nil {
				span.SetStatus(otelcodes.Error, err.Error())
				span.RecordError(err)
			} else {
				m.writeTaskResult(ctx, task, requestctx.FromContext(ctx), begin, statsSnapshot)
				loggerx.Infow(ctx, taskLogMessage("任务 执行完成", requestctx.FromContext(ctx)), fields...)
				span.SetStatus(otelcodes.Ok, "ok")
			}
			// 统一写入 span attributes/status，方便按 queue/task_type/workflow 维度检索。
			span.SetAttributes(
				attribute.String("messaging.system", "redis"),
				attribute.String("messaging.destination", queue),
				attribute.String("messaging.operation", "process"),
				attribute.String("app.task_type", task.Type()),
			)
			if meta != nil && meta.WorkflowID != "" {
				span.SetAttributes(
					attribute.String("app.workflow_id", meta.WorkflowID),
					attribute.String("app.workflow_name", meta.WorkflowName),
					attribute.String("app.workflow_node", meta.WorkflowNode),
				)
			}
			span.SetAttributes(loggerx.TraceAttributesFromMeta(requestctx.FromContext(ctx))...)
			span.End()
			if err != nil {
				return errors.Tag(err)
			}
			return nil
		})
	}
}

// newTask 把当前上下文中的链路、语言和用户信息注入 Asynq 任务头。
func (m *Manager) newTask(ctx context.Context, taskType string, payload []byte, extraHeaders map[string]string) *asynq.Task {
	headers := make(map[string]string, len(extraHeaders)+6)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
	if meta := requestctx.FromContext(ctx); meta != nil {
		if meta.Locale != "" {
			headers[headerLocale] = meta.Locale
		}
		if meta.UserID > 0 {
			headers[headerUserID] = strconv.Itoa(meta.UserID)
		}
		if meta.UserName != "" {
			headers[headerUserName] = meta.UserName
		}
	}
	if appID := m.appNamespace(); appID != "" {
		headers[headerAppID] = appID
	}
	for key, value := range extraHeaders {
		if value != "" {
			headers[key] = value
		}
	}
	return asynq.NewTaskWithHeaders(taskType, payload, headers)
}

// workflowTaskMetaFromTask 从任务头和 payload 中提取工作流元数据。
func workflowTaskMetaFromTask(task *asynq.Task) WorkflowTaskMeta {
	if task == nil {
		return WorkflowTaskMeta{}
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   helper.FirstNonEmptyString(task.Headers()[headerWorkflowID]),
		WorkflowName: helper.FirstNonEmptyString(task.Headers()[headerWorkflowName]),
		WorkflowNode: helper.FirstNonEmptyString(task.Headers()[headerWorkflowNode]),
		ShardIndex:   toInt(task.Headers()[headerShardIndex]),
		ShardTotal:   toInt(task.Headers()[headerShardTotal]),
	}
	if len(task.Payload()) == 0 {
		return meta
	}
	_ = json.Unmarshal(task.Payload(), &meta)
	if meta.WorkflowID == "" {
		meta.WorkflowID = task.Headers()[headerWorkflowID]
	}
	if meta.WorkflowName == "" {
		meta.WorkflowName = task.Headers()[headerWorkflowName]
	}
	if meta.WorkflowNode == "" {
		meta.WorkflowNode = task.Headers()[headerWorkflowNode]
	}
	if meta.ShardTotal == 0 {
		meta.ShardTotal = toInt(task.Headers()[headerShardTotal])
	}
	return meta
}

// workflowTaskMetaFromTaskInfo 从 Asynq 任务详情中提取工作流元数据。
// 立即执行从 TaskInfo 中复原 workflowID、node 和 shardIndex。
func workflowTaskMetaFromTaskInfo(info *asynq.TaskInfo) WorkflowTaskMeta {
	if info == nil {
		return WorkflowTaskMeta{}
	}
	meta := WorkflowTaskMeta{
		WorkflowID:   helper.FirstNonEmptyString(info.Headers[headerWorkflowID]),
		WorkflowName: helper.FirstNonEmptyString(info.Headers[headerWorkflowName]),
		WorkflowNode: helper.FirstNonEmptyString(info.Headers[headerWorkflowNode]),
		ShardIndex:   toInt(info.Headers[headerShardIndex]),
		ShardTotal:   toInt(info.Headers[headerShardTotal]),
	}
	if len(info.Payload) > 0 {
		_ = json.Unmarshal(info.Payload, &meta)
	}
	if meta.WorkflowID == "" {
		meta.WorkflowID = info.Headers[headerWorkflowID]
	}
	if meta.WorkflowName == "" {
		meta.WorkflowName = info.Headers[headerWorkflowName]
	}
	if meta.WorkflowNode == "" {
		meta.WorkflowNode = info.Headers[headerWorkflowNode]
	}
	if meta.ShardTotal == 0 {
		meta.ShardTotal = toInt(info.Headers[headerShardTotal])
	}
	return meta
}

// taskPayloadString 从任务 payload 中读取指定首层字段，主要用于 mode 等日志检索字段补齐。
func taskPayloadString(task *asynq.Task, key string) string {
	if task == nil || len(task.Payload()) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(task.Payload(), &payloadMap); err != nil {
		return ""
	}
	return strings.TrimSpace(anyToString(payloadMap[key]))
}
