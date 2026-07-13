package task

import (
	corelogic "admin/internal/logic"
	"cmp"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/svc"
	"admin/internal/task/queue"
	"admin/internal/types"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	taskStatePending     = "pending"                                   // 待执行任务状态，表示任务已入队等待 Worker 消费
	taskStateActive      = "active"                                    // 执行中任务状态，表示任务已被 Worker 领取
	taskStateScheduled   = "scheduled"                                 // 定时任务状态，表示任务将在指定时间进入待执行队列
	taskStateRetry       = "retry"                                     // 重试任务状态，表示任务失败后等待下一次自动重试
	taskStateArchived    = "archived"                                  // 归档任务状态，表示任务已终止自动重试，通常需要人工补偿
	taskStateCompleted   = "completed"                                 // 已完成任务状态，表示任务成功执行且处于保留窗口内
	taskStateAggregating = "aggregating"                               // 聚合中任务状态，表示任务等待同组聚合后再执行
	taskListScanPageSize = 100                                         // 任务列表多状态聚合的单页扫描大小
	taskListScanMaxPages = 50                                          // 任务列表多状态聚合最大扫描页数，避免宽范围查询拖慢接口
	taskListScanMaxRows  = taskListScanPageSize * taskListScanMaxPages // 单次聚合最多读取的任务数
)

// taskOverviewStateKeys 定义任务总览页需要返回统计的状态顺序。
var taskOverviewStateKeys = []string{
	taskStatePending,
	taskStateActive,
	taskStateScheduled,
	taskStateRetry,
	taskStateArchived,
	taskStateCompleted,
	taskStateAggregating,
}

// taskExecutedStateKeys 定义时间段查询默认聚合的执行态。
var taskExecutedStateKeys = []string{
	taskStateActive,
	taskStateRetry,
	taskStateArchived,
	taskStateCompleted,
}

// TaskLogic 封装任务系统的管理接口逻辑。
type TaskLogic struct {
	*corelogic.BaseLogic // 复用上下文、日志和任务依赖访问能力
}

// NewTaskLogic 创建任务管理逻辑对象。
func NewTaskLogic(r *http.Request, svcCtx *svc.ServiceContext) *TaskLogic {
	return &TaskLogic{BaseLogic: corelogic.NewBaseLogic(r, svcCtx)}
}

// TriggerWorkflow 手动触发工作流。
func (l *TaskLogic) TriggerWorkflow(req *types.TriggerTaskWorkflowReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.TriggerWorkflow 任务系统未启用"))
	}
	resp, err := l.Svc.Task.EnqueueWorkflowTrigger(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskTriggerSuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.TriggerWorkflow 任务系统未启用"))
	case errors.Is(err, taskqueue.ErrWorkflowNotFound):
		return types.NotFound(i18n.MsgKeyTaskWorkflowNotFound, err).ToBizResult()
	case errors.Is(err, taskqueue.ErrWorkflowAlreadyExists), errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyTaskDuplicate).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.TriggerWorkflow 工作流任务重复"))
	default:
		return types.ServerError(i18n.MsgKeyTaskTriggerFail, err, "TaskLogic.TriggerWorkflow").ToBizResult()
	}
}

// EnqueueTask 手动投递一个已注册的通用任务。
func (l *TaskLogic) EnqueueTask(req *types.EnqueueTaskReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.EnqueueTask 任务系统未启用"))
	}
	resp, err := l.Svc.Task.EnqueueRegisteredTask(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskEnqueueSuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.EnqueueTask 任务系统未启用"))
	case errors.Is(err, taskqueue.ErrTaskTypeNotFound):
		return types.NotFound(i18n.MsgKeyTaskTypeNotFound, err).ToBizResult()
	case errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyTaskDuplicate).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.EnqueueTask 任务重复"))
	default:
		return types.ServerError(i18n.MsgKeyTaskEnqueueFail, err, "TaskLogic.EnqueueTask").ToBizResult()
	}
}

