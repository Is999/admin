package taskhistory

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"admin/internal/model"
	taskqueue "admin/internal/task/queue"
	taskstats "admin/internal/task/stats"
	"admin/internal/types"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestWorkflowUpsertClauseUsesWorkflowIdentity 验证同一工作流的后续终态覆盖旧汇总并更新全部观测字段。
func TestWorkflowUpsertClauseUsesWorkflowIdentity(t *testing.T) {
	conflict := workflowUpsertClause()
	if len(conflict.Columns) != 2 || conflict.Columns[0].Name != "app_id" || conflict.Columns[1].Name != "workflow_id" {
		t.Fatalf("工作流 upsert 唯一键异常: %+v", conflict.Columns)
	}
	updated := make([]string, 0, len(conflict.DoUpdates))
	for _, assignment := range conflict.DoUpdates {
		updated = append(updated, assignment.Column.Name)
	}
	for _, field := range []string{"event_id", "status", "snapshot_json", "finished_at", "created_at"} {
		if !slices.Contains(updated, field) {
			t.Fatalf("工作流 upsert 缺少观测字段: field=%s updated=%v", field, updated)
		}
	}
}

// TestWorkflowRowKeepsBoundedAdaptiveDetail 验证存储层保留上游已经限流的自适应明细。
func TestWorkflowRowKeepsBoundedAdaptiveDetail(t *testing.T) {
	now := time.Now().UTC()
	store := &Store{appID: "app-1"}
	row, err := store.workflowRow(taskqueue.HistoryEvent{
		EventID: "workflow-event",
		Kind:    "workflow",
		Workflow: &types.TaskWorkflowStatusResp{
			WorkflowID: "workflow-1", WorkflowName: "cache.refresh", PeriodicName: "cache-refresh-minute",
			Status: "success", Source: "periodic", Queue: "maintenance", Targets: []string{"user:1", "user:2"},
			DetailLevel: "shard",
			CreatedAt:   now.Add(-time.Second).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano),
			ExecutionTrace: &taskstats.Snapshot{TotalCount: 10},
			Nodes: []types.TaskWorkflowNodeItem{{
				Name: "refresh", Expected: 2, Succeeded: 2,
				ExecutionTrace: &taskstats.Snapshot{TotalCount: 10, Details: []taskstats.Detail{{Action: "read", Name: "user:1", Count: 10}}},
				ShardTraces: []types.TaskWorkflowShardTraceItem{{
					ShardIndex: 0, ShardTotal: 2,
					ExecutionTrace: &taskstats.Snapshot{TotalCount: 5, Details: []taskstats.Detail{{Action: "read", Name: "user:1", Count: 5}}},
				}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("转换工作流历史失败: %v", err)
	}
	if row.TraceTotal != 10 || row.TaskTotal != 2 || row.Succeeded != 2 {
		t.Fatalf("工作流汇总字段错误: %+v", row)
	}
	var snapshot types.TaskWorkflowStatusResp
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		t.Fatalf("解析历史快照失败: %v", err)
	}
	if len(snapshot.Targets) != 0 {
		t.Fatalf("历史快照不应保存目标列表: %+v", snapshot)
	}
	if len(snapshot.Nodes[0].ExecutionTrace.Details) != 1 || len(snapshot.Nodes[0].ShardTraces) != 1 || len(snapshot.Nodes[0].ShardTraces[0].ExecutionTrace.Details) != 1 {
		t.Fatalf("上游有界明细应原样落库: %+v", snapshot)
	}
	if snapshot.DataSource != "database" || snapshot.DetailLevel != "shard" || snapshot.DetailTruncated || snapshot.HistoryStatus != "persisted" {
		t.Fatalf("历史快照来源元数据错误: %+v", snapshot)
	}
}

// TestFailureRowTruncatesErrorAndNeverContainsPayloadFields 验证失败历史只有固定排障摘要。
func TestFailureRowTruncatesErrorAndNeverContainsPayloadFields(t *testing.T) {
	store := &Store{appID: "app-1"}
	row, err := store.failureRow(taskqueue.HistoryEvent{
		EventID: "failure-event",
		Kind:    "failure",
		Failure: &types.TaskFailureItem{
			TaskID: "task-1", TaskType: strings.Repeat("t", 140), Queue: "maintenance",
			Retried: -1, MaxRetry: -1, ErrorMessage: strings.Repeat("错", 1200),
			FailedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		t.Fatalf("转换失败历史失败: %v", err)
	}
	if len([]rune(row.ErrorMessage)) != 1000 || len([]rune(row.TaskType)) != 128 || row.Retried != 0 || row.MaxRetry != 0 {
		t.Fatalf("失败摘要边界错误: %+v", row)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("序列化失败历史失败: %v", err)
	}
	text := strings.ToLower(string(raw))
	if strings.Contains(text, "payload") || strings.Contains(text, "result") {
		t.Fatalf("失败历史不得包含业务载荷或结果: %s", text)
	}
}

// TestTaskRowKeepsBoundedTraceWithoutBusinessPayload 验证独立任务历史只保存有界处理明细。
func TestTaskRowKeepsBoundedTraceWithoutBusinessPayload(t *testing.T) {
	now := time.Now().UTC()
	store := &Store{appID: "app-1"}
	row, err := store.taskRow(taskqueue.HistoryEvent{
		EventID: "task-event",
		Kind:    "task",
		Task: &types.TaskRunHistoryItem{
			TaskID: strings.Repeat("i", 140), TaskType: "file:cleanup", TaskName: "file-cleanup",
			Queue: "maintenance", Status: "success", TraceTotal: 10, TraceRead: 8, TraceDelete: 2,
			ExecutionTrace: &taskstats.Snapshot{TotalCount: 10, ReadCount: 8, DeleteCount: 2,
				Details: []taskstats.Detail{{Action: "delete", Name: "expired-files", Count: 2}}},
			StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		t.Fatalf("转换普通任务历史失败: %v", err)
	}
	if len([]rune(row.TaskID)) != 128 || row.TraceTotal != 10 || row.TraceDelete != 2 {
		t.Fatalf("普通任务历史摘要边界错误: %+v", row)
	}
	var snapshot types.TaskRunHistoryItem
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		t.Fatalf("解析普通任务历史快照失败: %v", err)
	}
	if snapshot.DataSource != "database" || snapshot.PersistedAt == "" || snapshot.ExecutionTrace == nil || len(snapshot.ExecutionTrace.Details) != 1 {
		t.Fatalf("普通任务历史快照内容异常: %+v", snapshot)
	}
	text := strings.ToLower(row.SnapshotJSON)
	if strings.Contains(text, "payload") || strings.Contains(text, "result") {
		t.Fatalf("普通任务历史不得包含业务载荷或结果: %s", text)
	}
}

// TestTaskRowKeepsWorkflowIdentityWithoutLargeDetail 验证工作流任务只保存身份和聚合计数。
func TestTaskRowKeepsWorkflowIdentityWithoutLargeDetail(t *testing.T) {
	now := time.Now().UTC()
	store := &Store{appID: "app-1"}
	row, err := store.taskRow(taskqueue.HistoryEvent{
		EventID: "workflow-task-event",
		Kind:    "task",
		Task: &types.TaskRunHistoryItem{
			TaskID: "workflow-1:refresh:0", TaskType: "cache:refresh", TaskName: "cache.refresh/refresh",
			Queue: "maintenance", Status: "success", WorkflowID: "workflow-1", WorkflowName: "cache.refresh",
			WorkflowNode: "refresh", ShardIndex: 0, ShardTotal: 16, TraceTotal: 100,
			StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		t.Fatalf("转换工作流任务历史失败: %v", err)
	}
	if row.WorkflowID != "workflow-1" || row.WorkflowName != "cache.refresh" || row.WorkflowNode != "refresh" || row.ShardIndex != 0 || row.ShardTotal != 16 || row.TraceTotal != 100 {
		t.Fatalf("工作流任务身份或聚合计数错误: %+v", row)
	}
	var snapshot types.TaskRunHistoryItem
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		t.Fatalf("解析工作流任务历史快照失败: %v", err)
	}
	if snapshot.ExecutionTrace != nil || snapshot.WorkflowID != row.WorkflowID || snapshot.ShardTotal != row.ShardTotal {
		t.Fatalf("工作流任务快照边界异常: %+v", snapshot)
	}
}

// TestListTaskRunsUsesCoveredColumnsAndCursor 验证列表不读取快照 JSON，并使用终态时间游标分页。
func TestListTaskRunsUsesCoveredColumnsAndCursor(t *testing.T) {
	db := newHistoryDryRunDB(t)
	end := time.Now().UTC()
	cursor := encodeHistoryCursor(end.Add(-time.Minute), 99)
	query := db.Model(&model.TaskRun{}).
		Where("app_id = ? AND finished_at >= ? AND finished_at < ?", "app-1", end.Add(-time.Hour), end).
		Where("(finished_at < ? OR (finished_at = ? AND id < ?))", end.Add(-time.Minute), end.Add(-time.Minute), uint64(99)).
		Select("id, task_id, task_type, task_name, queue, source, periodic_name, workflow_id, workflow_name, workflow_node, shard_index, shard_total, status, retried, max_retry, trace_id, trace_total, trace_read, trace_write, trace_delete, trace_error, duration_ms, error_message, started_at, finished_at, created_at").
		Order("finished_at DESC").Order("id DESC").Limit(21).Find(&[]model.TaskRun{})
	if query.Error != nil {
		t.Fatalf("生成普通任务历史列表 SQL 失败: %v", query.Error)
	}
	if strings.Contains(strings.ToLower(query.Statement.SQL.String()), "snapshot_json") {
		t.Fatalf("普通任务历史列表不应读取快照 JSON: %s", query.Statement.SQL.String())
	}
	if _, _, ok := decodeHistoryCursor(cursor); !ok {
		t.Fatalf("普通任务历史游标不可解码: %q", cursor)
	}
}

// TestTaskRunWorkflowFilterUsesCoveredEquality 验证工作流任务过滤使用组合索引等值条件。
func TestTaskRunWorkflowFilterUsesCoveredEquality(t *testing.T) {
	db := newHistoryDryRunDB(t)
	query := applyTaskRunFilters(db.Model(&model.TaskRun{}), &types.ListTaskRunsReq{WorkflowID: "workflow-1"}).
		Find(&[]model.TaskRun{})
	if query.Error != nil {
		t.Fatalf("生成工作流任务过滤 SQL 失败: %v", query.Error)
	}
	if !strings.Contains(query.Statement.SQL.String(), "workflow_id = ?") {
		t.Fatalf("工作流任务过滤语义异常: %s", query.Statement.SQL.String())
	}
}

// TestTaskRunNameFiltersUseCoveredEquality 验证全部任务按任务名称和周期任务名称使用组合索引等值查询。
func TestTaskRunNameFiltersUseCoveredEquality(t *testing.T) {
	db := newHistoryDryRunDB(t)
	query := applyTaskRunFilters(db.Model(&model.TaskRun{}), &types.ListTaskRunsReq{
		TaskName: "archive.run/archive.execute", PeriodicName: "archive-admin-log-hourly",
	}).
		Find(&[]model.TaskRun{})
	if query.Error != nil {
		t.Fatalf("生成普通任务名称过滤 SQL 失败: %v", query.Error)
	}
	sql := query.Statement.SQL.String()
	if !strings.Contains(sql, "task_name = ?") || !strings.Contains(sql, "periodic_name = ?") || strings.Contains(sql, " OR ") {
		t.Fatalf("普通任务名称过滤语义异常: %s", query.Statement.SQL.String())
	}
}

// TestFailureNameFiltersUseCoveredEquality 验证失败明细按任务名称和周期任务名称使用组合索引等值查询。
func TestFailureNameFiltersUseCoveredEquality(t *testing.T) {
	db := newHistoryDryRunDB(t)
	query := applyFailureFilters(db.Model(&model.TaskFailure{}), &types.ListTaskFailuresReq{
		TaskName: "archive.run/archive.execute", PeriodicName: "archive-admin-log-hourly",
	}).Find(&[]model.TaskFailure{})
	if query.Error != nil {
		t.Fatalf("生成失败明细名称过滤 SQL 失败: %v", query.Error)
	}
	for _, condition := range []string{"task_name = ?", "periodic_name = ?"} {
		if !strings.Contains(query.Statement.SQL.String(), condition) {
			t.Fatalf("失败明细名称过滤缺少 %q: %s", condition, query.Statement.SQL.String())
		}
	}
}

// TestWorkflowPeriodicFilterSupportsLegacyDisplayName 验证周期历史查询兼容旧版误写入的展示名称。
func TestWorkflowPeriodicFilterSupportsLegacyDisplayName(t *testing.T) {
	db := newHistoryDryRunDB(t)
	query := applyWorkflowFilters(db.Model(&model.TaskWorkflowRun{}), &types.ListTaskWorkflowsReq{
		PeriodicName: "archive-admin-log-hourly",
	}).Find(&[]model.TaskWorkflowRun{})
	if query.Error != nil {
		t.Fatalf("生成工作流历史过滤 SQL 失败: %v", query.Error)
	}
	if !strings.Contains(query.Statement.SQL.String(), "periodic_name IN (?,?)") {
		t.Fatalf("周期名称过滤未使用有界 IN 查询: %s", query.Statement.SQL.String())
	}
	wantVars := []string{"archive-admin-log-hourly", "周期任务触发:archive-admin-log-hourly"}
	for _, value := range wantVars {
		if !slices.Contains(query.Statement.Vars, any(value)) {
			t.Fatalf("周期名称过滤缺少参数 %q: %+v", value, query.Statement.Vars)
		}
	}
	if got := normalizePeriodicName(wantVars[1]); got != wantVars[0] {
		t.Fatalf("旧版周期展示名称归一化=%q，期望=%q", got, wantVars[0])
	}
}

// newHistoryDryRunDB 创建任务历史 SQL 断言使用的 MySQL DryRun 连接。
func newHistoryDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("创建任务历史 DryRun 数据库失败: %v", err)
	}
	return db
}
