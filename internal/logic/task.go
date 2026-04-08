package logic

import (
	"cmp"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/svc"
	"admin_cron/internal/taskqueue"
	"admin_cron/internal/types"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	taskStatePending     = "pending"     // 待执行任务状态，表示任务已入队等待 Worker 消费
	taskStateActive      = "active"      // 执行中任务状态，表示任务已被 Worker 领取
	taskStateScheduled   = "scheduled"   // 定时任务状态，表示任务将在指定时间进入待执行队列
	taskStateRetry       = "retry"       // 重试任务状态，表示任务失败后等待下一次自动重试
	taskStateArchived    = "archived"    // 归档任务状态，表示任务已终止自动重试，通常需要人工补偿
	taskStateCompleted   = "completed"   // 已完成任务状态，表示任务成功执行且处于保留窗口内
	taskStateAggregating = "aggregating" // 聚合中任务状态，表示任务等待同组聚合后再执行
	taskListScanPageSize = 100           // 任务列表多状态聚合的单页扫描大小
	taskListScanMaxPages = 50            // 任务列表多状态聚合最大扫描页数，避免宽范围查询拖慢接口
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
	*BaseLogic // 复用上下文、日志和任务依赖访问能力
}

// NewTaskLogic 创建任务管理逻辑对象。
func NewTaskLogic(r *http.Request, svcCtx *svc.ServiceContext) *TaskLogic {
	return &TaskLogic{BaseLogic: NewBaseLogic(r, svcCtx)}
}

// TriggerWorkflow 手动触发工作流。
func (l *TaskLogic) TriggerWorkflow(req *types.TriggerTaskWorkflowReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.TriggerWorkflow 任务系统未启用"))
	}
	resp, err := l.svc.Task.EnqueueWorkflowTrigger(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskTriggerSuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.TriggerWorkflow 任务系统未启用"))
	case errors.Is(err, taskqueue.ErrWorkflowNotFound):
		return types.NotFound(i18n.MsgKeyTaskWorkflowNotFound, err).ToBizResult()
	case errors.Is(err, taskqueue.ErrWorkflowAlreadyExists), errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyTaskDuplicate).
			WithError(wrapLogicError(err, "TaskLogic.TriggerWorkflow 工作流任务重复"))
	default:
		return types.ServerError(i18n.MsgKeyTaskTriggerFail, err, "TaskLogic.TriggerWorkflow").ToBizResult()
	}
}

// EnqueueTask 手动投递一个已注册的通用任务。
func (l *TaskLogic) EnqueueTask(req *types.EnqueueTaskReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.EnqueueTask 任务系统未启用"))
	}
	resp, err := l.svc.Task.EnqueueRegisteredTask(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskEnqueueSuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.EnqueueTask 任务系统未启用"))
	case errors.Is(err, taskqueue.ErrTaskTypeNotFound):
		return types.NotFound(i18n.MsgKeyTaskTypeNotFound, err).ToBizResult()
	case errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyTaskDuplicate).
			WithError(wrapLogicError(err, "TaskLogic.EnqueueTask 任务重复"))
	default:
		return types.ServerError(i18n.MsgKeyTaskEnqueueFail, err, "TaskLogic.EnqueueTask").ToBizResult()
	}
}