// GetWorkflowStatus 查询工作流实例状态。
func (l *TaskLogic) GetWorkflowStatus(req *types.GetTaskWorkflowReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetWorkflowStatus 任务系统未启用"))
	}
	resp, err := l.Svc.Task.GetWorkflowStatus(l.Ctx, req.WorkflowID)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.GetWorkflowStatus 任务系统未启用"))
	case errors.Is(err, redis.Nil):
		return types.NotFound(i18n.MsgKeyTaskWorkflowNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// GetTaskInfo 查询单个任务详情。
func (l *TaskLogic) GetTaskInfo(req *types.GetTaskInfoReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetTaskInfo 任务系统未启用"))
	}
	resp, err := l.Svc.Task.GetTaskInfo(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.GetTaskInfo 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	case errors.Is(err, asynq.ErrTaskNotFound):
		return types.NotFound(i18n.MsgKeyTaskNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// ListTasks 按队列和状态分页查询任务列表。
func (l *TaskLogic) ListTasks(req *types.ListTaskItemsReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListTasks 任务系统未启用"))
	}
	resp, err := l.Svc.Task.ListTasks(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.ListTasks 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	case errors.Is(err, taskqueue.ErrTaskListScanLimitExceeded):
		return types.ParamError(err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// ListTasksOverview 按“总览聚合 + 按需查询”模式返回任务列表。
func (l *TaskLogic) ListTasksOverview(req *types.ListTaskItemsOverviewReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListTasksOverview 任务系统未启用"))
	}
	resp, err := l.listTasksOverview(req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.ListTasksOverview 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	case errors.Is(err, taskqueue.ErrTaskListScanLimitExceeded):
		return types.ParamError(err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// listTasksOverview 执行受控聚合查询，避免前端首屏并发请求风暴。
func (l *TaskLogic) listTasksOverview(req *types.ListTaskItemsOverviewReq) (*types.TaskListOverviewResp, error) {
	queueNames, stateTotals, err := l.resolveOverviewQueues(strings.TrimSpace(req.Queue))
	if err != nil {
		return nil, errors.Tag(err)
	}
	startTime := strings.TrimSpace(req.StartTime)
	endTime := strings.TrimSpace(req.EndTime)
	stateCandidates := buildOverviewStates(strings.TrimSpace(req.State), strings.TrimSpace(req.Group), req.IncludeAggregating)
	resp := &types.TaskListOverviewResp{
		Queue:         strings.TrimSpace(req.Queue),
		State:         strings.TrimSpace(req.State),
		Group:         strings.TrimSpace(req.Group),
		TaskID:        strings.TrimSpace(req.TaskID),
		WorkflowID:    strings.TrimSpace(req.WorkflowID),
		TaskName:      strings.TrimSpace(req.TaskName),
		StartTime:     startTime,
		EndTime:       endTime,
		Page:          req.Page,
		PageSize:      req.PageSize,
		AggregateMode: len(queueNames) > 1,
		Queues:        append([]string(nil), queueNames...),
		StateTotals:   stateTotals,
		Tasks:         make([]types.TaskItem, 0),
	}
	if taskOverviewHasTimeRange(req) && strings.TrimSpace(req.State) == "" {
		currentResp, err := l.aggregateTasksByStates(queueNames, taskExecutedStateKeys, strings.TrimSpace(req.Group), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), startTime, endTime, req.Page, req.PageSize)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = currentResp.Total
		resp.Tasks = currentResp.Tasks
		return resp, nil
	}
	if strings.TrimSpace(req.State) == "" {
		currentResp, err := l.aggregateTasksByStates(queueNames, stateCandidates, strings.TrimSpace(req.Group), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), startTime, endTime, req.Page, req.PageSize)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = currentResp.Total
		resp.Tasks = currentResp.Tasks
		return resp, nil
	}
	currentResp, err := l.aggregateTasksByStates(queueNames, stateCandidates, strings.TrimSpace(req.Group), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), startTime, endTime, req.Page, req.PageSize)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp.EffectiveState = stateCandidates[0]
	resp.Total = currentResp.Total
	resp.Tasks = currentResp.Tasks
	return resp, nil
}

// taskOverviewHasTimeRange 判断总览查询是否带任务时间段筛选。
func taskOverviewHasTimeRange(req *types.ListTaskItemsOverviewReq) bool {
	if req == nil {
		return false
	}
	return strings.TrimSpace(req.StartTime) != "" || strings.TrimSpace(req.EndTime) != ""
}

// resolveOverviewQueues 从一次 Asynq 队列快照解析查询队列与状态计数，避免同一请求重复探测 Redis。
func (l *TaskLogic) resolveOverviewQueues(queue string) ([]string, map[string]int64, error) {
	totals := newTaskStateTotals()
	queueResp, err := l.Svc.Task.ListQueues(l.Ctx)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	queue = strings.TrimSpace(queue)
	queueNames := make([]string, 0, len(queueResp.Queues))
	for _, item := range queueResp.Queues {
		queueName := strings.TrimSpace(item.Name)
		if queueName == "" {
			continue
		}
		if queue != "" && queueName != queue {
			continue
		}
		queueNames = append(queueNames, queueName)
		totals[taskStatePending] += int64(item.Pending)
		totals[taskStateActive] += int64(item.Active)
		totals[taskStateScheduled] += int64(item.Scheduled)
		totals[taskStateRetry] += int64(item.Retry)
		totals[taskStateArchived] += int64(item.Archived)
		totals[taskStateCompleted] += int64(item.Completed)
		totals[taskStateAggregating] += int64(item.Aggregating)
	}
	if queue != "" && len(queueNames) == 0 {
		return nil, nil, asynq.ErrQueueNotFound
	}
	return queueNames, totals, nil
}

// newTaskStateTotals 返回任务列表固定状态集合，避免前端因为缺少某个 key 误判为后端未返回统计。
func newTaskStateTotals() map[string]int64 {
	totals := make(map[string]int64, len(taskOverviewStateKeys))
	for _, state := range taskOverviewStateKeys {
		totals[state] = 0
	}
	return totals
}

// aggregateTasksByStates 聚合多个任务状态，并按任务活动时间统一排序分页。
func (l *TaskLogic) aggregateTasksByStates(queueNames []string, states []string, group string, taskID string, workflowID string, taskName string, startTime string, endTime string, page int, pageSize int) (*types.TaskListResp, error) {
	mergedTasks := make([]types.TaskItem, 0)
	var total int64
	page, pageSize = normalizeTaskListPage(page, pageSize)
	if page*pageSize > taskListScanMaxRows {
		return nil, errors.Wrapf(taskqueue.ErrTaskListScanLimitExceeded,
			"任务总览最多查询前 %d 条记录，请缩小页码或筛选范围", taskListScanMaxRows)
	}
	// 单队列单状态直接使用 Asynq 原生分页，避免为了聚合扫描前置页。
	if len(queueNames) == 1 && len(states) == 1 {
		return l.Svc.Task.ListTasks(l.Ctx, &types.ListTaskItemsReq{
			Queue:      queueNames[0],
			State:      states[0],
			Group:      group,
			TaskID:     taskID,
			WorkflowID: workflowID,
			TaskName:   taskName,
			StartTime:  startTime,
			EndTime:    endTime,
			Page:       page,
			PageSize:   pageSize,
		})
	}
	needsWidePage := taskListAggregateNeedsWidePage(taskID, workflowID, taskName, startTime, endTime)
	maxPages := taskListAggregateMaxPages(taskID, workflowID, taskName, startTime, endTime, page, pageSize)
	for _, state := range states {
		for _, queueName := range queueNames {
			if needsWidePage {
				currentResp, err := l.Svc.Task.ListTasks(l.Ctx, &types.ListTaskItemsReq{
					Queue:      queueName,
					State:      state,
					Group:      group,
					TaskID:     taskID,
					WorkflowID: workflowID,
					TaskName:   taskName,
					StartTime:  startTime,
					EndTime:    endTime,
					Page:       1,
					PageSize:   taskListScanMaxRows,
				})
				if err != nil {
					return nil, errors.Tag(err)
				}
				total += currentResp.Total
				mergedTasks = append(mergedTasks, currentResp.Tasks...)
				if currentResp.Total > int64(len(currentResp.Tasks)) {
					return nil, errors.Wrapf(taskqueue.ErrTaskListScanLimitExceeded,
						"任务筛选结果超过 %d 条，请缩小筛选范围", taskListScanMaxRows)
				}
				continue
			}
			for currentPage := 1; currentPage <= maxPages; currentPage++ {
				currentResp, err := l.Svc.Task.ListTasks(l.Ctx, &types.ListTaskItemsReq{
					Queue:      queueName,
					State:      state,
					Group:      group,
					TaskID:     taskID,
					WorkflowID: workflowID,
					TaskName:   taskName,
					StartTime:  startTime,
					EndTime:    endTime,
					Page:       currentPage,
					PageSize:   taskListScanPageSize,
				})
				if err != nil {
					return nil, errors.Tag(err)
				}
				if currentPage == 1 {
					total += currentResp.Total
				}
				mergedTasks = append(mergedTasks, currentResp.Tasks...)
				if len(currentResp.Tasks) < taskListScanPageSize || int64(currentPage*taskListScanPageSize) >= currentResp.Total {
					break
				}
			}
		}
	}
	slices.SortFunc(mergedTasks, func(left, right types.TaskItem) int {
		return cmp.Compare(buildTaskItemSortValue(right), buildTaskItemSortValue(left))
	})
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start >= len(mergedTasks) {
		mergedTasks = []types.TaskItem{}
	} else {
		end := minTaskPageEnd(start+pageSize, len(mergedTasks))
		mergedTasks = mergedTasks[start:end]
	}
	return &types.TaskListResp{
		Group:     group,
		TaskID:    taskID,
		TaskName:  taskName,
		StartTime: startTime,
		EndTime:   endTime,
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
		Tasks:     mergedTasks,
	}, nil
}

// taskListAggregateMaxPages 返回多状态聚合需要扫描的页数上限。
func taskListAggregateMaxPages(taskID string, workflowID string, taskName string, startTime string, endTime string, page int, pageSize int) int {
	if taskListAggregateNeedsWidePage(taskID, workflowID, taskName, startTime, endTime) {
		return taskListScanMaxPages
	}
	page, pageSize = normalizeTaskListPage(page, pageSize)
	neededRows := page * pageSize
	neededPages := (neededRows + taskListScanPageSize - 1) / taskListScanPageSize
	return neededPages
}

// taskListAggregateNeedsWidePage 判断多状态聚合是否需要让队列层一次完成受控扫描。
func taskListAggregateNeedsWidePage(taskID string, workflowID string, taskName string, startTime string, endTime string) bool {
	return strings.TrimSpace(taskID) != "" ||
		strings.TrimSpace(workflowID) != "" ||
		strings.TrimSpace(taskName) != "" ||
		strings.TrimSpace(startTime) != "" ||
		strings.TrimSpace(endTime) != ""
}

// normalizeTaskListPage 收敛任务列表分页参数，收敛内部直接调用绕过请求校验的场景。
func normalizeTaskListPage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// buildOverviewStates 返回任务总览页允许聚合或精确查询的状态顺序。
func buildOverviewStates(state string, group string, includeAggregating bool) []string {
	if strings.TrimSpace(state) != "" {
		return []string{strings.TrimSpace(state)}
	}
	states := []string{taskStateRetry, taskStateActive, taskStatePending, taskStateScheduled, taskStateArchived, taskStateCompleted}
	if includeAggregating && strings.TrimSpace(group) != "" {
		states = append(states, taskStateAggregating)
	}
	return states
}

// buildTaskItemSortValue 统一生成任务排序时间戳，优先看完成、失败、开始和下次执行时间。
func buildTaskItemSortValue(item types.TaskItem) int64 {
	for _, rawValue := range taskItemActivityTimeCandidates(item) {
		if strings.TrimSpace(rawValue) == "" {
			continue
		}
		parsedValue, err := time.Parse(time.RFC3339, rawValue)
		if err == nil {
			return parsedValue.UnixMilli()
		}
	}
	return 0
}

// taskItemActivityTimeCandidates 按任务状态返回活动时间候选字段。
func taskItemActivityTimeCandidates(item types.TaskItem) []string {
	switch strings.TrimSpace(strings.ToLower(item.State)) {
	case taskStateScheduled:
		return []string{item.NextProcessAt, item.Deadline}
	case taskStatePending, taskStateAggregating:
		return []string{item.NextProcessAt, item.Deadline}
	default:
		return []string{item.CompletedAt, item.LastFailedAt, item.StartedAt, item.NextProcessAt, item.Deadline}
	}
}

// minTaskPageEnd 返回两个整数中的较小值。
func minTaskPageEnd(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

// ListQueues 查询任务队列与 worker 概览。
func (l *TaskLogic) ListQueues() *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListQueues 任务系统未启用"))
	}
	resp, err := l.Svc.Task.ListQueues(l.Ctx)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	if errors.Is(err, taskqueue.ErrTaskQueueDisabled) {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.ListQueues 任务系统未启用"))
	}
	return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
}

// GetConfigReloadStatus 查询 config.yaml 热加载运行状态。
func (l *TaskLogic) GetConfigReloadStatus() *types.BizResult {
	if l.Svc == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetConfigReloadStatus 配置热加载未启用"))
	}
	status := l.Svc.CurrentHotReloadStatus()
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.TaskConfigReloadStatusResp{
			Enabled:                status.Enabled,
			Watching:               status.Watching,
			ConfigFile:             status.ConfigFile,
			CheckIntervalSeconds:   status.CheckIntervalSeconds,
			ConfigVersion:          status.ConfigVersion,
			ConfigSummary:          status.ConfigSummary,
			RestartRequired:        status.RestartRequired,
			RestartReason:          status.RestartReason,
			LastStatus:             status.LastStatus,
			LastMessage:            l.messageFromKeyOrRaw(status.LastMessageKey, status.LastMessage),
			LastTriggerSource:      status.LastTriggerSource,
			LastFailureCategory:    status.LastFailureCategory,
			LastCheckedAt:          formatTaskTime(status.LastCheckedAt),
			LastReloadAt:           formatTaskTime(status.LastReloadAt),
			LastSuccessAt:          formatTaskTime(status.LastSuccessAt),
			LastFailureAt:          formatTaskTime(status.LastFailureAt),
			ReloadCount:            status.ReloadCount,
			SuppressedFailureCount: status.SuppressedFailureCount,
		})
}

// messageFromKeyOrRaw 优先使用状态源头保存的 key 翻译，动态错误说明保留原文。
func (l *TaskLogic) messageFromKeyOrRaw(messageKey, fallback string) string {
	messageKey = strings.TrimSpace(messageKey)
	if messageKey != "" {
		return l.Message(messageKey)
	}
	return strings.TrimSpace(fallback)
}

// RunConfigReload 手动触发一次配置重载，并返回最新热加载状态。
func (l *TaskLogic) RunConfigReload() *types.BizResult {
	if l.Svc == nil || l.Svc.ConfigReload == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.RunConfigReload 配置热加载未启用"))
	}
	if err := l.Svc.ConfigReload.ReloadConfig(l.Ctx, "manual_api"); err != nil {
		return types.ServerError(i18n.MsgKeyTaskTriggerFail, err, "TaskLogic.RunConfigReload").ToBizResult()
	}
	return l.GetConfigReloadStatus()
}

