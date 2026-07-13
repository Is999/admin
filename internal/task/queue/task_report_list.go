package taskqueue

import (
	"context"
	"math"
	"sort"
	"strings"

	keys "admin/common/rediskeys"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// taskReportListBatchSize 是日报复用 Asynq 批量列表脚本时的固定批量。
	taskReportListBatchSize = 100
	// taskReportNativeEntryOverheadBytes 为每条 Redis 回包预留协议、容器和解码内存裕量。
	taskReportNativeEntryOverheadBytes = 1024
)

// reportRankedTask 保存 Asynq zset 绝对排名和对应任务，用于把原生升序页还原成日报倒序页。
type reportRankedTask struct {
	rank int64           // 任务在终态 zset 中的升序绝对排名
	info *asynq.TaskInfo // Asynq 任务详情
}

// reportContextRedis 把 Asynq Inspector 内部的后台 context 收口到本轮日报 context。
type reportContextRedis struct {
	redis.UniversalClient                 // 底层任务 Redis 客户端
	ctx                   context.Context // 日报任务的超时与取消信号
}

// SIsMember 让 Inspector 的队列存在性检查响应日报取消。
func (c *reportContextRedis) SIsMember(_ context.Context, key string, member interface{}) *redis.BoolCmd {
	return c.UniversalClient.SIsMember(c.ctx, key, member)
}

// EvalSha 让已缓存的 Asynq 详情脚本响应日报取消。
func (c *reportContextRedis) EvalSha(_ context.Context, sha1 string, keys []string, args ...interface{}) *redis.Cmd {
	return c.UniversalClient.EvalSha(c.ctx, sha1, keys, args...)
}

// Eval 让 Asynq 详情脚本的冷缓存回退响应日报取消。
func (c *reportContextRedis) Eval(_ context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return c.UniversalClient.Eval(c.ctx, script, keys, args...)
}