// GetWorkflowStatus 查询工作流实例状态。
func (l *TaskLogic) GetWorkflowStatus(req *types.GetTaskWorkflowReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetWorkflowStatus 任务系统未启用"))
	}
	resp, err := l.svc.Task.GetWorkflowStatus(l.Context(), req.WorkflowID)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.GetWorkflowStatus 任务系统未启用"))
	case errors.Is(err, redis.Nil):
		return types.NotFound(i18n.MsgKeyTaskWorkflowNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// GetTaskInfo 查询单个任务详情。
func (l *TaskLogic) GetTaskInfo(req *types.GetTaskInfoReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetTaskInfo 任务系统未启用"))
	}
	resp, err := l.svc.Task.GetTaskInfo(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.GetTaskInfo 任务系统未启用"))
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
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListTasks 任务系统未启用"))
	}
	resp, err := l.svc.Task.ListTasks(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.ListTasks 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// ListTasksOverview 按“总览聚合 + 按需查询”模式返回任务列表。
func (l *TaskLogic) ListTasksOverview(req *types.ListTaskItemsOverviewReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListTasksOverview 任务系统未启用"))
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
			WithError(wrapLogicError(err, "TaskLogic.ListTasksOverview 任务系统未启用"))
	default:
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
	}
}

// listTasksOverview 执行受控聚合查询，避免前端首屏并发请求风暴。
func (l *TaskLogic) listTasksOverview(req *types.ListTaskItemsOverviewReq) (*types.TaskListOverviewResp, error) {
	queueNames, err := l.resolveOverviewQueueNames(strings.TrimSpace(req.Queue))
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(queueNames) == 0 {
		queueNames = []string{strings.TrimSpace(req.Queue)}
	}
	stateCandidates := buildOverviewStates(strings.TrimSpace(req.State), strings.TrimSpace(req.Group), req.IncludeAggregating)
	resp := &types.TaskListOverviewResp{
		Queue:         strings.TrimSpace(req.Queue),
		State:         strings.TrimSpace(req.State),
		Group:         strings.TrimSpace(req.Group),
		WorkflowID:    strings.TrimSpace(req.WorkflowID),
		TaskName:      strings.TrimSpace(req.TaskName),
		TimeStart:     strings.TrimSpace(req.TimeStart),
		TimeEnd:       strings.TrimSpace(req.TimeEnd),
		Page:          req.Page,
		PageSize:      req.PageSize,
		AggregateMode: len(queueNames) > 1,
		Queues:        append([]string(nil), queueNames...),
		StateTotals:   l.buildOverviewStateTotals(queueNames),
		Tasks:         make([]types.TaskItem, 0),
	}
	if taskOverviewHasTimeRange(req) && strings.TrimSpace(req.State) == "" {
		currentResp, err := l.aggregateTasksByStates(queueNames, taskExecutedStateKeys, strings.TrimSpace(req.Group), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), strings.TrimSpace(req.TimeStart), strings.TrimSpace(req.TimeEnd), req.Page, req.PageSize)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = currentResp.Total
		resp.Tasks = currentResp.Tasks
		return resp, nil
	}
	if strings.TrimSpace(req.State) == "" {
		currentResp, err := l.aggregateTasksByStates(queueNames, stateCandidates, strings.TrimSpace(req.Group), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), strings.TrimSpace(req.TimeStart), strings.TrimSpace(req.TimeEnd), req.Page, req.PageSize)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = currentResp.Total
		resp.Tasks = currentResp.Tasks
		return resp, nil
	}
	for _, state := range stateCandidates {
		currentResp, err := l.aggregateTasksByState(queueNames, state, strings.TrimSpace(req.Group), strings.TrimSpace(req.WorkflowID), strings.TrimSpace(req.TaskName), strings.TrimSpace(req.TimeStart), strings.TrimSpace(req.TimeEnd), req.Page, req.PageSize)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if currentResp.Total > 0 || len(currentResp.Tasks) > 0 {
			resp.EffectiveState = state
			resp.Total = currentResp.Total
			resp.Tasks = currentResp.Tasks
			return resp, nil
		}
	}
	if len(stateCandidates) == 1 {
		resp.EffectiveState = stateCandidates[0]
	}
	return resp, nil
}

// taskOverviewHasTimeRange 判断总览查询是否带任务时间段筛选。
func taskOverviewHasTimeRange(req *types.ListTaskItemsOverviewReq) bool {
	if req == nil {
		return false
	}
	return strings.TrimSpace(req.TimeStart) != "" || strings.TrimSpace(req.TimeEnd) != ""
}

// buildOverviewStateTotals 汇总当前可见队列的状态计数。
// 该计数来自 Asynq 队列快照，真实列表仍以 ListTasks 查询结果为准。
func (l *TaskLogic) buildOverviewStateTotals(queueNames []string) map[string]int64 {
	totals := newTaskStateTotals()
	if l == nil || l.svc == nil || l.svc.Task == nil {
		return totals
	}
	queueSet := make(map[string]struct{}, len(queueNames))
	for _, queueName := range queueNames {
		queueName = strings.TrimSpace(queueName)
		if queueName == "" {
			continue
		}
		queueSet[queueName] = struct{}{}
	}
	queueResp, err := l.svc.Task.ListQueues(l.Context())
	if err != nil || queueResp == nil {
		return totals
	}
	for _, item := range queueResp.Queues {
		queueName := strings.TrimSpace(item.Name)
		if queueName == "" {
			continue
		}
		if len(queueSet) > 0 {
			if _, ok := queueSet[queueName]; !ok {
				continue
			}
		}
		totals[taskStatePending] += int64(item.Pending)
		totals[taskStateActive] += int64(item.Active)
		totals[taskStateScheduled] += int64(item.Scheduled)
		totals[taskStateRetry] += int64(item.Retry)
		totals[taskStateArchived] += int64(item.Archived)
		totals[taskStateCompleted] += int64(item.Completed)
		totals[taskStateAggregating] += int64(item.Aggregating)
	}
	return totals
}