// formatTaskTime 把零值时间统一转换为空串，避免接口返回 Go 零时间。
func formatTaskTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// RunTask 让可执行状态的任务立即转入 pending 队列执行。
func (l *TaskLogic) RunTask(req *types.OperateTaskReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.RunTask 任务系统未启用"))
	}
	err := l.Svc.Task.RunTask(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskRunSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.RunTask 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	case errors.Is(err, asynq.ErrTaskNotFound):
		return types.NotFound(i18n.MsgKeyTaskNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskRunFail, err, "TaskLogic.RunTask").ToBizResult()
	}
}

// ListRegisteredTaskTypes 返回当前已注册的任务类型清单，供管理后台总控台下拉复用。
func (l *TaskLogic) ListRegisteredTaskTypes() *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListRegisteredTaskTypes 任务系统未启用"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.TaskTypeRegistryResp{Items: l.Svc.Task.ListRegisteredTaskTypes(l.Ctx)})
}

// ListRegisteredWorkflows 返回当前已注册的工作流清单，供管理后台总控台选择工作流名称。
func (l *TaskLogic) ListRegisteredWorkflows() *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListRegisteredWorkflows 任务系统未启用"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.WorkflowRegistryResp{Items: l.Svc.Task.ListRegisteredWorkflows(l.Ctx)})
}