// ListReportTasks 按终态时间窗口批量读取日报任务。
//
// 该入口先用 zset 统计定位窗口排名，再复用 Asynq 单页 Lua 一次读取整批任务，避免通用列表逐条
// GetTaskInfo 产生多次 Redis 往返。offset 和 limit 基于窗口内倒序结果，limit 最大为 100。
func (m *Manager) ListReportTasks(ctx context.Context, req *types.ListTaskItemsReq, offset, limit int, byteBudget int64) (*types.TaskListResp, types.TaskReportScanUsage, error) {
	if !m.IsEnabled() {
		return nil, types.TaskReportScanUsage{}, ErrTaskQueueDisabled
	}
	if req == nil {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表请求不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if offset < 0 {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表 offset 不能小于 0")
	}
	if limit <= 0 || limit > taskReportListBatchSize {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表 limit 必须在 1 到 %d 之间", taskReportListBatchSize)
	}
	if byteBudget <= 0 {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表字节预算必须大于 0")
	}
	queueName := strings.TrimSpace(req.Queue)
	state := strings.TrimSpace(req.State)
	if state != asynq.TaskStateArchived.String() && state != asynq.TaskStateCompleted.String() {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表不支持状态: %s", state)
	}
	timeRange, err := parseTaskListTimeRange(strings.TrimSpace(req.StartTime), strings.TrimSpace(req.EndTime))
	if err != nil {
		return nil, types.TaskReportScanUsage{}, errors.Tag(err)
	}
	if !timeRange.hasRange {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务列表必须提供统计时间窗口")
	}
	internalQueue := m.namespacedQueueName(queueName)
	if internalQueue == "" {
		return nil, types.TaskReportScanUsage{}, errors.Errorf("日报任务队列名无效: %s", queueName)
	}
	infos, total, usage, err := m.listReportTaskInfoSlice(ctx, internalQueue, state, timeRange, offset, limit, byteBudget)
	if err != nil {
		return nil, usage, errors.Tag(err)
	}
	if usage.ByteLimited {
		return &types.TaskListResp{Queue: queueName, State: state, StartTime: strings.TrimSpace(req.StartTime), EndTime: strings.TrimSpace(req.EndTime), PageSize: limit, Total: total, Tasks: []types.TaskItem{}}, usage, nil
	}
	periodicInfos := reportPeriodicTaskInfos(infos)
	runtimeRecords, runtimeBytes, runtimeLimited, err := m.readTaskRuntimes(ctx, periodicInfos, byteBudget-usage.Bytes)
	if err != nil {
		return nil, usage, errors.Tag(err)
	}
	usage.Bytes += runtimeBytes
	if runtimeLimited {
		usage.ByteLimited = true
		return &types.TaskListResp{Queue: queueName, State: state, StartTime: strings.TrimSpace(req.StartTime), EndTime: strings.TrimSpace(req.EndTime), PageSize: limit, Total: total, Tasks: []types.TaskItem{}}, usage, nil
	}
	resp := &types.TaskListResp{
		Queue:     queueName,
		State:     state,
		StartTime: strings.TrimSpace(req.StartTime),
		EndTime:   strings.TrimSpace(req.EndTime),
		PageSize:  limit,
		Total:     total,
		Tasks:     make([]types.TaskItem, 0, len(infos)),
	}
	for _, info := range infos {
		if info == nil {
			continue
		}
		item := m.toTaskItemWithRuntime(info, nil, runtimeRecords[taskRuntimeIdentity(info.Queue, info.ID)])
		// 日报只保留聚合必需字段，避免跨页累积原始 payload/result 内存。
		item.Payload = nil
		item.Result = nil
		resp.Tasks = append(resp.Tasks, item)
	}
	return resp, usage, nil
}

// listReportTaskInfoSlice 定位窗口内倒序切片，并按 Asynq 原生批量页读取任务详情。
func (m *Manager) listReportTaskInfoSlice(ctx context.Context, internalQueue, state string, timeRange taskListTimeRange, offset, limit int, byteBudget int64) ([]*asynq.TaskInfo, int64, types.TaskReportScanUsage, error) {
	infos, total, stable, usage, err := m.listReportTaskInfoSnapshot(ctx, internalQueue, state, timeRange, offset, limit, byteBudget)
	if err != nil {
		return nil, 0, usage, errors.Tag(err)
	}
	if usage.ByteLimited {
		return nil, total, usage, nil
	}
	if !stable {
		return nil, 0, usage, errors.Errorf("日报任务终态集合发生变化，无法取得一致快照 state=%s", state)
	}
	return infos, total, usage, nil
}

// listReportTaskInfoSnapshot 读取一次终态任务切片，并校验批量详情仍对应同一窗口排名快照。
func (m *Manager) listReportTaskInfoSnapshot(ctx context.Context, internalQueue, state string, timeRange taskListTimeRange, offset, limit int, byteBudget int64) ([]*asynq.TaskInfo, int64, bool, types.TaskReportScanUsage, error) {
	key, err := keys.TaskAsynqStateZSetKey(internalQueue, state)
	if err != nil {
		return nil, 0, false, types.TaskReportScanUsage{}, errors.Tag(err)
	}
	minScore, maxScore, ok := descZSetScoreRange(state, timeRange, taskCompletedRetention)
	if !ok {
		return nil, 0, false, types.TaskReportScanUsage{}, errors.Errorf("日报任务状态不支持时间索引: %s", state)
	}
	// MULTI/EXEC 保证窗口数量和绝对排名来自同一个 Redis 时刻。
	pipe := m.redis.TxPipeline()
	windowTotalCmd := pipe.ZCount(ctx, key, minScore, maxScore)
	throughMaxCmd := pipe.ZCount(ctx, key, "-inf", maxScore)
	_, execErr := pipe.Exec(ctx)
	pipe.Discard()
	if execErr != nil {
		return nil, 0, false, types.TaskReportScanUsage{}, errors.Wrap(execErr, "统计日报任务窗口排名失败")
	}
	windowTotal := windowTotalCmd.Val()
	if windowTotal <= 0 || int64(offset) >= windowTotal {
		return []*asynq.TaskInfo{}, windowTotal, true, types.TaskReportScanUsage{}, nil
	}
	take := min(int64(limit), windowTotal-int64(offset))
	highRank := throughMaxCmd.Val() - 1 - int64(offset)
	pageStart := highRank / taskReportListBatchSize * taskReportListBatchSize
	// 单次只消费一个 Asynq 原生页；调用方继续推进 offset，避免跨页重复读取完整 msg/result。
	take = min(take, highRank-pageStart+1)
	lowRank := highRank - take + 1
	if lowRank < 0 || highRank < lowRank || highRank >= throughMaxCmd.Val() {
		return nil, 0, false, types.TaskReportScanUsage{}, errors.Errorf("日报任务窗口排名异常 state=%s low=%d high=%d through_max=%d", state, lowRank, highRank, throughMaxCmd.Val())
	}
	usage := types.TaskReportScanUsage{Tasks: taskReportListBatchSize}
	pageStop := pageStart + taskReportListBatchSize - 1
	pageIDs, pageBytes, limited, sizeErr := m.preflightReportNativePage(ctx, key, internalQueue, pageStart, pageStop, byteBudget)
	if sizeErr != nil {
		return nil, 0, false, usage, errors.Tag(sizeErr)
	}
	usage.Bytes = pageBytes
	usage.ByteLimited = limited
	if limited {
		return nil, windowTotal, true, usage, nil
	}
	ranked := make([]reportRankedTask, 0, int(take))
	firstPage := lowRank / taskReportListBatchSize
	lastPage := highRank / taskReportListBatchSize
	for page := firstPage; page <= lastPage; page++ {
		if err = ctx.Err(); err != nil {
			return nil, 0, false, usage, errors.Tag(err)
		}
		pageItems, pageErr := m.listReportNativePage(ctx, internalQueue, state, int(page)+1)
		if pageErr != nil {
			return nil, 0, false, usage, errors.Tag(pageErr)
		}
		if !reportNativePageIDsMatch(pageIDs, pageItems) {
			return nil, windowTotal, false, usage, nil
		}
		pageStart := page * taskReportListBatchSize
		requiredStop := min(highRank, pageStart+taskReportListBatchSize-1)
		requiredLength := requiredStop - pageStart + 1
		if int64(len(pageItems)) < requiredLength {
			return nil, 0, false, usage, errors.Errorf("日报任务批量详情不完整 state=%s page=%d got=%d want_at_least=%d", state, page+1, len(pageItems), requiredLength)
		}
		for index, info := range pageItems {
			rank := pageStart + int64(index)
			if rank < lowRank || rank > highRank {
				continue
			}
			ranked = append(ranked, reportRankedTask{rank: rank, info: info})
		}
	}
	if int64(len(ranked)) != take {
		return nil, 0, false, usage, errors.Errorf("日报任务窗口详情数量异常 state=%s got=%d want=%d", state, len(ranked), take)
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].rank < ranked[j].rank
	})
	stable, err := m.reportTaskSnapshotStable(ctx, key, minScore, maxScore, windowTotal, throughMaxCmd.Val(), lowRank, highRank, ranked)
	if err != nil {
		return nil, 0, false, usage, errors.Tag(err)
	}
	if !stable {
		return nil, windowTotal, false, usage, nil
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].rank > ranked[j].rank
	})
	result := make([]*asynq.TaskInfo, 0, len(ranked))
	for _, item := range ranked {
		result = append(result, item.info)
	}
	return result, windowTotal, true, usage, nil
}

