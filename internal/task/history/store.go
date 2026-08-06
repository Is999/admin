package taskhistory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"admin/internal/model"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// historyCreateBatchSize 限制单条 INSERT 的行数和 SQL 体积。
	historyCreateBatchSize = 50
	// historySnapshotMaxBytes 与完整事件硬上限一致，并为落库元数据保留工作流快照预算之外的空间。
	historySnapshotMaxBytes = 128 << 10
	// legacyPeriodicPrefix 是旧版误写入周期名称字段的展示前缀，仅用于短期历史兼容读取。
	legacyPeriodicPrefix = "周期任务触发:"
)

// Store 使用主库保存短期工作流汇总和最终失败明细。
type Store struct {
	appID   string   // 应用命名空间
	readDB  *gorm.DB // 只读查询连接
	writeDB *gorm.DB // 主库写连接
}

// New 创建任务终态历史存储。
func New(appID string, readDB *gorm.DB, writeDB *gorm.DB) *Store {
	appID = strings.TrimSpace(appID)
	if appID == "" || readDB == nil || writeDB == nil {
		return nil
	}
	return &Store{appID: appID, readDB: readDB, writeDB: writeDB}
}

// Persist 在一个短事务内幂等批量写入终态历史。
func (s *Store) Persist(ctx context.Context, events []taskqueue.HistoryEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	taskRows := make([]model.TaskRun, 0, len(events))
	workflowRows := make([]model.TaskWorkflowRun, 0, len(events))
	failureRows := make([]model.TaskFailure, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case "task":
			row, err := s.taskRow(event)
			if err != nil {
				return errors.Tag(err)
			}
			taskRows = append(taskRows, row)
		case "workflow":
			row, err := s.workflowRow(event)
			if err != nil {
				return errors.Tag(err)
			}
			workflowRows = append(workflowRows, row)
		case "failure":
			row, err := s.failureRow(event)
			if err != nil {
				return errors.Tag(err)
			}
			failureRows = append(failureRows, row)
		default:
			return errors.Errorf("不支持的任务历史事件类型: %s", event.Kind)
		}
	}
	return errors.Tag(s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(taskRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&taskRows, historyCreateBatchSize).Error; err != nil {
				return errors.Wrap(err, "批量写入任务终态历史失败")
			}
		}
		if len(workflowRows) > 0 {
			if err := tx.Clauses(workflowUpsertClause()).CreateInBatches(&workflowRows, historyCreateBatchSize).Error; err != nil {
				return errors.Wrap(err, "批量写入工作流历史失败")
			}
		}
		if len(failureRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&failureRows, historyCreateBatchSize).Error; err != nil {
				return errors.Wrap(err, "批量写入任务失败历史失败")
			}
		}
		return nil
	}))
}

// taskRow 把单个任务终态事件转换为有界摘要行。
func (s *Store) taskRow(event taskqueue.HistoryEvent) (model.TaskRun, error) {
	if event.Task == nil || strings.TrimSpace(event.EventID) == "" {
		return model.TaskRun{}, errors.Errorf("任务终态历史事件缺少快照或事件 ID")
	}
	snapshot := *event.Task
	persistedAt := time.Now()
	snapshot.DataSource = "database"
	snapshot.PersistedAt = persistedAt.Format(time.RFC3339)
	raw, err := json.Marshal(&snapshot)
	if err != nil {
		return model.TaskRun{}, errors.Wrap(err, "序列化任务终态历史快照失败")
	}
	if len(raw) > historySnapshotMaxBytes {
		return model.TaskRun{}, errors.Errorf("任务终态历史快照超过上限 task_id=%s bytes=%d", snapshot.TaskID, len(raw))
	}
	startedAt, err := parseHistoryTime(snapshot.StartedAt)
	if err != nil {
		return model.TaskRun{}, errors.Wrap(err, "解析任务开始时间失败")
	}
	finishedAt, err := parseHistoryTime(snapshot.FinishedAt)
	if err != nil {
		return model.TaskRun{}, errors.Wrap(err, "解析任务完成时间失败")
	}
	return model.TaskRun{
		AppID: s.appID, EventID: event.EventID, TaskID: truncateText(snapshot.TaskID, 128),
		TaskType: truncateText(snapshot.TaskType, 128), TaskName: truncateText(snapshot.TaskName, 128),
		Queue: truncateText(snapshot.Queue, 128), Source: truncateText(snapshot.Source, 32),
		PeriodicName: truncateText(snapshot.PeriodicName, 128), Status: snapshot.Status,
		WorkflowID: truncateText(snapshot.WorkflowID, 128), WorkflowName: truncateText(snapshot.WorkflowName, 128),
		WorkflowNode: truncateText(snapshot.WorkflowNode, 128), ShardIndex: max(snapshot.ShardIndex, 0), ShardTotal: max(snapshot.ShardTotal, 0),
		Retried: max(snapshot.Retried, 0), MaxRetry: max(snapshot.MaxRetry, 0),
		TraceID: truncateText(snapshot.TraceID, 64), TraceTotal: snapshot.TraceTotal,
		TraceRead: snapshot.TraceRead, TraceWrite: snapshot.TraceWrite, TraceDelete: snapshot.TraceDelete,
		TraceError: snapshot.TraceError, DurationMS: max(snapshot.DurationMS, 0),
		ErrorMessage: truncateText(snapshot.ErrorMessage, 1000), SnapshotJSON: string(raw),
		StartedAt: startedAt, FinishedAt: finishedAt, CreatedAt: persistedAt,
	}, nil
}

