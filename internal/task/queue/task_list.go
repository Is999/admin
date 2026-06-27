package taskqueue

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/types"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// ListTasks 按队列和状态分页查询任务列表。
func (m *Manager) ListTasks(ctx context.Context, req *types.ListTaskItemsReq) (*types.TaskListResp, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("任务列表请求不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queue := strings.TrimSpace(req.Queue)
	state := strings.TrimSpace(req.State)
	group := strings.TrimSpace(req.Group)
	taskID := strings.TrimSpace(req.TaskID)
	workflowID := strings.TrimSpace(req.WorkflowID)
	// taskName 是任务名称关键字筛选条件，空值时保留 Asynq 原生分页路径。
	taskName := strings.TrimSpace(req.TaskName)
	startTime := strings.TrimSpace(req.StartTime)
	endTime := strings.TrimSpace(req.EndTime)
	timeRange, err := parseTaskListTimeRange(startTime, endTime)
	if err != nil {
		return nil, errors.Tag(err)
	}
	internalQueue := m.namespacedQueueName(queue)
	internalGroup := m.namespacedGroup(group)
	resp := &types.TaskListResp{
		Queue:      queue,
		State:      state,
		Group:      group,
		TaskID:     taskID,
		WorkflowID: workflowID,
		TaskName:   taskName,
		StartTime:  startTime,
		EndTime:    endTime,
		Page:       req.Page,
		PageSize:   req.PageSize,
		Tasks:      make([]types.TaskItem, 0),
	}
	needsItemScan := taskID != "" || workflowID != "" || taskName != "" || taskTimeRangeNeedsItemScan(state, timeRange)
	periodicNextRuns := m.periodicNextRuns(time.Now())
	if !needsItemScan {
		items, total, err := m.listTaskInfoPage(ctx, internalQueue, internalGroup, state, req.Page, req.PageSize, timeRange)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resp.Total = total
		resp.Tasks = make([]types.TaskItem, 0, len(items))
		for _, item := range items {
			resp.Tasks = append(resp.Tasks, m.toTaskItemWithPeriodicNextRuns(ctx, item, periodicNextRuns))
		}
		return resp, nil
	}
	filteredTasks, total, err := m.listTaskItemsByFilters(ctx, internalQueue, internalGroup, state, taskID, workflowID, taskName, req.Page, req.PageSize, timeRange, periodicNextRuns)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp.Total = total
	resp.Tasks = filteredTasks
	return resp, nil
}

// taskTimeRangeNeedsItemScan 判断时间范围是否需要读取任务详情后再过滤。
func taskTimeRangeNeedsItemScan(state string, timeRange taskListTimeRange) bool {
	if !timeRange.hasRange {
		return false
	}
	switch strings.TrimSpace(state) {
	case "scheduled", "archived", "completed":
		return false
	default:
		return true
	}
}

// listTaskInfoPage 按状态读取单页 Asynq 任务详情，并返回该状态下的总数。
func (m *Manager) listTaskInfoPage(ctx context.Context, internalQueue string, internalGroup string, state string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if state == "scheduled" {
		return m.listScheduledTaskInfoPage(ctx, internalQueue, page, pageSize, timeRange)
	}
	if taskStateUsesDescZSet(state) {
		return m.listDescZSetTaskInfoPage(ctx, internalQueue, state, page, pageSize, timeRange)
	}
	opts := []asynq.ListOption{asynq.Page(page), asynq.PageSize(pageSize)}
	var (
		items []*asynq.TaskInfo
		err   error
	)
	switch state {
	case "pending":
		items, err = m.inspector.ListPendingTasks(internalQueue, opts...)
	case "active":
		items, err = m.inspector.ListActiveTasks(internalQueue, opts...)
	case "aggregating":
		items, err = m.inspector.ListAggregatingTasks(internalQueue, internalGroup, opts...)
	default:
		return nil, 0, errors.Errorf("不支持的任务状态: %s", state)
	}
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	total := int64(len(items))
	if info, qerr := m.inspector.GetQueueInfo(internalQueue); qerr == nil {
		total = queueInfoTotalByState(info, state)
	}
	return items, total, nil
}

// taskStateUsesDescZSet 判断状态是否需要按 zset 分数倒序读取。
// 这些状态底层都是 zset；倒序读取能让任务列表优先展示最近的重试、归档和完成记录。
func taskStateUsesDescZSet(state string) bool {
	switch strings.TrimSpace(state) {
	case "retry", "archived", "completed":
		return true
	default:
		return false
	}
}

// listDescZSetTaskInfoPage 按 zset 分数倒序读取任务详情，并保留队列级总数。
// completed 的分数为完成时间加保留时长；本项目统一保留时长，因此默认列表可按该分数展示最新完成记录。
func (m *Manager) listDescZSetTaskInfoPage(ctx context.Context, internalQueue string, state string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key, err := keys.TaskAsynqStateZSetKey(internalQueue, state)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	pageStart, pageStop := taskListPageRange(page, pageSize)
	var (
		ids   []string
		total int64
	)
	if minScore, maxScore, ok := descZSetScoreRange(state, timeRange, m.completedRetention()); ok {
		total, err = m.redis.ZCount(ctx, key, minScore, maxScore).Result()
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if total <= 0 {
			return []*asynq.TaskInfo{}, 0, nil
		}
		ids, err = m.redis.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:    minScore,
			Max:    maxScore,
			Offset: pageStart,
			Count:  int64(pageSize),
		}).Result()
	} else {
		total, err = m.taskTotalByState(internalQueue, state)
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if total <= 0 {
			return []*asynq.TaskInfo{}, 0, nil
		}
		ids, err = m.redis.ZRevRange(ctx, key, pageStart, pageStop).Result()
	}
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	items := make([]*asynq.TaskInfo, 0, len(ids))
	for _, id := range ids {
		taskInfo, infoErr := m.inspector.GetTaskInfo(internalQueue, id)
		if errors.Is(infoErr, asynq.ErrTaskNotFound) {
			continue
		}
		if infoErr != nil {
			return nil, 0, errors.Tag(infoErr)
		}
		if taskInfo.State.String() != state {
			continue
		}
		items = append(items, taskInfo)
	}
	return items, total, nil
}

