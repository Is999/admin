package task

import (
	"strings"
	"testing"
	"time"

	"admin/internal/jobs/taskreport"
)

// TestResolveReportWindowUsesScheduledAt 验证默认日报窗口锚定周期触发时间，而非 worker 实际执行时间。
func TestResolveReportWindowUsesScheduledAt(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	start, gotEnd, err := resolveReportWindow(taskreport.TaskPayload{}, end.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("解析日报窗口失败: %v", err)
	}
	if !gotEnd.Equal(end) || !start.Equal(end.Add(-24*time.Hour)) {
		t.Fatalf("日报窗口=(%s,%s)，期望=(%s,%s)", start, gotEnd, end.Add(-24*time.Hour), end)
	}
}

// TestResolveReportWindowKeepsManualOverride 验证手动补跑窗口仍优先于周期触发时间。
func TestResolveReportWindowKeepsManualOverride(t *testing.T) {
	manualStart := "2026-07-15T10:00:00+08:00"
	manualEnd := "2026-07-16T10:00:00+08:00"
	start, end, err := resolveReportWindow(taskreport.TaskPayload{
		WindowStart: manualStart,
		WindowEnd:   manualEnd,
	}, "2026-07-18T10:00:00+08:00")
	if err != nil {
		t.Fatalf("解析手动日报窗口失败: %v", err)
	}
	if start.Format(time.RFC3339) != manualStart || end.Format(time.RFC3339) != manualEnd {
		t.Fatalf("手动日报窗口被覆盖: start=%s end=%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
}

// TestResolveReportWindowRejectsPartialManualOverride 验证手动窗口必须同时提供开始和结束时间。
func TestResolveReportWindowRejectsPartialManualOverride(t *testing.T) {
	tests := []struct {
		name    string                 // 测试场景
		payload taskreport.TaskPayload // 手动日报窗口载荷
	}{
		{
			name: "only_start",
			payload: taskreport.TaskPayload{
				WindowStart: "2026-07-17T10:00:00+08:00",
			},
		},
		{
			name: "only_end",
			payload: taskreport.TaskPayload{
				WindowEnd: "2026-07-18T10:00:00+08:00",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := resolveReportWindow(tt.payload, ""); err == nil || !strings.Contains(err.Error(), "同时提供") {
				t.Fatalf("单边手动日报窗口应被成对校验拒绝，实际 err=%v", err)
			}
		})
	}
}

// TestResolveReportWindowRejectsInvalidScheduledAt 验证损坏的内部触发时间不会静默退回漂移窗口。
func TestResolveReportWindowRejectsInvalidScheduledAt(t *testing.T) {
	if _, _, err := resolveReportWindow(taskreport.TaskPayload{}, "invalid"); err == nil {
		t.Fatal("期望非法计划触发时间返回错误")
	}
}

// TestResolveReportWindowRejectsSubsecondOverride 验证手工窗口不能使用 Asynq 终态时间无法区分的亚秒边界。
func TestResolveReportWindowRejectsSubsecondOverride(t *testing.T) {
	_, _, err := resolveReportWindow(taskreport.TaskPayload{
		WindowStart: "2026-07-17T10:00:00.001+08:00",
		WindowEnd:   "2026-07-18T10:00:00.001+08:00",
	}, "")
	if err == nil {
		t.Fatal("亚秒日报窗口应被拒绝")
	}
}

// TestResolveReportWindowRejectsFutureManualWindow 验证手工补跑不能生成看似正常的未来空日报。
func TestResolveReportWindowRejectsFutureManualWindow(t *testing.T) {
	end := time.Now().Add(time.Minute).UTC()
	start := end.Add(-24 * time.Hour)
	_, _, err := resolveReportWindow(taskreport.TaskPayload{
		WindowStart: start.Format(time.RFC3339),
		WindowEnd:   end.Format(time.RFC3339),
	}, "")
	if err == nil {
		t.Fatal("未来日报窗口应被拒绝")
	}
}

// TestDailySummaryWorkflowUsesScheduledUniqueKey 验证日报按计划时刻防重，不会因 worker 延迟吞掉下一周期。
func TestDailySummaryWorkflowUsesScheduledUniqueKey(t *testing.T) {
	definition := dailySummaryWorkflow()
	if definition == nil || !definition.PeriodicUniqueBySchedule {
		t.Fatal("日报工作流必须按计划触发时刻隔离幂等键")
	}
}