// workflowUpsertClause 让同一工作流的手工重跑覆盖旧终态，避免历史列表和日报重复计数。
func workflowUpsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "app_id"}, {Name: "workflow_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"event_id", "workflow_name", "periodic_name", "source", "queue", "status",
			"node_total", "task_total", "succeeded", "failed", "skipped",
			"trace_total", "trace_read", "trace_write", "trace_delete", "trace_error",
			"duration_ms", "error_message", "snapshot_json", "workflow_created_at", "finished_at", "created_at",
		}),
	}
}

// workflowRow 把已经限流裁剪的工作流快照转换为终态汇总行。
func (s *Store) workflowRow(event taskqueue.HistoryEvent) (model.TaskWorkflowRun, error) {
	if event.Workflow == nil || strings.TrimSpace(event.EventID) == "" {
		return model.TaskWorkflowRun{}, errors.Errorf("工作流历史事件缺少快照或事件 ID")
	}
	snapshot := *event.Workflow
	snapshot.Targets = nil
	persistedAt := time.Now()
	snapshot.DataSource = "database"
	snapshot.HistoryStatus = "persisted"
	snapshot.PersistedAt = persistedAt.Format(time.RFC3339)
	raw, err := json.Marshal(&snapshot)
	if err != nil {
		return model.TaskWorkflowRun{}, errors.Wrap(err, "序列化工作流历史快照失败")
	}
	if len(raw) > historySnapshotMaxBytes {
		return model.TaskWorkflowRun{}, errors.Errorf("工作流历史快照超过上限 workflow_id=%s bytes=%d", snapshot.WorkflowID, len(raw))
	}
	createdAt, err := parseHistoryTime(snapshot.CreatedAt)
	if err != nil {
		return model.TaskWorkflowRun{}, errors.Wrap(err, "解析工作流创建时间失败")
	}
	finishedAt, err := parseHistoryTime(snapshot.FinishedAt)
	if err != nil {
		return model.TaskWorkflowRun{}, errors.Wrap(err, "解析工作流完成时间失败")
	}
	row := model.TaskWorkflowRun{
		AppID:             s.appID,
		EventID:           event.EventID,
		WorkflowID:        snapshot.WorkflowID,
		WorkflowName:      truncateText(snapshot.WorkflowName, 128),
		PeriodicName:      truncateText(snapshot.PeriodicName, 128),
		Source:            truncateText(snapshot.Source, 32),
		Queue:             truncateText(snapshot.Queue, 128),
		Status:            snapshot.Status,
		NodeTotal:         len(snapshot.Nodes),
		DurationMS:        max(snapshot.DurationMS, 0),
		ErrorMessage:      truncateText(snapshot.ErrorMessage, 1000),
		SnapshotJSON:      string(raw),
		WorkflowCreatedAt: createdAt,
		FinishedAt:        finishedAt,
		CreatedAt:         persistedAt,
	}
	for _, node := range snapshot.Nodes {
		row.TaskTotal += max(node.Expected, node.Succeeded+node.Failed+node.Skipped)
		row.Succeeded += node.Succeeded
		row.Failed += node.Failed
		row.Skipped += node.Skipped
	}
	if snapshot.ExecutionTrace != nil {
		row.TraceTotal = snapshot.ExecutionTrace.TotalCount
		row.TraceRead = snapshot.ExecutionTrace.ReadCount
		row.TraceWrite = snapshot.ExecutionTrace.InsertCount + snapshot.ExecutionTrace.UpdateCount + snapshot.ExecutionTrace.UpsertCount
		row.TraceDelete = snapshot.ExecutionTrace.DeleteCount
		row.TraceError = snapshot.ExecutionTrace.ErrorCount
	}
	return row, nil
}