// descZSetScoreRange 返回可直接用 zset 分数过滤的任务时间范围。
// archived 分数就是失败时间；completed 分数是完成时间加保留时长，按当前统一保留配置反推查询范围。
func descZSetScoreRange(state string, timeRange taskListTimeRange, retention time.Duration) (string, string, bool) {
	if !timeRange.hasRange {
		return "", "", false
	}
	scoreOffset := int64(0)
	switch strings.TrimSpace(state) {
	case "archived":
	case "completed":
		scoreOffset = int64(retention.Seconds())
	default:
		return "", "", false
	}
	minScore := "-inf"
	maxScore := "+inf"
	if !timeRange.start.IsZero() {
		minScore = strconv.FormatInt(timeRange.start.Unix()+scoreOffset, 10)
	}
	if !timeRange.end.IsZero() {
		maxScore = strconv.FormatInt(timeRange.end.Unix()+scoreOffset, 10)
	}
	return minScore, maxScore, true
}

// taskTotalByState 返回队列中指定状态的任务总数。
func (m *Manager) taskTotalByState(internalQueue string, state string) (int64, error) {
	info, err := m.inspector.GetQueueInfo(internalQueue)
	if err != nil {
		return 0, errors.Tag(err)
	}
	return queueInfoTotalByState(info, state), nil
}

// listScheduledTaskInfoPage 按计划执行时间倒序读取 scheduled 任务。
// Asynq Inspector 的 ListScheduledTasks 固定按 NextProcessAt 升序且没有公开倒序选项，因此这里只对 scheduled 使用 zset 倒序取 ID，再复用 Inspector 装配任务详情。
func (m *Manager) listScheduledTaskInfoPage(ctx context.Context, internalQueue string, page int, pageSize int, timeRange taskListTimeRange) ([]*asynq.TaskInfo, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	total, err := m.scheduledTaskTotal(ctx, internalQueue, timeRange)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	if total <= 0 {
		return []*asynq.TaskInfo{}, 0, nil
	}
	ids, err := m.listScheduledTaskIDs(ctx, internalQueue, page, pageSize, timeRange)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	items := make([]*asynq.TaskInfo, 0, len(ids))
	for _, id := range ids {
		taskInfo, infoErr := m.inspector.GetTaskInfo(internalQueue, id)
		if errors.Is(infoErr, asynq.ErrTaskNotFound) {
			continue
		}
		if infoErr != nil {
			return nil, 0, errors.Tag(infoErr)
		}
		if taskInfo.State != asynq.TaskStateScheduled {
			continue
		}
		items = append(items, taskInfo)
	}
	return items, total, nil
}