// preflightReportNativePage 在调用 Asynq 详情 Lua 前计算整个物理页回包上界。
func (m *Manager) preflightReportNativePage(ctx context.Context, stateKey, internalQueue string, pageStart, pageStop int64, byteBudget int64) ([]string, int64, bool, error) {
	ids, err := m.redis.ZRange(ctx, stateKey, pageStart, pageStop).Result()
	if err != nil {
		return nil, 0, false, errors.Wrap(err, "读取日报原生页任务 ID 失败")
	}
	if len(ids) == 0 {
		return ids, 0, false, nil
	}
	type entrySizeCommands struct {
		msg    *redis.IntCmd // msg 是 Asynq protobuf 任务消息字节数
		result *redis.IntCmd // result 是任务结果字节数
	}
	commands := make([]entrySizeCommands, 0, len(ids))
	pipe := m.redis.Pipeline()
	for _, id := range ids {
		taskKey := keys.TaskAsynqTaskHashKey(internalQueue, id)
		commands = append(commands, entrySizeCommands{
			msg:    pipe.HStrLen(ctx, taskKey, "msg"),
			result: pipe.HStrLen(ctx, taskKey, "result"),
		})
	}
	_, execErr := pipe.Exec(ctx)
	pipe.Discard()
	if execErr != nil {
		return nil, 0, false, errors.Wrap(execErr, "预检日报原生页字节数失败")
	}
	totalBytes := int64(0)
	for index, command := range commands {
		msgBytes, msgErr := command.msg.Result()
		if msgErr != nil {
			return nil, 0, false, errors.Wrapf(msgErr, "读取日报任务消息大小失败 task_id=%s", ids[index])
		}
		resultBytes, resultErr := command.result.Result()
		if resultErr != nil {
			return nil, 0, false, errors.Wrapf(resultErr, "读取日报任务结果大小失败 task_id=%s", ids[index])
		}
		if msgBytes <= 0 || resultBytes < 0 {
			return nil, 0, false, errors.Errorf("日报任务 hash 不完整 task_id=%s msg=%d result=%d", ids[index], msgBytes, resultBytes)
		}
		entryBytes := msgBytes + resultBytes + taskReportNativeEntryOverheadBytes
		if entryBytes < 0 || totalBytes > math.MaxInt64-entryBytes {
			return nil, 0, false, errors.Errorf("日报原生页字节数溢出")
		}
		totalBytes += entryBytes
		if totalBytes > byteBudget {
			return ids, 0, true, nil
		}
	}
	return ids, totalBytes, false, nil
}

