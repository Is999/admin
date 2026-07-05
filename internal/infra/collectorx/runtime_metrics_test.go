package collectorx

import (
	"context"
	"testing"
	"time"

	"admin/internal/config"
)

// TestRuntimeMetricsSnapshotAggregatesWindows 确保运行态指标按 1/5/15 分钟窗口聚合。
func TestRuntimeMetricsSnapshotAggregatesWindows(t *testing.T) {
	stats := newCollectorRuntimeStats()
	now := time.Date(2026, 7, 6, 10, 0, 0, 0, time.Local)
	stats.add(now, "biz-hot", collectorRuntimeCounters{
		published:     3,
		consumed:      3,
		processed:     5,
		succeeded:     4,
		failed:        1,
		duplicate:     2,
		batches:       1,
		failedBatches: 1,
		batchSizeSum:  5,
		costMsSum:     12,
		maxCostMs:     12,
	})
	stats.add(now.Add(-2*time.Minute), "biz-warm", collectorRuntimeCounters{
		processed:    7,
		succeeded:    7,
		batches:      1,
		batchSizeSum: 7,
		costMsSum:    21,
		maxCostMs:    21,
	})
	stats.add(now.Add(-20*time.Minute), "biz-expired", collectorRuntimeCounters{
		processed:    11,
		succeeded:    11,
		batches:      1,
		batchSizeSum: 11,
		costMsSum:    30,
		maxCostMs:    30,
	})

	snapshot := stats.snapshot(now)
	if snapshot.Totals.Processed != 23 {
		t.Fatalf("累计处理量 = %d, want 23", snapshot.Totals.Processed)
	}
	if snapshot.Totals.Duplicate != 2 {
		t.Fatalf("累计去重量 = %d, want 2", snapshot.Totals.Duplicate)
	}
	if snapshot.Recent1m.Processed != 5 || snapshot.Recent5m.Processed != 12 || snapshot.Recent15m.Processed != 12 {
		t.Fatalf("窗口处理量不符合预期: 1m=%d 5m=%d 15m=%d", snapshot.Recent1m.Processed, snapshot.Recent5m.Processed, snapshot.Recent15m.Processed)
	}
	if snapshot.Recent1m.AvgBatchSize != 5 || snapshot.Recent5m.AvgBatchSize != 6 {
		t.Fatalf("平均批次大小不符合预期: 1m=%.2f 5m=%.2f", snapshot.Recent1m.AvgBatchSize, snapshot.Recent5m.AvgBatchSize)
	}
	if len(snapshot.BizTypeTop15m) != 2 || snapshot.BizTypeTop15m[0].BizType != "biz-warm" {
		t.Fatalf("bizType 热点排行不符合预期: %+v", snapshot.BizTypeTop15m)
	}
}

// TestProcessBatchRecordsRuntimeMetrics 确保真实批处理结果会进入运行态指标。
func TestProcessBatchRecordsRuntimeMetrics(t *testing.T) {
	manager := &Manager{
		cfg:          configEnabledForRuntimeMetricTest(),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	if err := manager.RegisterProcessorFunc("biz", func(context.Context, []Event) ([]ProcessResult, error) {
		return []ProcessResult{
			{EventID: "e1", Success: true},
			{EventID: "e2", Success: false, Error: "write failed"},
		}, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "biz"},
		{EventID: "e2", BizType: "biz"},
	})
	if len(success) != 1 || len(failed) != 1 {
		t.Fatalf("批处理结果不符合预期 success=%v failed=%v", success, failed)
	}
	snapshot := manager.RuntimeMetricsSnapshot()
	if snapshot.Totals.Processed != 2 || snapshot.Totals.Succeeded != 1 || snapshot.Totals.Failed != 1 {
		t.Fatalf("运行态累计指标不符合预期: %+v", snapshot.Totals)
	}
	if len(snapshot.BizTypeTop15m) != 1 || snapshot.BizTypeTop15m[0].BizType != "biz" || snapshot.BizTypeTop15m[0].FailedBatches != 1 {
		t.Fatalf("运行态 bizType 指标不符合预期: %+v", snapshot.BizTypeTop15m)
	}
}

// configEnabledForRuntimeMetricTest 返回运行态指标测试所需的最小配置。
func configEnabledForRuntimeMetricTest() config.CollectorConfig {
	return config.CollectorConfig{Enabled: true}
}
