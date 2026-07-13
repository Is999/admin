package taskreport

import (
	"testing"
	"time"

	"admin/internal/config"

	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
)

// TestToLarkReportUsesModeEnvironment 确保日报通知环境来自顶层 Mode。
func TestToLarkReportUsesModeEnvironment(t *testing.T) {
	got := ToLarkReport(config.Config{
		RestConf: rest.RestConf{ServiceConf: service.ServiceConf{
			Name: "admin",
			Mode: "pro",
		}},
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "cron_admin",
			Environment: "dev",
		},
	}, Report{
		WindowStart: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		TimeBuckets: []TimeBucketSummary{{
			StartAt:         "2026-07-02T09:00:00Z",
			EndAt:           "2026-07-02T10:00:00Z",
			TaskExecutions:  12,
			TraceTotalCount: 34,
		}},
	})
	if got.ServiceName != "cron_admin" || got.Environment != "pro" || got.AppID != "site-1" {
		t.Fatalf("Lark 日报基础字段不符合预期: %+v", got)
	}
	if got.ReportID == "" || got.ReportID != taskDailyReportID("site-1", got.WindowStart, got.WindowEnd) {
		t.Fatalf("Lark 日报稳定标识异常: %q", got.ReportID)
	}
	if len(got.TimeBuckets) != 1 || got.TimeBuckets[0].TaskExecutions != 12 || got.TimeBuckets[0].TraceTotalCount != 34 {
		t.Fatalf("Lark 日报时间分布未透传: %+v", got.TimeBuckets)
	}
}

// TestTaskDailyReportIDIsolatesSiteAndWindow 确保同站点同窗口稳定，不同站点或窗口互不冲突。
func TestTaskDailyReportIDIsolatesSiteAndWindow(t *testing.T) {
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	end := start.Add(24 * time.Hour)
	first := taskDailyReportID("site-1", start, end)
	if first == "" || first != taskDailyReportID("site-1", start.UTC(), end.UTC()) {
		t.Fatalf("同一时刻的日报 ID 应稳定: %q", first)
	}
	if first == taskDailyReportID("site-2", start, end) || first == taskDailyReportID("site-1", start.Add(time.Hour), end) {
		t.Fatal("不同站点或窗口不应共用日报 ID")
	}
	if got := taskDailyReportID("", start, end); got != "" {
		t.Fatalf("空站点不应生成日报 ID: %q", got)
	}
}