// newTaskStateTotals 返回任务列表固定状态集合，避免前端因为缺少某个 key 误判为后端未返回统计。
func newTaskStateTotals() map[string]int64 {
	totals := make(map[string]int64, len(taskOverviewStateKeys))
	for _, state := range taskOverviewStateKeys {
		totals[state] = 0
	}
	return totals
}

// resolveOverviewQueueNames 根据指定队列或当前可见队列快照解析本次聚合查询队列列表。
func (l *TaskLogic) resolveOverviewQueueNames(queue string) ([]string, error) {
	if strings.TrimSpace(queue) != "" {
		return []string{strings.TrimSpace(queue)}, nil
	}
	queueResp, err := l.svc.Task.ListQueues(l.Context())
	if err != nil {
		return nil, errors.Tag(err)
	}
	queueNames := make([]string, 0, len(queueResp.Queues))
	for _, item := range queueResp.Queues {
		queueName := strings.TrimSpace(item.Name)
		if queueName == "" {
			continue
		}
		queueNames = append(queueNames, queueName)
	}
	return queueNames, nil
}

// aggregateTasksByState 对指定状态执行单次队列聚合，并统一排序与分页。
func (l *TaskLogic) aggregateTasksByState(queueNames []string, state string, group string, workflowID string, taskName string, timeStart string, timeEnd string, page int, pageSize int) (*types.TaskListResp, error) {
	if len(queueNames) == 1 {
		return l.svc.Task.ListTasks(l.Context(), &types.ListTaskItemsReq{
			Queue:      queueNames[0],
			State:      state,
			Group:      group,
			WorkflowID: workflowID,
			TaskName:   taskName,
			TimeStart:  timeStart,
			TimeEnd:    timeEnd,
			Page:       page,
			PageSize:   pageSize,
		})
	}
	mergedTasks := make([]types.TaskItem, 0)
	resultMap := make(map[string]types.TaskItem)
	var total int64
	for _, queueName := range queueNames {
		currentResp, err := l.svc.Task.ListTasks(l.Context(), &types.ListTaskItemsReq{
			Queue:      queueName,
			State:      state,
			Group:      group,
			WorkflowID: workflowID,
			TaskName:   taskName,
			TimeStart:  timeStart,
			TimeEnd:    timeEnd,
			Page:       1,
			PageSize:   100,
		})
		if err != nil {
			continue
		}
		total += currentResp.Total
		for _, item := range currentResp.Tasks {
			resultMap[item.Queue+":"+item.ID] = item
		}
	}
	for _, item := range resultMap {
		mergedTasks = append(mergedTasks, item)
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
		State:     state,
		Group:     group,
		TaskName:  taskName,
		TimeStart: timeStart,
		TimeEnd:   timeEnd,
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
		Tasks:     mergedTasks,
	}, nil
}