// DeleteTask 删除指定任务。
func (l *TaskLogic) DeleteTask(req *types.OperateTaskReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.DeleteTask 任务系统未启用"))
	}
	err := l.Svc.Task.DeleteTask(l.Ctx, req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskDeleteSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.DeleteTask 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	case errors.Is(err, asynq.ErrTaskNotFound):
		return types.NotFound(i18n.MsgKeyTaskNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskDeleteFail, err, "TaskLogic.DeleteTask").ToBizResult()
	}
}

// PauseQueue 暂停队列消费。
func (l *TaskLogic) PauseQueue(req *types.OperateTaskQueueReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.PauseQueue 任务系统未启用"))
	}
	err := l.Svc.Task.PauseQueue(l.Ctx, req.Queue)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskPauseSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.PauseQueue 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskPauseFail, err, "TaskLogic.PauseQueue").ToBizResult()
	}
}

// ResumeQueue 恢复队列消费。
func (l *TaskLogic) ResumeQueue(req *types.OperateTaskQueueReq) *types.BizResult {
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ResumeQueue 任务系统未启用"))
	}
	err := l.Svc.Task.ResumeQueue(l.Ctx, req.Queue)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskResumeSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "TaskLogic.ResumeQueue 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskResumeFail, err, "TaskLogic.ResumeQueue").ToBizResult()
	}
}
