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
	if len(got.TimeBuckets) != 1 || got.TimeBuckets[0].TaskExecutions != 12 || got.TimeBuckets[0].TraceTotalCount != 34 {
		t.Fatalf("Lark 日报时间分布未透传: %+v", got.TimeBuckets)
	}
}
