package validation

import (
	"context"
	"testing"
	"time"
)

// fakeSource 是校验测试使用的只读数据源。
type fakeSource struct {
	name string          // 数据源名称
	rows []SourceSummary // 返回的摘要行
}

// Name 返回数据源名称。
func (s fakeSource) Name() string {
	return s.name
}

// Load 返回预设摘要行。
func (s fakeSource) Load(ctx context.Context, uids []int64, start, end time.Time) ([]SourceSummary, error) {
	return s.rows, nil
}

// TestValidateUIDsBuildsDiffs 验证多侧字段差异会进入报告。
func TestValidateUIDsBuildsDiffs(t *testing.T) {
	harness := NewHarness(
		fakeSource{name: "v1", rows: []SourceSummary{{Source: "v1", UID: 1001, Fields: map[string]any{"score": int64(2)}}}},
		fakeSource{name: "v2", rows: []SourceSummary{{Source: "v2", UID: 1001, Fields: map[string]any{"score": int64(3)}}}},
	)
	report, err := harness.ValidateUIDs(context.Background(), "wf", []int64{1001}, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if len(report.Diffs) != 1 {
		t.Fatalf("期望 1 条差异，实际=%d report=%+v", len(report.Diffs), report)
	}
}