// aggregateTasksByStates 聚合多个任务状态，并按任务活动时间统一排序分页。
func (l *TaskLogic) aggregateTasksByStates(queueNames []string, states []string, group string, workflowID string, taskName string, timeStart string, timeEnd string, page int, pageSize int) (*types.TaskListResp, error) {
	mergedTasks := make([]types.TaskItem, 0)
	var total int64
	page, pageSize = normalizeTaskListPage(page, pageSize)
	needsWidePage := taskListAggregateNeedsWidePage(workflowID, taskName, timeStart, timeEnd)
	maxPages := taskListAggregateMaxPages(workflowID, taskName, timeStart, timeEnd, page, pageSize)
	for _, state := range states {
		for _, queueName := range queueNames {
			if needsWidePage {
				currentResp, err := l.svc.Task.ListTasks(l.Context(), &types.ListTaskItemsReq{
					Queue:      queueName,
					State:      state,
					Group:      group,
					WorkflowID: workflowID,
					TaskName:   taskName,
					TimeStart:  timeStart,
					TimeEnd:    timeEnd,
					Page:       1,
					PageSize:   taskListScanPageSize * taskListScanMaxPages,
				})
				if err != nil {
					return nil, errors.Tag(err)
				}
				total += currentResp.Total
				mergedTasks = append(mergedTasks, currentResp.Tasks...)
				continue
			}
			for currentPage := 1; currentPage <= maxPages; currentPage++ {
				currentResp, err := l.svc.Task.ListTasks(l.Context(), &types.ListTaskItemsReq{
					Queue:      queueName,
					State:      state,
					Group:      group,
					WorkflowID: workflowID,
					TaskName:   taskName,
					TimeStart:  timeStart,
					TimeEnd:    timeEnd,
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
		TaskName:  taskName,
		TimeStart: timeStart,
		TimeEnd:   timeEnd,
		Page:      page,
		PageSize:  pageSize,
		Total:     total,
		Tasks:     mergedTasks,
	}, nil
}

// taskListAggregateMaxPages 返回多状态聚合需要扫描的页数上限。
func taskListAggregateMaxPages(workflowID string, taskName string, timeStart string, timeEnd string, page int, pageSize int) int {
	if taskListAggregateNeedsWidePage(workflowID, taskName, timeStart, timeEnd) {
		return taskListScanMaxPages
	}
	page, pageSize = normalizeTaskListPage(page, pageSize)
	neededRows := page * pageSize
	neededPages := (neededRows + taskListScanPageSize - 1) / taskListScanPageSize
	if neededPages > taskListScanMaxPages {
		return taskListScanMaxPages
	}
	return neededPages
}

// taskListAggregateNeedsWidePage 判断多状态聚合是否需要让队列层一次完成受控扫描。
func taskListAggregateNeedsWidePage(workflowID string, taskName string, timeStart string, timeEnd string) bool {
	return strings.TrimSpace(workflowID) != "" ||
		strings.TrimSpace(taskName) != "" ||
		strings.TrimSpace(timeStart) != "" ||
		strings.TrimSpace(timeEnd) != ""
}

// normalizeTaskListPage 收敛任务列表分页参数，兼容内部直接调用绕过请求校验的场景。
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
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListQueues 任务系统未启用"))
	}
	resp, err := l.svc.Task.ListQueues(l.Context())
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(resp)
	}
	if errors.Is(err, taskqueue.ErrTaskQueueDisabled) {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.ListQueues 任务系统未启用"))
	}
	return types.ServerError(i18n.MsgKeyTaskQueryFail, err).ToBizResult()
}

// GetConfigReloadStatus 查询 config.yaml 热加载运行状态。
func (l *TaskLogic) GetConfigReloadStatus() *types.BizResult {
	if l.svc == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.GetConfigReloadStatus 配置热加载未启用"))
	}
	status := l.svc.CurrentHotReloadStatus()
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
			LastMessage:            status.LastMessage,
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

// RunConfigReload 手动触发一次配置重载，并返回最新热加载状态。
func (l *TaskLogic) RunConfigReload() *types.BizResult {
	if l.svc == nil || l.svc.ConfigReload == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.RunConfigReload 配置热加载未启用"))
	}
	if err := l.svc.ConfigReload.ReloadConfig(l.Context(), "manual_api"); err != nil {
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
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.RunTask 任务系统未启用"))
	}
	err := l.svc.Task.RunTask(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskRunSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.RunTask 任务系统未启用"))
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
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListRegisteredTaskTypes 任务系统未启用"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.TaskTypeRegistryResp{Items: l.svc.Task.ListRegisteredTaskTypes()})
}

// ListRegisteredWorkflows 返回当前已注册的工作流清单，供管理后台总控台选择工作流名称。
func (l *TaskLogic) ListRegisteredWorkflows() *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ListRegisteredWorkflows 任务系统未启用"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.WorkflowRegistryResp{Items: l.svc.Task.ListRegisteredWorkflows()})
}

// DeleteTask 删除指定任务。
func (l *TaskLogic) DeleteTask(req *types.OperateTaskReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.DeleteTask 任务系统未启用"))
	}
	err := l.svc.Task.DeleteTask(l.Context(), req)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskDeleteSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.DeleteTask 任务系统未启用"))
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
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.PauseQueue 任务系统未启用"))
	}
	err := l.svc.Task.PauseQueue(l.Context(), req.Queue)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskPauseSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.PauseQueue 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskPauseFail, err, "TaskLogic.PauseQueue").ToBizResult()
	}
}

// ResumeQueue 恢复队列消费。
func (l *TaskLogic) ResumeQueue(req *types.OperateTaskQueueReq) *types.BizResult {
	if l.svc == nil || l.svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(taskqueue.ErrTaskQueueDisabled, "TaskLogic.ResumeQueue 任务系统未启用"))
	}
	err := l.svc.Task.ResumeQueue(l.Context(), req.Queue)
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskResumeSuccess)
	}
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(wrapLogicError(err, "TaskLogic.ResumeQueue 任务系统未启用"))
	case errors.Is(err, asynq.ErrQueueNotFound):
		return types.NotFound(i18n.MsgKeyTaskQueueNotFound, err).ToBizResult()
	default:
		return types.ServerError(i18n.MsgKeyTaskResumeFail, err, "TaskLogic.ResumeQueue").ToBizResult()
	}
}
