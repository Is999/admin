package taskreport

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"admin/internal/types"
)

// reportFakeManager 提供日报所需的有界队列快照。
type reportFakeManager struct {
	resp    *types.TaskQueueListResp // resp 是队列快照
	limited bool                     // limited 表示队列数量被截断
	err     error                    // err 是队列读取故障
}

// ListReportQueues 返回预置队列快照。
func (m *reportFakeManager) ListReportQueues(context.Context, int) (*types.TaskQueueListResp, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	return m.resp, m.limited, m.err
}

// reportFakeHistory 记录 DB 日报构建入参并返回预置结果。
type reportFakeHistory struct {
	req    ReportRequest  // req 是历史查询窗口
	queues []QueueSummary // queues 是实时队列快照
	report Report         // report 是预置日报
	err    error          // err 是历史聚合故障
}

// BuildTaskReport 实现 DB 历史日报构建测试桩。
func (h *reportFakeHistory) BuildTaskReport(_ context.Context, req ReportRequest, queues []QueueSummary) (Report, error) {
	h.req = req
	h.queues = append([]QueueSummary(nil), queues...)
	return h.report, h.err
}

// TestWindow 验证日报窗口按指定基准时间回看 24 小时并对齐整秒。
func TestWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 1, 17, 0, 0, 987654321, loc)
	start, end := Window(now)
	wantEnd := time.Date(2026, 7, 1, 17, 0, 0, 0, loc)
	if !end.Equal(wantEnd) || !start.Equal(wantEnd.Add(-24*time.Hour)) {
		t.Fatalf("window=(%s,%s)，期望=(%s,%s)", start, end, wantEnd.Add(-24*time.Hour), wantEnd)
	}
}

// TestBuildRejectsInvalidWindow 验证日报入口拒绝亚秒、空缺和倒置窗口。
func TestBuildRejectsInvalidWindow(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	service := NewService(&reportFakeManager{}, &reportFakeHistory{})
	tests := []struct {
		name string        // 测试场景
		req  ReportRequest // 非法请求
		want string        // 期望错误片段
	}{
		{name: "subsecond", req: ReportRequest{WindowStart: end.Add(-time.Hour), WindowEnd: end.Add(time.Nanosecond)}, want: "整秒对齐"},
		{name: "missing_end", req: ReportRequest{WindowStart: end.Add(-time.Hour)}, want: "同时提供"},
		{name: "reversed", req: ReportRequest{WindowStart: end, WindowEnd: end.Add(-time.Second)}, want: "统计窗口非法"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Build(context.Background(), test.req)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error=%v，期望包含 %q", err, test.want)
			}
		})
	}
}

// TestBuildUsesDatabaseHistory 验证日报只委托 DB 汇总，并合并 Redis 当前队列积压。
func TestBuildUsesDatabaseHistory(t *testing.T) {
	end := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	history := &reportFakeHistory{report: Report{WorkflowTotal: 3}}
	manager := &reportFakeManager{
		limited: true,
		resp: &types.TaskQueueListResp{Queues: []types.TaskQueueItem{{
			Name: " maintenance ", Pending: 2, Active: 1, Completed: 9,
		}}},
	}
	report, err := NewService(manager, history).Build(context.Background(), ReportRequest{
		WindowStart: end.Add(-24 * time.Hour), WindowEnd: end, GeneratedAt: end,
	})
	if err != nil {
		t.Fatalf("构建 DB 日报失败: %v", err)
	}
	if report.WorkflowTotal != 3 || len(report.IntegrityWarnings) != 1 {
		t.Fatalf("DB 日报或截断警告异常: %+v", report)
	}
	if len(history.queues) != 1 || history.queues[0].Name != "maintenance" || history.queues[0].Pending != 2 || history.queues[0].Completed != 9 {
		t.Fatalf("实时队列摘要透传异常: %+v", history.queues)
	}
	if !history.req.WindowStart.Equal(end.Add(-24*time.Hour)) || !history.req.WindowEnd.Equal(end) {
		t.Fatalf("DB 日报窗口异常: %+v", history.req)
	}
}

// TestBuildKeepsDefaultQueueObservability 验证空队列快照仍保留三个默认队列指标。
func TestBuildKeepsDefaultQueueObservability(t *testing.T) {
	history := &reportFakeHistory{}
	manager := &reportFakeManager{resp: &types.TaskQueueListResp{}}
	if _, err := NewService(manager, history).Build(context.Background(), ReportRequest{}); err != nil {
		t.Fatalf("空队列快照构建日报失败: %v", err)
	}
	if len(history.queues) != 3 || history.queues[0].Name != "critical" || history.queues[1].Name != "default" || history.queues[2].Name != "maintenance" {
		t.Fatalf("默认队列摘要异常: %+v", history.queues)
	}
}

// TestBuildFailsClosedWithoutHistory 验证缺少 DB 历史时不会回退扫描 Redis。
func TestBuildFailsClosedWithoutHistory(t *testing.T) {
	_, err := NewService(&reportFakeManager{}, nil).Build(context.Background(), ReportRequest{})
	if err == nil || !strings.Contains(err.Error(), "历史落库未启用") {
		t.Fatalf("缺少 DB 历史时 error=%v", err)
	}
}

// TestBuildPropagatesSourceErrors 验证队列或数据库故障会显式返回，不生成误导日报。
func TestBuildPropagatesSourceErrors(t *testing.T) {
	queueErr := stderrors.New("queue unavailable")
	if _, err := NewService(&reportFakeManager{err: queueErr}, &reportFakeHistory{}).Build(context.Background(), ReportRequest{}); !stderrors.Is(err, queueErr) {
		t.Fatalf("队列故障未透传: %v", err)
	}
	historyErr := stderrors.New("database unavailable")
	manager := &reportFakeManager{resp: &types.TaskQueueListResp{}}
	if _, err := NewService(manager, &reportFakeHistory{err: historyErr}).Build(context.Background(), ReportRequest{}); !stderrors.Is(err, historyErr) {
		t.Fatalf("数据库故障未透传: %v", err)
	}
}