// reportNativePageIDsMatch 确认字节预检与 Asynq 详情 Lua 读取的是同一物理页。
func reportNativePageIDsMatch(ids []string, infos []*asynq.TaskInfo) bool {
	if len(ids) != len(infos) {
		return false
	}
	for index, id := range ids {
		if infos[index] == nil || infos[index].ID != id {
			return false
		}
	}
	return true
}

// reportPeriodicTaskInfos 只为周期来源任务读取日报运行快照。
func reportPeriodicTaskInfos(infos []*asynq.TaskInfo) []*asynq.TaskInfo {
	result := make([]*asynq.TaskInfo, 0, len(infos))
	for _, info := range infos {
		if info != nil && strings.TrimSpace(info.Headers[headerTaskSource]) == WorkflowSourcePeriodic {
			result = append(result, info)
		}
	}
	return result
}

// reportTaskSnapshotStable 原子复核窗口数量、排名边界和任务 ID，防止清理或裁剪并发移动原生分页。
func (m *Manager) reportTaskSnapshotStable(ctx context.Context, key, minScore, maxScore string, windowTotal, throughMax, lowRank, highRank int64, ranked []reportRankedTask) (bool, error) {
	pipe := m.redis.TxPipeline()
	windowTotalCmd := pipe.ZCount(ctx, key, minScore, maxScore)
	throughMaxCmd := pipe.ZCount(ctx, key, "-inf", maxScore)
	idsCmd := pipe.ZRange(ctx, key, lowRank, highRank)
	_, execErr := pipe.Exec(ctx)
	pipe.Discard()
	if execErr != nil {
		return false, errors.Wrap(execErr, "复核日报任务窗口快照失败")
	}
	if windowTotalCmd.Val() != windowTotal || throughMaxCmd.Val() != throughMax {
		return false, nil
	}
	ids := idsCmd.Val()
	if len(ids) != len(ranked) {
		return false, nil
	}
	for index, id := range ids {
		if ranked[index].info == nil || ranked[index].info.ID != id {
			return false, nil
		}
	}
	return true, nil
}

// listReportNativePage 通过支持调用方取消的 Asynq 原生批量 Lua 读取一个终态任务页。
func (m *Manager) listReportNativePage(ctx context.Context, internalQueue, state string, page int) ([]*asynq.TaskInfo, error) {
	if ctx == nil {
		return nil, errors.New("日报任务详情 context 不能为空")
	}
	inspector := asynq.NewInspectorFromRedisClient(&reportContextRedis{UniversalClient: m.redis, ctx: ctx})
	opts := []asynq.ListOption{asynq.Page(page), asynq.PageSize(taskReportListBatchSize)}
	switch state {
	case asynq.TaskStateArchived.String():
		return inspector.ListArchivedTasks(internalQueue, opts...)
	case asynq.TaskStateCompleted.String():
		return inspector.ListCompletedTasks(internalQueue, opts...)
	default:
		return nil, errors.Errorf("日报任务批量读取不支持状态: %s", state)
	}
}
