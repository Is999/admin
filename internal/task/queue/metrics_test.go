package taskqueue

import "testing"

// TestNormalizeTaskMetricGuardedLabel 验证对应场景符合预期。
func TestNormalizeTaskMetricGuardedLabel(t *testing.T) {
	labels := map[string]struct{}{"registered": {}}
	if got := normalizeTaskMetricGuardedLabel("", 1, labels); got != taskMetricLabelUnknown {
		t.Fatalf("空 label 应归并为 unknown，got=%s", got)
	}
	if got := normalizeTaskMetricGuardedLabel("registered", 1, labels); got != "registered" {
		t.Fatalf("已登记 label 应保留，got=%s", got)
	}
	if got := normalizeTaskMetricGuardedLabel("overflow", 1, labels); got != taskMetricLabelOther {
		t.Fatalf("超出上限的 label 应归并为 other，got=%s", got)
	}
}