// failureRow 把最终失败事件转换为不含业务载荷的排障行。
func (s *Store) failureRow(event taskqueue.HistoryEvent) (model.TaskFailure, error) {
	if event.Failure == nil || strings.TrimSpace(event.EventID) == "" {
		return model.TaskFailure{}, errors.Errorf("任务失败历史事件缺少快照或事件 ID")
	}
	failedAt, err := parseHistoryTime(event.Failure.FailedAt)
	if err != nil {
		return model.TaskFailure{}, errors.Wrap(err, "解析任务失败时间失败")
	}
	return model.TaskFailure{
		AppID:        s.appID,
		EventID:      event.EventID,
		TaskID:       event.Failure.TaskID,
		TaskType:     truncateText(event.Failure.TaskType, 128),
		TaskName:     truncateText(event.Failure.TaskName, 128),
		Queue:        truncateText(event.Failure.Queue, 128),
		Source:       truncateText(event.Failure.Source, 32),
		PeriodicName: truncateText(event.Failure.PeriodicName, 128),
		WorkflowID:   event.Failure.WorkflowID,
		WorkflowName: truncateText(event.Failure.WorkflowName, 128),
		WorkflowNode: truncateText(event.Failure.WorkflowNode, 128),
		Retried:      max(event.Failure.Retried, 0),
		MaxRetry:     max(event.Failure.MaxRetry, 0),
		ErrorMessage: truncateText(event.Failure.ErrorMessage, 1000),
		TraceID:      truncateText(event.Failure.TraceID, 64),
		FailedAt:     failedAt,
		CreatedAt:    time.Now(),
	}, nil
}

// GetTaskRun 返回指定任务终态历史详情。
func (s *Store) GetTaskRun(ctx context.Context, id uint64) (*types.TaskRunHistoryItem, error) {
	if s == nil || id == 0 {
		return nil, redis.Nil
	}
	var row model.TaskRun
	err := s.readDB.WithContext(ctx).Model(&model.TaskRun{}).
		Where("app_id = ? AND id = ?", s.appID, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, redis.Nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "查询任务终态历史详情失败")
	}
	var snapshot types.TaskRunHistoryItem
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		return nil, errors.Wrap(err, "解析任务终态历史快照失败")
	}
	snapshot.ID = row.ID
	snapshot.DataSource = "database"
	snapshot.PersistedAt = row.CreatedAt.Format(time.RFC3339)
	return &snapshot, nil
}

// ListTaskRuns 使用 finished_at + id 游标查询全部任务终态历史。
func (s *Store) ListTaskRuns(ctx context.Context, req *types.ListTaskRunsReq) (*types.TaskRunHistoryListResp, error) {
	if s == nil || req == nil {
		return nil, errors.Errorf("任务终态历史存储或请求为空")
	}
	start, end, err := parseHistoryRange(req.StartTime, req.EndTime)
	if err != nil {
		return nil, errors.Tag(err)
	}
	query := s.readDB.WithContext(ctx).Model(&model.TaskRun{}).
		Where("app_id = ? AND finished_at >= ? AND finished_at < ?", s.appID, start, end)
	query = applyTaskRunFilters(query, req)
	if cursorAt, cursorID, ok := decodeHistoryCursor(req.Cursor); ok {
		query = query.Where("(finished_at < ? OR (finished_at = ? AND id < ?))", cursorAt, cursorAt, cursorID)
	} else if strings.TrimSpace(req.Cursor) != "" {
		return nil, errors.Errorf("任务终态历史 cursor 非法")
	}
	rows := make([]model.TaskRun, 0, req.PageSize+1)
	if err = query.Select("id, task_id, task_type, task_name, queue, source, periodic_name, workflow_id, workflow_name, workflow_node, shard_index, shard_total, status, retried, max_retry, trace_id, trace_total, trace_read, trace_write, trace_delete, trace_error, duration_ms, error_message, started_at, finished_at, created_at").
		Order("finished_at DESC").Order("id DESC").Limit(req.PageSize + 1).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询任务终态历史列表失败")
	}
	resp := &types.TaskRunHistoryListResp{Items: make([]types.TaskRunHistoryItem, 0, min(len(rows), req.PageSize)), DataSource: "database"}
	if len(rows) > req.PageSize {
		resp.HasMore = true
		rows = rows[:req.PageSize]
	}
	for _, row := range rows {
		resp.Items = append(resp.Items, taskRunHistoryItem(row))
	}
	if resp.HasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeHistoryCursor(last.FinishedAt, last.ID)
	}
	return resp, nil
}

