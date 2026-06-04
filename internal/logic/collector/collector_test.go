package collector

import (
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestBuildCollectorWindowSuccessQueryUsesDoneAtFallback 验证近期成功统计兼容历史 finished_at 空值。
func TestBuildCollectorWindowSuccessQueryUsesDoneAtFallback(t *testing.T) {
	db := newCollectorDryRunDB(t)
	since := time.Date(2026, 6, 25, 11, 0, 0, 0, time.Local)
	row := struct {
		SuccessCount int64   `gorm:"column:success_count"` // 成功任务数
		AvgCostMs    float64 `gorm:"column:avg_cost_ms"`   // 平均耗时毫秒
		MaxCostMs    float64 `gorm:"column:max_cost_ms"`   // 最大耗时毫秒
	}{}

	tx := buildCollectorWindowSuccessQuery(db, since).Find(&row)
	if tx.Error != nil {
		t.Fatalf("buildCollectorWindowSuccessQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"COUNT(*) AS success_count",
		"(finished_at >= ? OR (finished_at IS NULL AND updated_at >= ?))",
		"TIMESTAMPDIFF(MICROSECOND, started_at, finished_at)",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if strings.Contains(sqlText, "started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= ?") {
		t.Fatalf("unexpected strict finished_at filter in SQL: %s", sqlText)
	}
}

// TestBuildCollectorBizTypeOverviewQueryKeepsUnknown 验证 biz_type 热点不再丢弃空维度。
func TestBuildCollectorBizTypeOverviewQueryKeepsUnknown(t *testing.T) {
	db := newCollectorDryRunDB(t)
	now := time.Date(2026, 6, 25, 11, 15, 0, 0, time.Local)
	since := now.Add(-15 * time.Minute)
	rows := make([]struct {
		BizType string `gorm:"column:biz_type"` // 业务类型
	}, 0)

	tx := buildCollectorBizTypeOverviewQuery(db, now, since).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildCollectorBizTypeOverviewQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"COALESCE(NULLIF(biz_type, ''), 'unknown') AS biz_type",
		"GROUP BY COALESCE(NULLIF(biz_type, ''), 'unknown')",
		"(finished_at >= ? OR (finished_at IS NULL AND updated_at >= ?))",
		"ORDER BY ready_count DESC",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	for _, bad := range []string{"biz_type <> ''", "NOW()"} {
		if strings.Contains(sqlText, bad) {
			t.Fatalf("unexpected %q in SQL: %s", bad, sqlText)
		}
	}
}

// TestBuildCollectorTransportOverviewQueryKeepsUnknown 验证 transport 分布不再丢弃空来源。
func TestBuildCollectorTransportOverviewQueryKeepsUnknown(t *testing.T) {
	db := newCollectorDryRunDB(t)
	now := time.Date(2026, 6, 25, 11, 15, 0, 0, time.Local)
	rows := make([]struct {
		Transport string `gorm:"column:transport"` // 来源通道
	}, 0)

	tx := buildCollectorTransportOverviewQuery(db, now).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildCollectorTransportOverviewQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"COALESCE(NULLIF(transport, ''), 'unknown') AS transport",
		"GROUP BY COALESCE(NULLIF(transport, ''), 'unknown')",
		"ORDER BY total_count DESC",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if strings.Contains(sqlText, "transport <> ''") {
		t.Fatalf("unexpected transport filter in SQL: %s", sqlText)
	}
}

// newCollectorDryRunDB 创建 Collector SQL 断言使用的 DryRun DB。
func newCollectorDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
