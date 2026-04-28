package taskstats

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTrackerAggregatesCountsAndDetails(t *testing.T) {
	ctx, _ := WithTracker(context.Background(), "demo.task")
	RecordRead(ctx, "source_rows", 3)
	RecordRead(ctx, "source_rows", 2)
	RecordInsert(ctx, "target_rows", 4)
	Observe(ctx, ActionUpdate, "checkpoint", 1, 500*time.Microsecond)
	Record(ctx, "custom_action", "custom_stage", 0)

	snapshot := SnapshotFromContext(ctx)
	if snapshot == nil {
		t.Fatal("期望生成执行统计快照，实际为空")
	}
	if snapshot.Name != "demo.task" {
		t.Fatalf("统计器名称错误，got=%q", snapshot.Name)
	}
	if snapshot.TotalCount != 10 || snapshot.ReadCount != 5 || snapshot.InsertCount != 4 || snapshot.UpdateCount != 1 {
		t.Fatalf("聚合计数错误: %+v", snapshot)
	}
	if snapshot.DurationMS <= 0 || snapshot.StartedAt == "" || snapshot.FinishedAt == "" {
		t.Fatalf("期望快照包含耗时和时间戳: %+v", snapshot)
	}
	if len(snapshot.Details) != 4 {
		t.Fatalf("期望 4 条明细，实际=%d details=%+v", len(snapshot.Details), snapshot.Details)
	}
	if snapshot.Details[0].Action != ActionCustom || snapshot.Details[0].Name != "custom_stage" {
		t.Fatalf("明细应按动作和名称稳定排序，实际=%+v", snapshot.Details)
	}
	foundSource := false
	for _, detail := range snapshot.Details {
		if detail.Action == ActionRead && detail.Name == "source_rows" {
			foundSource = true
			if detail.Count != 5 || detail.Times != 2 {
				t.Fatalf("读取明细累计错误: %+v", detail)
			}
		}
		if detail.Action == ActionUpdate && detail.Name == "checkpoint" && detail.ElapsedMS != 1 {
			t.Fatalf("亚毫秒耗时应按 1ms 记录，实际=%+v", detail)
		}
	}
	if !foundSource {
		t.Fatalf("缺少读取明细: %+v", snapshot.Details)
	}
}

func TestJoinDetailName(t *testing.T) {
	if got := JoinDetailName(" user_tag ", ".clean_profile.", DetailPartRows); got != "user_tag.clean_profile.rows" {
		t.Fatalf("明细名拼接结果错误: %s", got)
	}
	if got := JoinDetailName("", " . "); got != DetailNameDefault {
		t.Fatalf("空明细名应回退默认值: %s", got)
	}
}

func TestTrackerIsConcurrentSafe(t *testing.T) {
	ctx, _ := WithTracker(context.Background(), "concurrent.task")
	wg := sync.WaitGroup{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				RecordDelete(ctx, "hot_rows", 1)
			}
		}()
	}
	wg.Wait()

	snapshot := SnapshotFromContext(ctx)
	if snapshot == nil || snapshot.DeleteCount != 800 || snapshot.TotalCount != 800 {
		t.Fatalf("并发累计计数错误: %+v", snapshot)
	}
	if len(snapshot.Details) != 1 || snapshot.Details[0].Times != 800 {
		t.Fatalf("并发明细累计次数错误: %+v", snapshot)
	}
}