// GetWorkflow 返回指定工作流最新一次终态历史快照。
func (s *Store) GetWorkflow(ctx context.Context, workflowID string) (*types.TaskWorkflowStatusResp, error) {
	if s == nil {
		return nil, redis.Nil
	}
	var row model.TaskWorkflowRun
	err := s.readDB.WithContext(ctx).Model(&model.TaskWorkflowRun{}).
		Where("app_id = ? AND workflow_id = ?", s.appID, strings.TrimSpace(workflowID)).
		Order("finished_at DESC").Order("id DESC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, redis.Nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "查询工作流历史失败")
	}
	var snapshot types.TaskWorkflowStatusResp
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		return nil, errors.Wrap(err, "解析工作流历史快照失败")
	}
	snapshot.PeriodicName = normalizePeriodicName(snapshot.PeriodicName)
	snapshot.DataSource = "database"
	snapshot.HistoryStatus = "persisted"
	snapshot.PersistedAt = row.CreatedAt.Format(time.RFC3339)
	return &snapshot, nil
}

// ListWorkflows 使用 finished_at + id 游标查询有界工作流历史。
func (s *Store) ListWorkflows(ctx context.Context, req *types.ListTaskWorkflowsReq) (*types.TaskWorkflowHistoryListResp, error) {
	if s == nil || req == nil {
		return nil, errors.Errorf("工作流历史存储或请求为空")
	}
	start, end, err := parseHistoryRange(req.StartTime, req.EndTime)
	if err != nil {
		return nil, errors.Tag(err)
	}
	query := s.readDB.WithContext(ctx).Model(&model.TaskWorkflowRun{}).
		Where("app_id = ? AND finished_at >= ? AND finished_at < ?", s.appID, start, end)
	query = applyWorkflowFilters(query, req)
	if cursorAt, cursorID, ok := decodeHistoryCursor(req.Cursor); ok {
		query = query.Where("(finished_at < ? OR (finished_at = ? AND id < ?))", cursorAt, cursorAt, cursorID)
	} else if strings.TrimSpace(req.Cursor) != "" {
		return nil, errors.Errorf("工作流历史 cursor 非法")
	}
	rows := make([]model.TaskWorkflowRun, 0, req.PageSize+1)
	if err = query.Select("id, workflow_id, workflow_name, periodic_name, source, queue, status, node_total, task_total, succeeded, failed, skipped, trace_total, trace_error, duration_ms, error_message, workflow_created_at, finished_at, created_at").
		Order("finished_at DESC").Order("id DESC").Limit(req.PageSize + 1).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询工作流历史列表失败")
	}
	resp := &types.TaskWorkflowHistoryListResp{Items: make([]types.TaskWorkflowHistoryItem, 0, min(len(rows), req.PageSize)), DataSource: "database"}
	if len(rows) > req.PageSize {
		resp.HasMore = true
		rows = rows[:req.PageSize]
	}
	for _, row := range rows {
		resp.Items = append(resp.Items, workflowHistoryItem(row))
	}
	if resp.HasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeHistoryCursor(last.FinishedAt, last.ID)
	}
	return resp, nil
}

