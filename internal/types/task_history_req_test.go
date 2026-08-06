package types

import (
	"strings"
	"testing"
	"time"
)

// TestTaskHistoryRequestsApplyBoundedDefaults 验证历史查询默认使用 24 小时窗口和受控页大小。
func TestTaskHistoryRequestsApplyBoundedDefaults(t *testing.T) {
	workflowReq := &ListTaskWorkflowsReq{PageSize: 1000}
	if err := workflowReq.Validate(); err != nil {
		t.Fatalf("工作流历史默认查询校验失败: %v", err)
	}
	if workflowReq.PageSize != 100 || workflowReq.StartTime == "" || workflowReq.EndTime == "" {
		t.Fatalf("工作流历史默认边界错误: %+v", workflowReq)
	}
	taskReq := &ListTaskRunsReq{PageSize: 1000}
	if err := taskReq.Validate(); err != nil {
		t.Fatalf("普通任务历史默认查询校验失败: %v", err)
	}
	if taskReq.PageSize != 100 || taskReq.StartTime == "" || taskReq.EndTime == "" {
		t.Fatalf("普通任务历史默认边界错误: %+v", taskReq)
	}
	failureReq := &ListTaskFailuresReq{}
	if err := failureReq.Validate(); err != nil {
		t.Fatalf("失败历史默认查询校验失败: %v", err)
	}
	if failureReq.PageSize != 20 || failureReq.StartTime == "" || failureReq.EndTime == "" {
		t.Fatalf("失败历史默认边界错误: %+v", failureReq)
	}
}

// TestTaskHistoryRequestDefaultsStartFromExplicitEnd 验证历史回看窗口跟随调用方指定的结束时间。
func TestTaskHistoryRequestDefaultsStartFromExplicitEnd(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	req := &ListTaskWorkflowsReq{EndTime: end.Format(time.RFC3339)}
	if err := req.Validate(); err != nil {
		t.Fatalf("指定结束时间的历史查询校验失败: %v", err)
	}
	start, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		t.Fatalf("解析默认开始时间失败: %v", err)
	}
	if !start.Equal(end.Add(-24 * time.Hour)) {
		t.Fatalf("默认开始时间=%s，期望=%s", start, end.Add(-24*time.Hour))
	}
}

// TestTaskHistoryRequestsRejectUnboundedRange 验证在线接口不能扫描超过 31 天的历史。
func TestTaskHistoryRequestsRejectUnboundedRange(t *testing.T) {
	end := time.Now().UTC()
	start := end.Add(-31*24*time.Hour - time.Second)
	for _, request := range []interface{ Validate() error }{
		&ListTaskRunsReq{StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339)},
		&ListTaskWorkflowsReq{StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339)},
		&ListTaskFailuresReq{StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339)},
	} {
		if err := request.Validate(); err == nil {
			t.Fatal("超过 31 天的任务历史查询应被拒绝")
		}
	}
}

// TestTaskHistoryRequestsRejectOversizedFilters 验证历史查询不接受无界过滤值和游标。
func TestTaskHistoryRequestsRejectOversizedFilters(t *testing.T) {
	for _, request := range []interface{ Validate() error }{
		&ListTaskRunsReq{Queue: strings.Repeat("q", 129)},
		&ListTaskRunsReq{WorkflowID: strings.Repeat("w", 129)},
		&ListTaskRunsReq{Cursor: strings.Repeat("c", 257)},
		&ListTaskRunsReq{Status: "completed"},
		&ListTaskWorkflowsReq{Queue: strings.Repeat("q", 129)},
		&ListTaskWorkflowsReq{Cursor: strings.Repeat("c", 257)},
		&ListTaskFailuresReq{Queue: strings.Repeat("q", 129)},
		&ListTaskFailuresReq{TaskName: strings.Repeat("n", 129)},
		&ListTaskFailuresReq{PeriodicName: strings.Repeat("p", 129)},
		&ListTaskFailuresReq{Cursor: strings.Repeat("c", 257)},
	} {
		if err := request.Validate(); err == nil {
			t.Fatal("超长任务历史查询参数应被拒绝")
		}
	}
}