func TestMergeSnapshotsAggregatesWorkflowCounts(t *testing.T) {
	first := &Snapshot{
		Name:        "node/shard-0",
		StartedAt:   "2026-06-05T10:00:00Z",
		FinishedAt:  "2026-06-05T10:00:02Z",
		DurationMS:  2000,
		TotalCount:  5,
		ReadCount:   3,
		UpdateCount: 2,
		Details: []Detail{
			{Action: ActionRead, Name: "user_tag.clean_profile.scanned", Count: 3, Times: 1, ElapsedMS: 10},
			{Action: ActionUpdate, Name: "user_tag.clean_profile.updated", Count: 2, Times: 1, ElapsedMS: 20},
		},
	}
	second := &Snapshot{
		Name:        "node/shard-1",
		StartedAt:   "2026-06-05T10:00:01Z",
		FinishedAt:  "2026-06-05T10:00:04Z",
		DurationMS:  3000,
		TotalCount:  8,
		ReadCount:   4,
		UpdateCount: 4,
		Details: []Detail{
			{Action: ActionRead, Name: "user_tag.clean_profile.scanned", Count: 4, Times: 2, ElapsedMS: 30},
			{Action: ActionUpdate, Name: "user_tag.clean_profile.updated", Count: 4, Times: 1, ElapsedMS: 40},
		},
	}

	merged := MergeSnapshots("workflow/user_tag.full", first, nil, second)
	if merged == nil {
		t.Fatal("期望合并出工作流统计摘要，实际为空")
	}
	if merged.Name != "workflow/user_tag.full" || merged.TotalCount != 13 || merged.ReadCount != 7 || merged.UpdateCount != 6 {
		t.Fatalf("合并计数错误: %+v", merged)
	}
	if merged.StartedAt != "2026-06-05T10:00:00Z" || merged.FinishedAt != "2026-06-05T10:00:04Z" || merged.DurationMS != 4000 {
		t.Fatalf("合并时间窗口错误: %+v", merged)
	}
	if len(merged.Details) != 2 {
		t.Fatalf("期望同名明细被聚合，实际 details=%+v", merged.Details)
	}
	for _, detail := range merged.Details {
		switch detail.Action {
		case ActionRead:
			if detail.Count != 7 || detail.Times != 3 || detail.ElapsedMS != 40 {
				t.Fatalf("读取明细合并错误: %+v", detail)
			}
		case ActionUpdate:
			if detail.Count != 6 || detail.Times != 2 || detail.ElapsedMS != 60 {
				t.Fatalf("更新明细合并错误: %+v", detail)
			}
		}
	}
}

func TestSnapshotFromContextReturnsNilWithoutBusinessDetails(t *testing.T) {
	if snapshot := SnapshotFromContext(context.Background()); snapshot != nil {
		t.Fatalf("未挂统计器时应返回 nil，实际=%+v", snapshot)
	}
	ctx, _ := WithTracker(context.Background(), "empty.task")
	if snapshot := SnapshotFromContext(ctx); snapshot != nil {
		t.Fatalf("无明细时应返回 nil，实际=%+v", snapshot)
	}
}

func TestProgressCalculatesPercentAndRemaining(t *testing.T) {
	progress := NewProgress(ProgressUnitShard, ProgressStatusRunning, 10, 3, 1, 2)
	if progress == nil {
		t.Fatal("期望生成进度快照")
	}
	if progress.Finished != 6 || progress.Remaining != 4 || progress.Running != 4 || progress.Pending != 0 {
		t.Fatalf("进度计数错误: %+v", progress)
	}
	if progress.Percent != 60 || progress.SuccessPercent != 30 {
		t.Fatalf("进度百分比错误: %+v", progress)
	}
}

func TestMergeProgressAggregatesItems(t *testing.T) {
	first := NewProgress(ProgressUnitShard, ProgressStatusSuccess, 2, 2, 0, 0)
	second := NewProgress(ProgressUnitShard, ProgressStatusRunning, 3, 1, 0, 0)
	merged := MergeProgress(ProgressUnitNodeInstance, ProgressStatusRunning, first, second)
	if merged == nil {
		t.Fatal("期望生成聚合进度")
	}
	if merged.Total != 5 || merged.Succeeded != 3 || merged.Finished != 3 || merged.Running != 2 {
		t.Fatalf("聚合进度计数错误: %+v", merged)
	}
	if merged.Percent != 60 || merged.SuccessPercent != 60 {
		t.Fatalf("聚合进度百分比错误: %+v", merged)
	}
}