// ListFailures 使用 failed_at + id 游标查询有界失败历史。
func (s *Store) ListFailures(ctx context.Context, req *types.ListTaskFailuresReq) (*types.TaskFailureListResp, error) {
	if s == nil || req == nil {
		return nil, errors.Errorf("失败历史存储或请求为空")
	}
	start, end, err := parseHistoryRange(req.StartTime, req.EndTime)
	if err != nil {
		return nil, errors.Tag(err)
	}
	query := s.readDB.WithContext(ctx).Model(&model.TaskFailure{}).
		Where("app_id = ? AND failed_at >= ? AND failed_at < ?", s.appID, start, end)
	query = applyFailureFilters(query, req)
	if cursorAt, cursorID, ok := decodeHistoryCursor(req.Cursor); ok {
		query = query.Where("(failed_at < ? OR (failed_at = ? AND id < ?))", cursorAt, cursorAt, cursorID)
	} else if strings.TrimSpace(req.Cursor) != "" {
		return nil, errors.Errorf("失败历史 cursor 非法")
	}
	rows := make([]model.TaskFailure, 0, req.PageSize+1)
	if err = query.Order("failed_at DESC").Order("id DESC").Limit(req.PageSize + 1).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询失败历史列表失败")
	}
	resp := &types.TaskFailureListResp{Items: make([]types.TaskFailureItem, 0, min(len(rows), req.PageSize)), DataSource: "database"}
	if len(rows) > req.PageSize {
		resp.HasMore = true
		rows = rows[:req.PageSize]
	}
	for _, row := range rows {
		resp.Items = append(resp.Items, types.TaskFailureItem{
			ID: row.ID, TaskID: row.TaskID, TaskType: row.TaskType, TaskName: row.TaskName,
			Queue: row.Queue, Source: row.Source, PeriodicName: normalizePeriodicName(row.PeriodicName),
			WorkflowID: row.WorkflowID, WorkflowName: row.WorkflowName,
			WorkflowNode: row.WorkflowNode, Retried: row.Retried, MaxRetry: row.MaxRetry,
			ErrorMessage: row.ErrorMessage, TraceID: row.TraceID, FailedAt: row.FailedAt.Format(time.RFC3339),
			DataSource: "database",
		})
	}
	if resp.HasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeHistoryCursor(last.FailedAt, last.ID)
	}
	return resp, nil
}

// applyFailureFilters 使用组合索引等值过滤最终失败任务。
func applyFailureFilters(query *gorm.DB, req *types.ListTaskFailuresReq) *gorm.DB {
	if value := strings.TrimSpace(req.TaskID); value != "" {
		query = query.Where("task_id = ?", value)
	}
	if value := strings.TrimSpace(req.WorkflowID); value != "" {
		query = query.Where("workflow_id = ?", value)
	}
	if value := strings.TrimSpace(req.TaskType); value != "" {
		query = query.Where("task_type = ?", value)
	}
	if value := strings.TrimSpace(req.TaskName); value != "" {
		query = query.Where("task_name = ?", value)
	}
	if value := strings.TrimSpace(req.PeriodicName); value != "" {
		query = query.Where("periodic_name = ?", value)
	}
	if value := strings.TrimSpace(req.Queue); value != "" {
		query = query.Where("queue = ?", value)
	}
	return query
}

// WindowSummary 通过覆盖索引聚合指定窗口，避免读取工作流 JSON 明细。
func (s *Store) WindowSummary(ctx context.Context, start time.Time, end time.Time) (types.TaskHistoryWindowSummary, error) {
	type aggregateRow struct {
		Total      int64   `gorm:"column:total"`       // 工作流总数
		Success    int64   `gorm:"column:success"`     // 成功工作流数
		Failed     int64   `gorm:"column:failed"`      // 失败工作流数
		AverageMS  float64 `gorm:"column:average_ms"`  // 平均耗时毫秒
		MaxMS      int64   `gorm:"column:max_ms"`      // 最大耗时毫秒
		TraceTotal int64   `gorm:"column:trace_total"` // 处理总量
		TraceError int64   `gorm:"column:trace_error"` // 处理错误量
	}
	var row aggregateRow
	err := s.readDB.WithContext(ctx).Model(&model.TaskWorkflowRun{}).
		Select("COUNT(*) AS total, COALESCE(SUM(status = ?), 0) AS success, COALESCE(SUM(status = ?), 0) AS failed, COALESCE(AVG(duration_ms), 0) AS average_ms, COALESCE(MAX(duration_ms), 0) AS max_ms, COALESCE(SUM(trace_total), 0) AS trace_total, COALESCE(SUM(trace_error), 0) AS trace_error", "success", "failed").
		Where("app_id = ? AND finished_at >= ? AND finished_at < ?", s.appID, start, end).
		Take(&row).Error
	if err != nil {
		return types.TaskHistoryWindowSummary{}, errors.Wrap(err, "聚合任务历史窗口失败")
	}
	resp := types.TaskHistoryWindowSummary{
		WindowStart: start.Format(time.RFC3339), WindowEnd: end.Format(time.RFC3339),
		Total: row.Total, Success: row.Success, Failed: row.Failed,
		AverageMS: int64(row.AverageMS), MaxMS: row.MaxMS, TraceTotal: row.TraceTotal, TraceError: row.TraceError,
	}
	if resp.Total > 0 {
		resp.SuccessRate = float64(resp.Success) * 100 / float64(resp.Total)
	}
	return resp, nil
}

