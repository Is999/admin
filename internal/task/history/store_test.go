package taskhistory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	taskqueue "admin/internal/task/queue"
	taskstats "admin/internal/task/stats"
	"admin/internal/types"
)

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