// scheduledTaskTotal 返回 scheduled 总数；带时间段时按 nextProcessAt 分数范围统计。
func (m *Manager) scheduledTaskTotal(ctx context.Context, internalQueue string, timeRange taskListTimeRange) (int64, error) {
	if !timeRange.hasRange {
		info, err := m.inspector.GetQueueInfo(internalQueue)
		if err != nil {
			return 0, errors.Tag(err)
		}
		return int64(info.Scheduled), nil
	}
	minScore, maxScore := scheduledScoreRange(timeRange)
	total, err := m.redis.ZCount(ctx, keys.TaskAsynqScheduledKey(internalQueue), minScore, maxScore).Result()
	if err != nil {
		return 0, errors.Tag(err)
	}
	return total, nil
}

// listScheduledTaskIDs 按 scheduled.nextProcessAt 倒序读取任务 ID。
func (m *Manager) listScheduledTaskIDs(ctx context.Context, internalQueue string, page int, pageSize int, timeRange taskListTimeRange) ([]string, error) {
	pageStart, pageStop := taskListPageRange(page, pageSize)
	key := keys.TaskAsynqScheduledKey(internalQueue)
	if !timeRange.hasRange {
		return m.redis.ZRevRange(ctx, key, pageStart, pageStop).Result()
	}
	minScore, maxScore := scheduledScoreRange(timeRange)
	return m.redis.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    minScore,
		Max:    maxScore,
		Offset: pageStart,
		Count:  int64(pageSize),
	}).Result()
}

// scheduledScoreRange 把 nextProcessAt 时间范围转换为 Asynq scheduled zset 分数范围。
func scheduledScoreRange(timeRange taskListTimeRange) (string, string) {
	minScore := "-inf"
	maxScore := "+inf"
	if !timeRange.start.IsZero() {
		minScore = strconv.FormatInt(timeRange.start.Unix(), 10)
	}
	if !timeRange.end.IsZero() {
		maxScore = strconv.FormatInt(timeRange.end.Unix(), 10)
	}
	return minScore, maxScore
}

// taskListPageRange 把接口分页参数转换为 Redis 闭区间下标。
func taskListPageRange(page int, pageSize int) (int64, int64) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := int64((page - 1) * pageSize)
	return start, start + int64(pageSize) - 1
}

// listTaskItemsByFilters 在指定队列和状态下扫描任务，并按时间倒序过滤后分页。
// 管理接口层补充倒序和时间段过滤，保证列表排序稳定。
func (m *Manager) listTaskItemsByFilters(ctx context.Context, internalQueue string, internalGroup string, state string, taskID string, workflowID string, taskName string, page int, pageSize int, timeRange taskListTimeRange, periodicNextRuns []periodicNextRun) ([]types.TaskItem, int64, error) {
	matchedTasks := make([]types.TaskItem, 0)
	taskID = strings.TrimSpace(taskID)
	workflowID = strings.TrimSpace(workflowID)
	taskName = strings.TrimSpace(taskName)
	for currentPage := 1; currentPage <= taskListFilterMaxPages; currentPage++ {
		items, _, err := m.listTaskInfoPage(ctx, internalQueue, internalGroup, state, currentPage, taskListFilterPageSize, timeRange)
		if err != nil {
			return nil, 0, errors.Tag(err)
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			taskItem := m.toTaskItemWithPeriodicNextRuns(ctx, item, periodicNextRuns)
			if !taskItemMatchesListFilters(taskItem, taskID, workflowID, taskName) {
				continue
			}
			if !timeRange.Contains(taskItem) {
				continue
			}
			matchedTasks = append(matchedTasks, taskItem)
		}
		if len(items) < taskListFilterPageSize {
			break
		}
	}
	sortTaskItemsByTimeDesc(matchedTasks)
	total := int64(len(matchedTasks))
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start >= len(matchedTasks) {
		return []types.TaskItem{}, total, nil
	}
	end := min(start+pageSize, len(matchedTasks))
	return matchedTasks[start:end], total, nil
}

// GetTaskInfo 查询单个任务的详情。
func (m *Manager) GetTaskInfo(ctx context.Context, req *types.GetTaskInfoReq) (*types.TaskItem, error) {
	if !m.IsEnabled() {
		return nil, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, errors.Errorf("任务详情请求不能为空")
	}
	_ = ctx
	info, err := m.inspector.GetTaskInfo(m.namespacedQueueName(strings.TrimSpace(req.Queue)), strings.TrimSpace(req.TaskID))
	if err != nil {
		return nil, errors.Tag(err)
	}
	return new(m.toTaskItem(ctx, info)), nil
}