// Cleanup 按索引选取小批主键删除过期历史，不做大事务和无界 DELETE。
func (s *Store) Cleanup(ctx context.Context, taskBefore time.Time, workflowBefore time.Time, failureBefore time.Time, limit int) (int64, error) {
	if s == nil || limit <= 0 || limit > 1000 {
		return 0, errors.Errorf("任务历史清理批次必须在 1-1000 之间")
	}
	var deleted int64
	for _, target := range []struct {
		model  any       // 待清理的 GORM Model
		before time.Time // 保留期截止时间
		column string    // 命中索引的时间列
	}{
		{model: &model.TaskRun{}, before: taskBefore, column: "finished_at"},
		{model: &model.TaskWorkflowRun{}, before: workflowBefore, column: "finished_at"},
		{model: &model.TaskFailure{}, before: failureBefore, column: "failed_at"},
	} {
		ids := make([]uint64, 0, limit)
		if err := s.writeDB.WithContext(ctx).Model(target.model).
			Where("app_id = ? AND "+target.column+" < ?", s.appID, target.before).
			Order(target.column+" ASC").Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return deleted, errors.Wrap(err, "查询过期任务历史失败")
		}
		if len(ids) == 0 {
			continue
		}
		result := s.writeDB.WithContext(ctx).Where("app_id = ? AND id IN ?", s.appID, ids).Delete(target.model)
		if result.Error != nil {
			return deleted, errors.Wrap(result.Error, "删除过期任务历史失败")
		}
		deleted += result.RowsAffected
	}
	return deleted, nil
}

// taskRunHistoryItem 把任务终态行转换为不含快照 JSON 的列表项。
func taskRunHistoryItem(row model.TaskRun) types.TaskRunHistoryItem {
	return types.TaskRunHistoryItem{
		ID: row.ID, TaskID: row.TaskID, TaskType: row.TaskType, TaskName: row.TaskName,
		Queue: row.Queue, Source: row.Source, PeriodicName: row.PeriodicName, Status: row.Status,
		WorkflowID: row.WorkflowID, WorkflowName: row.WorkflowName, WorkflowNode: row.WorkflowNode,
		ShardIndex: row.ShardIndex, ShardTotal: row.ShardTotal,
		Retried: row.Retried, MaxRetry: row.MaxRetry, TraceID: row.TraceID,
		TraceTotal: row.TraceTotal, TraceRead: row.TraceRead, TraceWrite: row.TraceWrite,
		TraceDelete: row.TraceDelete, TraceError: row.TraceError, DurationMS: row.DurationMS,
		ErrorMessage: row.ErrorMessage, StartedAt: row.StartedAt.Format(time.RFC3339),
		FinishedAt: row.FinishedAt.Format(time.RFC3339), PersistedAt: row.CreatedAt.Format(time.RFC3339),
		DataSource: "database",
	}
}

// applyTaskRunFilters 应用全部任务历史等值过滤，避免在线接口执行模糊扫描。
func applyTaskRunFilters(query *gorm.DB, req *types.ListTaskRunsReq) *gorm.DB {
	if value := strings.TrimSpace(req.TaskID); value != "" {
		query = query.Where("task_id = ?", value)
	}
	if value := strings.TrimSpace(req.WorkflowID); value != "" {
		query = query.Where("workflow_id = ?", value)
	}
	if value := strings.TrimSpace(req.TaskType); value != "" {
		query = query.Where("task_type = ?", value)
	}
	if value := strings.TrimSpace(req.TaskName); value != "" {
		query = query.Where("task_name = ?", value)
	}
	if value := strings.TrimSpace(req.PeriodicName); value != "" {
		query = query.Where("periodic_name = ?", value)
	}
	if value := strings.TrimSpace(req.Queue); value != "" {
		query = query.Where("queue = ?", value)
	}
	if value := strings.TrimSpace(req.Status); value != "" {
		query = query.Where("status = ?", value)
	}
	return query
}

// applyWorkflowFilters 应用工作流列表等值过滤，确保命中组合索引。
func applyWorkflowFilters(query *gorm.DB, req *types.ListTaskWorkflowsReq) *gorm.DB {
	if value := strings.TrimSpace(req.WorkflowID); value != "" {
		query = query.Where("workflow_id = ?", value)
	}
	if value := strings.TrimSpace(req.WorkflowName); value != "" {
		query = query.Where("workflow_name = ?", value)
	}
	if value := strings.TrimSpace(req.PeriodicName); value != "" {
		query = query.Where("periodic_name IN ?", []string{
			value,
			truncateText(legacyPeriodicPrefix+value, 128),
		})
	}
	if value := strings.TrimSpace(req.Status); value != "" {
		query = query.Where("status = ?", value)
	}
	if value := strings.TrimSpace(req.Source); value != "" {
		query = query.Where("source = ?", value)
	}
	if value := strings.TrimSpace(req.Queue); value != "" {
		query = query.Where("queue = ?", value)
	}
	return query
}

// workflowHistoryItem 转换工作流历史列表项。
func workflowHistoryItem(row model.TaskWorkflowRun) types.TaskWorkflowHistoryItem {
	return types.TaskWorkflowHistoryItem{
		ID: row.ID, WorkflowID: row.WorkflowID, WorkflowName: row.WorkflowName,
		PeriodicName: normalizePeriodicName(row.PeriodicName), Status: row.Status, Source: row.Source, Queue: row.Queue,
		NodeTotal: row.NodeTotal, TaskTotal: row.TaskTotal, Succeeded: row.Succeeded,
		Failed: row.Failed, Skipped: row.Skipped, TraceTotal: row.TraceTotal, TraceError: row.TraceError,
		DurationMS: row.DurationMS, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.WorkflowCreatedAt.Format(time.RFC3339), FinishedAt: row.FinishedAt.Format(time.RFC3339),
		PersistedAt: row.CreatedAt.Format(time.RFC3339), DataSource: "database", HistoryStatus: "persisted",
	}
}

// normalizePeriodicName 将旧版展示名称还原为周期任务原始名称。
func normalizePeriodicName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), legacyPeriodicPrefix)
}

// parseHistoryRange 解析调用层已校验的历史窗口。
func parseHistoryRange(startText string, endText string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(startText))
	if err != nil {
		return time.Time{}, time.Time{}, errors.Tag(err)
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(endText))
	if err != nil {
		return time.Time{}, time.Time{}, errors.Tag(err)
	}
	return start, end, nil
}

// parseHistoryTime 兼容 Redis 快照中的 RFC3339 与纳秒格式。
func parseHistoryTime(text string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil {
		return time.Time{}, errors.Tag(err)
	}
	return value, nil
}

// encodeHistoryCursor 生成不暴露 SQL 条件的稳定游标。
func encodeHistoryCursor(at time.Time, id uint64) string {
	value := strconv.FormatInt(at.UnixMilli(), 10) + ":" + strconv.FormatUint(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

// decodeHistoryCursor 解析 finished_at + id 游标。
func decodeHistoryCursor(cursor string) (time.Time, uint64, bool) {
	if strings.TrimSpace(cursor) == "" {
		return time.Time{}, 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return time.Time{}, 0, false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 {
		return time.Time{}, 0, false
	}
	milliseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, 0, false
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		return time.Time{}, 0, false
	}
	return time.UnixMilli(milliseconds), id, true
}

// truncateText 截断 DB varchar 字段，避免异常链路写入无界文本。
func truncateText(text string, limit int) string {
	items := []rune(strings.TrimSpace(text))
	if len(items) <= limit {
		return string(items)
	}
	return string(items[:limit])
}
