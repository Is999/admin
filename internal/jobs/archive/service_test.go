package archive

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"admin_cron/internal/config"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// newArchiveDryRunDB 创建归档测试使用的 GORM DryRun 连接，只生成 SQL 不访问真实数据库。
func newArchiveDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

// TestBuildHistoryTableNameByMonth 验证按月拆表时历史表命名稳定可预测。
func TestBuildHistoryTableNameByMonth(t *testing.T) {
	job := jobConfig{
		TableName:          "admin_log",
		HistoryTablePrefix: "admin_log_archive",
		SplitUnit:          SplitUnitMonth,
	}
	start := time.Date(2026, time.May, 3, 10, 0, 0, 0, time.Local)
	got := buildHistoryTableName(job, start, start.Add(time.Hour))
	if got != "admin_log_archive_202605" {
		t.Fatalf("buildHistoryTableName() = %s, want %s", got, "admin_log_archive_202605")
	}
}

// TestBuildHistoryTableNameByQuarter 验证按季度拆表时命名会落到正确季度编号。
func TestBuildHistoryTableNameByQuarter(t *testing.T) {
	job := jobConfig{
		TableName:          "admin_log",
		HistoryTablePrefix: "admin_log_archive",
		SplitUnit:          SplitUnitQuarter,
	}
	start := time.Date(2026, time.May, 3, 10, 0, 0, 0, time.Local)
	got := buildHistoryTableName(job, start, start.Add(time.Hour))
	if got != "admin_log_archive_2026q2" {
		t.Fatalf("buildHistoryTableName() = %s, want %s", got, "admin_log_archive_2026q2")
	}
}

// TestBuildHistoryTableNameByRuleAndNone 验证固定历史表和命名模板都能生成稳定表名。
func TestBuildHistoryTableNameByRuleAndNone(t *testing.T) {
	start := time.Date(2026, time.May, 3, 10, 15, 0, 0, time.Local)
	end := start.Add(15 * time.Minute)
	noneJob := jobConfig{
		TableName:          "admin_log",
		HistoryTablePrefix: "admin_log_archive",
		SplitUnit:          SplitUnitNone,
	}
	if got := buildHistoryTableName(noneJob, start, end); got != "admin_log_archive" {
		t.Fatalf("buildHistoryTableName(none) = %s, want %s", got, "admin_log_archive")
	}

	ruleJob := jobConfig{
		TableName:            "admin_log",
		HistoryTablePrefix:   "admin_log_archive",
		HistoryTableNameRule: "{prefix}_{range_start}_{range_end}",
	}
	got := buildHistoryTableName(ruleJob, start, end)
	want := "admin_log_archive_20260503101500_20260503103000"
	if got != want {
		t.Fatalf("buildHistoryTableName(rule) = %s, want %s", got, want)
	}
}

// TestAlignSegmentStartCustomDays 验证自定义天数拆分时会对齐到固定块起点。
func TestAlignSegmentStartCustomDays(t *testing.T) {
	input := time.Date(2026, time.May, 15, 12, 30, 0, 0, time.Local)
	start := alignSegmentStart(input, SplitUnitCustomDays, 90)
	next := nextSegmentBoundary(start, SplitUnitCustomDays, 90)
	if start.After(input) {
		t.Fatalf("alignSegmentStart() = %s, should not be after input %s", start.Format(time.DateTime), input.Format(time.DateTime))
	}
	if !next.After(input) {
		t.Fatalf("nextSegmentBoundary() = %s, should be after input %s", next.Format(time.DateTime), input.Format(time.DateTime))
	}
	if next.Sub(start) != 90*24*time.Hour {
		t.Fatalf("custom_days segment length = %s, want %s", next.Sub(start), 90*24*time.Hour)
	}
}

// TestNextSegmentBoundaryMonth 验证按月拆分时下一个边界会跳到次月月初。
func TestNextSegmentBoundaryMonth(t *testing.T) {
	start := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.Local)
	got := nextSegmentBoundary(start, SplitUnitMonth, 0)
	want := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("nextSegmentBoundary() = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}
}

// TestNextSegmentBoundaryMonthFromMidMonth 验证 watermark 落在月中时仍按自然月边界切段，避免跨月数据写入同一张月表。
func TestNextSegmentBoundaryMonthFromMidMonth(t *testing.T) {
	start := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.Local)
	got := nextSegmentBoundary(start, SplitUnitMonth, 0)
	want := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("nextSegmentBoundary() = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}
}

// TestNormalizeSplitUnitFallbackMonth 验证非法拆分粒度会安全回退到按月归档。
func TestNormalizeSplitUnitFallbackMonth(t *testing.T) {
	if got := normalizeSplitUnit("unknown"); got != SplitUnitMonth {
		t.Fatalf("normalizeSplitUnit() = %s, want %s", got, SplitUnitMonth)
	}
}

// TestNormalizeSplitUnitSupportsNone 验证 none 拆分粒度会保留为固定历史表模式。
func TestNormalizeSplitUnitSupportsNone(t *testing.T) {
	if got := normalizeSplitUnit("none"); got != SplitUnitNone {
		t.Fatalf("normalizeSplitUnit() = %s, want %s", got, SplitUnitNone)
	}
}

// TestAlignWindowEnd 验证归档和删除窗口会按固定秒数对齐到最近结束边界。
func TestAlignWindowEnd(t *testing.T) {
	input := time.Date(2026, time.May, 15, 10, 17, 35, 0, time.Local)
	got := alignWindowEnd(input, 15*60)
	want := time.Date(2026, time.May, 15, 10, 15, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("alignWindowEnd() = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}
}

// TestAlignInitialArchiveCursorWindowUsesWindowBoundary 验证窗口化归档首次起点不会回退到月初空跑。
func TestAlignInitialArchiveCursorWindowUsesWindowBoundary(t *testing.T) {
	input := time.Date(2025, time.May, 22, 14, 36, 52, 0, time.Local)
	windowJob := jobConfig{
		SplitUnit:            SplitUnitMonth,
		ArchiveWindowSeconds: 60 * 60,
	}
	got := alignInitialArchiveCursor(input, windowJob)
	want := time.Date(2025, time.May, 22, 14, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("alignInitialArchiveCursor(window) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}

	monthJob := jobConfig{SplitUnit: SplitUnitMonth}
	got = alignInitialArchiveCursor(input, monthJob)
	want = time.Date(2025, time.May, 1, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("alignInitialArchiveCursor(month) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}

	dateOnlyStringJob := jobConfig{
		SplitUnit:            SplitUnitMonth,
		TimeColumnType:       TimeColumnTypeString,
		TimeColumnFormat:     "2006-01-02",
		ArchiveWindowSeconds: 60 * 60,
	}
	got = alignInitialArchiveCursor(input, dateOnlyStringJob)
	if !got.Equal(want) {
		t.Fatalf("alignInitialArchiveCursor(date_only_string) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}
	if dateOnlyStringJob.hasArchiveSecondWindow() {
		t.Fatal("date-only 字符串时间列不应启用秒级归档窗口")
	}
}

// TestNextArchiveSegmentBoundaryClampsSplitBoundary 验证窗口化归档不会越过历史表拆分边界。
func TestNextArchiveSegmentBoundaryClampsSplitBoundary(t *testing.T) {
	dayJob := jobConfig{
		SplitUnit:            SplitUnitDay,
		ArchiveWindowSeconds: 3 * 60 * 60,
	}
	start := time.Date(2026, time.May, 15, 23, 0, 0, 0, time.Local)
	if got, want := nextArchiveSegmentBoundary(start, dayJob), time.Date(2026, time.May, 16, 0, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Fatalf("nextArchiveSegmentBoundary(day) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}

	monthJob := jobConfig{
		SplitUnit:            SplitUnitMonth,
		ArchiveWindowSeconds: 2 * 60 * 60,
	}
	monthStart := time.Date(2026, time.May, 31, 23, 0, 0, 0, time.Local)
	if got, want := nextArchiveSegmentBoundary(monthStart, monthJob), time.Date(2026, time.June, 1, 0, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Fatalf("nextArchiveSegmentBoundary(month) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}

	noneJob := jobConfig{
		SplitUnit:            SplitUnitNone,
		ArchiveWindowSeconds: 2 * 60 * 60,
	}
	if got, want := nextArchiveSegmentBoundary(monthStart, noneJob), monthStart.Add(2*time.Hour); !got.Equal(want) {
		t.Fatalf("nextArchiveSegmentBoundary(none) = %s, want %s", got.Format(time.DateTime), want.Format(time.DateTime))
	}
}

// TestNormalizedJobsDefaultControlDatabase 验证未显式配置时归档控制表默认使用主库。
func TestNormalizedJobsDefaultControlDatabase(t *testing.T) {
	// svcCtx 构造仅包含归档配置的最小服务上下文，用于验证默认控制库归属。
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:      "admin_log",
					Enabled:   true,
					Database:  string(svc.DatabaseMain),
					TableName: "admin_log",
				},
			},
		},
	}, svc.Dependencies{})
	// service 未显式传入 WithControlDatabase 时，控制表应默认使用主库。
	service := NewService(svcCtx)
	jobs := service.normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	if service.controlDatabaseName() != svc.DatabaseMain {
		t.Fatalf("controlDatabaseName() = %s, want %s", service.controlDatabaseName(), svc.DatabaseMain)
	}
	if jobs[0].TimeColumnType != TimeColumnTypeTime {
		t.Fatalf("TimeColumnType = %s, want %s", jobs[0].TimeColumnType, TimeColumnTypeTime)
	}
	if jobs[0].TimeColumnUnixUnit != "" {
		t.Fatalf("TimeColumnUnixUnit = %s, want empty for time column", jobs[0].TimeColumnUnixUnit)
	}
}

// TestNormalizedJobsWindowDefaults 验证归档和删除窗口配置会补齐安全默认值。
func TestNormalizedJobsWindowDefaults(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:                 "admin_log",
					Enabled:              true,
					Database:             string(svc.DatabaseMain),
					TableName:            "admin_log",
					HotKeepDays:          32,
					ArchiveDelayDays:     2,
					ArchiveWindowSeconds: 15 * 60,
					DeleteDelayDays:      32,
					DeleteWindowSeconds:  60 * 60,
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.ArchiveDelayDays != 2 || job.DeleteDelayDays != 32 {
		t.Fatalf("归档/删除延迟天数异常: archive=%d delete=%d", job.ArchiveDelayDays, job.DeleteDelayDays)
	}
	if job.ArchiveMaxWindowsPerRun != 1 {
		t.Fatalf("ArchiveMaxWindowsPerRun = %d, want 1", job.ArchiveMaxWindowsPerRun)
	}
	if job.ArchiveWindowMode != ArchiveWindowModeAuto {
		t.Fatalf("ArchiveWindowMode = %s, want %s", job.ArchiveWindowMode, ArchiveWindowModeAuto)
	}
	if job.ArchiveAutoMaxWindows != defaultArchiveAutoMaxWindows {
		t.Fatalf("ArchiveAutoMaxWindows = %d, want %d", job.ArchiveAutoMaxWindows, defaultArchiveAutoMaxWindows)
	}
	if job.ArchiveAutoLightRows != defaultArchiveAutoLightRows {
		t.Fatalf("ArchiveAutoLightRows = %d, want %d", job.ArchiveAutoLightRows, defaultArchiveAutoLightRows)
	}
	if job.ArchiveAutoLightElapsed != defaultArchiveAutoLightElapsed {
		t.Fatalf("ArchiveAutoLightElapsed = %s, want %s", job.ArchiveAutoLightElapsed, defaultArchiveAutoLightElapsed)
	}
	if job.DeleteMaxWindowsPerRun != 4 {
		t.Fatalf("DeleteMaxWindowsPerRun = %d, want 4", job.DeleteMaxWindowsPerRun)
	}
}

// TestNormalizedJobsDeleteAutoMaxWindowsWithoutArchiveWindow 验证仅配置删除秒级窗口时，删除 auto 仍能使用默认追赶上限。
func TestNormalizedJobsDeleteAutoMaxWindowsWithoutArchiveWindow(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:                "admin_log",
					Enabled:             true,
					Database:            string(svc.DatabaseMain),
					TableName:           "admin_log",
					HotKeepDays:         32,
					ArchiveDelayDays:    2,
					DeleteDelayDays:     32,
					DeleteWindowSeconds: 3600,
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.ArchiveMaxWindowsPerRun != 0 {
		t.Fatalf("ArchiveMaxWindowsPerRun = %d, want 0", job.ArchiveMaxWindowsPerRun)
	}
	if job.DeleteMaxWindowsPerRun != 1 {
		t.Fatalf("DeleteMaxWindowsPerRun = %d, want 1", job.DeleteMaxWindowsPerRun)
	}
	if job.ArchiveAutoMaxWindows != defaultArchiveAutoMaxWindows {
		t.Fatalf("ArchiveAutoMaxWindows = %d, want %d", job.ArchiveAutoMaxWindows, defaultArchiveAutoMaxWindows)
	}
	if !shouldContinueDeleteRun(job, 1, &deleteSegmentResult{RowsDeleted: 0, Elapsed: 20 * time.Millisecond, Progressed: true}) {
		t.Fatal("仅配置删除秒级窗口时，删除 auto 应继续追赶空窗口")
	}
}

// TestShouldContinueArchiveRunAutoFastForwardsLightWindows 验证 auto 模式会在轻量窗口后继续追水位。
func TestShouldContinueArchiveRunAutoFastForwardsLightWindows(t *testing.T) {
	job := jobConfig{
		ArchiveWindowSeconds:    3600,
		ArchiveWindowMode:       ArchiveWindowModeAuto,
		ArchiveMaxWindowsPerRun: 2,
		ArchiveAutoMaxWindows:   10,
		ArchiveAutoLightRows:    100,
		ArchiveAutoLightElapsed: time.Second,
	}
	lightResult := &archiveSegmentResult{RowsArchived: 0, Elapsed: 20 * time.Millisecond}
	if !shouldContinueArchiveRun(job, 0, nil) || !shouldContinueArchiveRun(job, 1, lightResult) {
		t.Fatal("auto 模式在基础窗口内应继续执行")
	}
	if !shouldContinueArchiveRun(job, 2, lightResult) {
		t.Fatal("auto 模式遇到轻量窗口后应继续追水位")
	}
	heavyRows := &archiveSegmentResult{RowsArchived: 101, Elapsed: 20 * time.Millisecond}
	if shouldContinueArchiveRun(job, 2, heavyRows) {
		t.Fatal("auto 模式遇到超过行数阈值的窗口后应停止追水位")
	}
	slowResult := &archiveSegmentResult{RowsArchived: 0, Elapsed: 2 * time.Second}
	if shouldContinueArchiveRun(job, 2, slowResult) {
		t.Fatal("auto 模式遇到超过耗时阈值的窗口后应停止追水位")
	}
	if shouldContinueArchiveRun(job, 10, lightResult) {
		t.Fatal("auto 模式达到自动窗口上限后应停止")
	}
}

// TestShouldContinueArchiveRunFixedStopsAtBaseLimit 验证 fixed 模式严格按 archive_max_windows_per_run 推进。
func TestShouldContinueArchiveRunFixedStopsAtBaseLimit(t *testing.T) {
	job := jobConfig{
		ArchiveWindowSeconds:    3600,
		ArchiveWindowMode:       ArchiveWindowModeFixed,
		ArchiveMaxWindowsPerRun: 2,
		ArchiveAutoMaxWindows:   10,
	}
	lightResult := &archiveSegmentResult{RowsArchived: 0, Elapsed: 20 * time.Millisecond}
	if !shouldContinueArchiveRun(job, 1, lightResult) {
		t.Fatal("fixed 模式在基础窗口内应继续执行")
	}
	if shouldContinueArchiveRun(job, 2, lightResult) {
		t.Fatal("fixed 模式达到基础窗口上限后应停止")
	}
}

// TestShouldContinueDeleteRunAutoFastForwardsLightWindows 验证删除 auto 模式会追赶空窗口和稀疏窗口。
func TestShouldContinueDeleteRunAutoFastForwardsLightWindows(t *testing.T) {
	job := jobConfig{
		TimeColumnType:          TimeColumnTypeTime,
		DeleteWindowSeconds:     3600,
		ArchiveWindowMode:       ArchiveWindowModeAuto,
		DeleteMaxWindowsPerRun:  2,
		DeleteBatchSize:         50,
		ArchiveAutoMaxWindows:   10,
		ArchiveAutoLightRows:    100,
		ArchiveAutoLightElapsed: time.Second,
	}
	lightResult := &deleteSegmentResult{RowsDeleted: 0, Elapsed: 20 * time.Millisecond, Progressed: true}
	if !shouldContinueDeleteRun(job, 0, nil) || !shouldContinueDeleteRun(job, 1, lightResult) {
		t.Fatal("删除 auto 模式在基础窗口内应继续执行")
	}
	if !shouldContinueDeleteRun(job, 2, lightResult) {
		t.Fatal("删除 auto 模式遇到空窗口后应继续追水位")
	}
	sparseResult := &deleteSegmentResult{RowsDeleted: 50, Elapsed: 20 * time.Millisecond, Progressed: true}
	if !shouldContinueDeleteRun(job, 3, sparseResult) {
		t.Fatal("删除 auto 模式遇到阈值内稀疏窗口后应继续追水位")
	}
	heavyRows := &deleteSegmentResult{RowsDeleted: 51, Elapsed: 20 * time.Millisecond, Progressed: true}
	if shouldContinueDeleteRun(job, 2, heavyRows) {
		t.Fatal("删除 auto 模式遇到超过 delete_batch_size 的窗口后应停止追水位")
	}
	defaultBatchJob := job
	defaultBatchJob.DeleteBatchSize = 0
	defaultBatchJob.ArchiveAutoLightRows = defaultDeleteBatchSize + 1
	defaultBatchRows := &deleteSegmentResult{RowsDeleted: int64(defaultDeleteBatchSize) + 1, Elapsed: 20 * time.Millisecond, Progressed: true}
	if shouldContinueDeleteRun(defaultBatchJob, 2, defaultBatchRows) {
		t.Fatal("删除 auto 模式未显式配置 delete_batch_size 时也应按默认删除批大小停止追水位")
	}
	slowResult := &deleteSegmentResult{RowsDeleted: 0, Elapsed: 2 * time.Second, Progressed: true}
	if shouldContinueDeleteRun(job, 2, slowResult) {
		t.Fatal("删除 auto 模式遇到超过耗时阈值的窗口后应停止追水位")
	}
	stalledResult := &deleteSegmentResult{RowsDeleted: 0, Elapsed: 20 * time.Millisecond, Progressed: false}
	if shouldContinueDeleteRun(job, 2, stalledResult) {
		t.Fatal("删除 auto 模式遇到未推进窗口后应停止，避免重复抢占同一区间")
	}
	if shouldContinueDeleteRun(job, 10, lightResult) {
		t.Fatal("删除 auto 模式达到自动窗口上限后应停止")
	}
}

// TestShouldContinueDeleteRunFixedStopsAtBaseLimit 验证 fixed 模式删除仍严格按 delete_max_windows_per_run 推进。
func TestShouldContinueDeleteRunFixedStopsAtBaseLimit(t *testing.T) {
	job := jobConfig{
		TimeColumnType:         TimeColumnTypeTime,
		DeleteWindowSeconds:    3600,
		ArchiveWindowMode:      ArchiveWindowModeFixed,
		DeleteMaxWindowsPerRun: 2,
		ArchiveAutoMaxWindows:  10,
	}
	lightResult := &deleteSegmentResult{RowsDeleted: 0, Elapsed: 20 * time.Millisecond, Progressed: true}
	if !shouldContinueDeleteRun(job, 1, lightResult) {
		t.Fatal("fixed 模式删除在基础窗口内应继续执行")
	}
	if shouldContinueDeleteRun(job, 2, lightResult) {
		t.Fatal("fixed 模式删除达到基础窗口上限后应停止")
	}
}

// TestArchiveUpperBoundUsesArchiveDelayDays 验证归档上界由 archive_delay_days 控制，而不是误用热表删除保留天数。
func TestArchiveUpperBoundUsesArchiveDelayDays(t *testing.T) {
	job := jobConfig{
		HotKeepDays:      32,
		ArchiveDelayDays: 2,
	}
	upperBound, ok := NewService(nil).archiveUpperBound(context.Background(), job, nil)
	if !ok {
		t.Fatal("archiveUpperBound() ok = false, want true")
	}
	want := time.Now().AddDate(0, 0, -2)
	if upperBound.Before(want.Add(-time.Hour)) || upperBound.After(want.Add(time.Hour)) {
		t.Fatalf("archiveUpperBound() = %s, want around %s", upperBound.Format(time.DateTime), want.Format(time.DateTime))
	}
}

// TestRangePredicateArgsStringTime 验证 string 任务使用字符串边界。
func TestRangePredicateArgsStringTime(t *testing.T) {
	job := jobConfig{TimeColumnType: TimeColumnTypeString, TimeColumnFormat: defaultArchiveStringTimeLayout}
	start := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local)
	args := rangePredicateArgs(job, start, end)
	if len(args) != 2 || args[0] != "2026-01-04 00:00:00" || args[1] != "2026-02-01 00:00:00" {
		t.Fatalf("rangePredicateArgs(string) = %#v", args)
	}
}

// TestRangePredicateArgsStringTimeCustomFormat 验证字符串时间列会使用配置格式绑定边界。
func TestRangePredicateArgsStringTimeCustomFormat(t *testing.T) {
	job := jobConfig{TimeColumnType: TimeColumnTypeString, TimeColumnFormat: "2006/01/02 15:04:05"}
	start := time.Date(2026, time.January, 4, 10, 30, 0, 0, time.Local)
	end := time.Date(2026, time.January, 4, 11, 30, 0, 0, time.Local)
	args := rangePredicateArgs(job, start, end)
	if len(args) != 2 || args[0] != "2026/01/04 10:30:00" || args[1] != "2026/01/04 11:30:00" {
		t.Fatalf("rangePredicateArgs(string_custom) = %#v", args)
	}
}

// TestRangePredicateArgsUnixSeconds 验证 Unix 秒时间列在归档事务内使用秒级 int64 边界。
func TestRangePredicateArgsUnixSeconds(t *testing.T) {
	job := jobConfig{TimeColumnType: TimeColumnTypeUnix, TimeColumnUnixUnit: TimeColumnUnixUnitSeconds}
	start := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local)
	args := rangePredicateArgs(job, start, end)
	if len(args) != 2 || args[0] != start.Unix() || args[1] != end.Unix() {
		t.Fatalf("rangePredicateArgs(unix seconds) = %#v", args)
	}
}

// TestRangePredicateArgsUnixMilliseconds 验证 Unix 毫秒时间列在归档事务内使用毫秒边界。
func TestRangePredicateArgsUnixMilliseconds(t *testing.T) {
	job := jobConfig{TimeColumnType: TimeColumnTypeUnix, TimeColumnUnixUnit: TimeColumnUnixUnitMilliseconds}
	start := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local)
	args := rangePredicateArgs(job, start, end)
	if len(args) != 2 || args[0] != start.UnixMilli() || args[1] != end.UnixMilli() {
		t.Fatalf("rangePredicateArgs(unix milliseconds) = %#v", args)
	}
}

// TestNormalizeTimeColumnType 验证时间列类型未配置时默认 time，旧枚举和非法类型会被拒绝。
func TestNormalizeTimeColumnType(t *testing.T) {
	if got := normalizeTimeColumnType(""); got != TimeColumnTypeTime {
		t.Fatalf("normalizeTimeColumnType(empty) = %s, want %s", got, TimeColumnTypeTime)
	}
	if got := normalizeTimeColumnType(TimeColumnTypeString); got != TimeColumnTypeString {
		t.Fatalf("normalizeTimeColumnType(string) = %s, want %s", got, TimeColumnTypeString)
	}
	if got := normalizeTimeColumnType(TimeColumnTypeUnix); got != TimeColumnTypeUnix {
		t.Fatalf("normalizeTimeColumnType(unix) = %s, want %s", got, TimeColumnTypeUnix)
	}
	for _, value := range []string{"datetime", "date_string", "unix_seconds", "unix_milliseconds", "bad_type"} {
		if got := normalizeTimeColumnType(value); got != "" {
			t.Fatalf("normalizeTimeColumnType(%s) = %s, want empty", value, got)
		}
	}
}

// TestNormalizeTimeColumnUnixUnit 验证 Unix 时间单位支持秒和毫秒。
func TestNormalizeTimeColumnUnixUnit(t *testing.T) {
	if got := normalizeTimeColumnUnixUnit(TimeColumnTypeUnix, ""); got != TimeColumnUnixUnitSeconds {
		t.Fatalf("normalizeTimeColumnUnixUnit(empty) = %s, want %s", got, TimeColumnUnixUnitSeconds)
	}
	if got := normalizeTimeColumnUnixUnit(TimeColumnTypeUnix, "milliseconds"); got != TimeColumnUnixUnitMilliseconds {
		t.Fatalf("normalizeTimeColumnUnixUnit(milliseconds) = %s, want %s", got, TimeColumnUnixUnitMilliseconds)
	}
	if got := normalizeTimeColumnUnixUnit(TimeColumnTypeUnix, "bad_unit"); got != "" {
		t.Fatalf("normalizeTimeColumnUnixUnit(bad_unit) = %s, want empty", got)
	}
}

// TestParseArchiveStringTimeStrictAndCustom 验证字符串时间支持默认完整时间格式和自定义格式。
func TestParseArchiveStringTimeStrictAndCustom(t *testing.T) {
	got, err := parseArchiveStringTime("2026-02-04 17:11:33", defaultArchiveStringTimeLayout)
	if err != nil {
		t.Fatalf("parseArchiveStringTime(valid) err = %v", err)
	}
	if got.Format(defaultArchiveStringTimeLayout) != "2026-02-04 17:11:33" {
		t.Fatalf("parseArchiveStringTime(valid) = %s, want 2026-02-04 17:11:33", got.Format(defaultArchiveStringTimeLayout))
	}
	full, err := parseArchiveStringTime("2026/02/04 17:11:33", "2006/01/02 15:04:05")
	if err != nil {
		t.Fatalf("parseArchiveStringTime(custom) err = %v", err)
	}
	if full.Format(time.DateTime) != "2026-02-04 17:11:33" {
		t.Fatalf("parseArchiveStringTime(custom) = %s, want 2026-02-04 17:11:33", full.Format(time.DateTime))
	}
	for _, value := range []string{"2026-02-04", "2026-02-31 17:11:33", ""} {
		if _, err = parseArchiveStringTime(value, defaultArchiveStringTimeLayout); err == nil {
			t.Fatalf("parseArchiveStringTime(%q) expected error", value)
		}
	}
}

// TestParseArchiveUnixTimeSupportsSecondsAndMilliseconds 验证 Unix 游标支持秒和毫秒，并拒绝无效时间。
func TestParseArchiveUnixTimeSupportsSecondsAndMilliseconds(t *testing.T) {
	if _, err := parseArchiveUnixTime(0, TimeColumnUnixUnitSeconds); err == nil {
		t.Fatal("parseArchiveUnixTime(0) err = nil, want error")
	}
	input := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	got, err := parseArchiveUnixTime(input.Unix(), TimeColumnUnixUnitSeconds)
	if err != nil {
		t.Fatalf("parseArchiveUnixTime(seconds) err = %v", err)
	}
	if got.Year() != 2026 || got.Month() != time.May || got.Day() != 15 {
		t.Fatalf("parseArchiveUnixTime(seconds) = %s, want 2026-05-15", got.Format(time.DateTime))
	}
	got, err = parseArchiveUnixTime(input.UnixMilli(), TimeColumnUnixUnitMilliseconds)
	if err != nil {
		t.Fatalf("parseArchiveUnixTime(milliseconds) err = %v", err)
	}
	if !got.Equal(input) {
		t.Fatalf("parseArchiveUnixTime(milliseconds) = %s, want %s", got.Format(time.DateTime), input.Format(time.DateTime))
	}
}

// TestParseArchiveStartAt 验证首次归档起点支持日期和完整时间格式。
func TestParseArchiveStartAt(t *testing.T) {
	dateOnly, err := parseArchiveStartAt("2026-01-01")
	if err != nil {
		t.Fatalf("parseArchiveStartAt(date) err = %v", err)
	}
	if dateOnly.Format(time.DateTime) != "2026-01-01 00:00:00" {
		t.Fatalf("parseArchiveStartAt(date) = %s, want 2026-01-01 00:00:00", dateOnly.Format(time.DateTime))
	}
	dateTime, err := parseArchiveStartAt("2026-01-01 12:30:00")
	if err != nil {
		t.Fatalf("parseArchiveStartAt(datetime) err = %v", err)
	}
	if dateTime.Format(time.DateTime) != "2026-01-01 12:30:00" {
		t.Fatalf("parseArchiveStartAt(datetime) = %s, want 2026-01-01 12:30:00", dateTime.Format(time.DateTime))
	}
	if _, err = parseArchiveStartAt("2026/01/01"); err == nil {
		t.Fatal("parseArchiveStartAt(invalid) expected error")
	}
}

// TestNormalizedJobsStringTimeConfig 验证 string 和 start_at 配置会进入运行期归档任务。
func TestNormalizedJobsStringTimeConfig(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:               "string_time_demo",
					Enabled:            true,
					Database:           string(svc.DatabaseMain),
					TableName:          "string_time_source",
					TimeColumn:         "create_date",
					TimeColumnType:     TimeColumnTypeString,
					PrimaryKey:         "id",
					HistoryTablePrefix: "string_time_source_archive",
					StartAt:            "2026-01-01",
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if !job.isStringTimeColumn() {
		t.Fatalf("isStringTimeColumn() = false, want true")
	}
	if !job.StartAt.Valid || job.StartAt.Time.Format("2006-01-02") != "2026-01-01" {
		t.Fatalf("StartAt = %#v, want 2026-01-01", job.StartAt)
	}
	if job.TimeColumnFormat != defaultArchiveStringTimeLayout {
		t.Fatalf("TimeColumnFormat = %s, want %s", job.TimeColumnFormat, defaultArchiveStringTimeLayout)
	}
	if got := buildHistoryTableName(job, job.StartAt.Time, job.StartAt.Time.AddDate(0, 1, 0)); got != "string_time_source_archive_202601" {
		t.Fatalf("buildHistoryTableName() = %s, want string_time_source_archive_202601", got)
	}
}

// TestNormalizedJobsUnixDefaultUnit 验证 unix 时间列未配置单位时默认按秒处理。
func TestNormalizedJobsUnixDefaultUnit(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:           "unix_time_demo",
					Enabled:        true,
					Database:       string(svc.DatabaseMain),
					TableName:      "unix_time_source",
					TimeColumn:     "created_time",
					TimeColumnType: TimeColumnTypeUnix,
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.TimeColumnType != TimeColumnTypeUnix {
		t.Fatalf("TimeColumnType = %s, want %s", job.TimeColumnType, TimeColumnTypeUnix)
	}
	if job.TimeColumnUnixUnit != TimeColumnUnixUnitSeconds {
		t.Fatalf("TimeColumnUnixUnit = %s, want %s", job.TimeColumnUnixUnit, TimeColumnUnixUnitSeconds)
	}
}

// TestNormalizedJobsSkipsInvalidAndKeepsValid 验证异常归档配置只跳过自身，不影响其它合法 job。
func TestNormalizedJobsSkipsInvalidAndKeepsValid(t *testing.T) {
	// svcCtx 同时配置一个非法数据库名 job 和一个合法 job，用于模拟运维误配后的部分可运行场景。
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:      "bad_database",
					Enabled:   true,
					Database:  "bad-name",
					TableName: "admin_log",
				},
				{
					Name:      "admin_log",
					Enabled:   true,
					Database:  string(svc.DatabaseMain),
					TableName: "admin_log",
				},
			},
		},
	}, svc.Dependencies{})

	// normalizedJobs 应保留合法 job，并由生产代码记录非法 job 的结构化错误日志。
	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	if jobs[0].Name != "admin_log" {
		t.Fatalf("normalizedJobs()[0].Name = %s, want admin_log", jobs[0].Name)
	}
}

// TestNormalizedJobsKeepsCustomConditions 验证归档和清理自定义条件会进入运行期配置。
func TestNormalizedJobsKeepsCustomConditions(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:             "admin_log",
					Enabled:          true,
					Database:         string(svc.DatabaseMain),
					TableName:        "admin_log",
					ArchiveCondition: "status = 1 AND action <> 'delete'",
					DeleteCondition:  "is_deleted = 0",
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	if jobs[0].ArchiveCondition != "status = 1 AND action <> 'delete'" {
		t.Fatalf("ArchiveCondition = %s", jobs[0].ArchiveCondition)
	}
	if jobs[0].DeleteCondition != "is_deleted = 0" {
		t.Fatalf("DeleteCondition = %s", jobs[0].DeleteCondition)
	}
}

// TestDeleteConditionSQLCombinesArchiveAndDelete 验证清理条件会叠加归档条件，避免删除未归档行。
func TestDeleteConditionSQLCombinesArchiveAndDelete(t *testing.T) {
	job := jobConfig{
		ArchiveCondition: "status = 1 OR status = 2",
		DeleteCondition:  "is_deleted = 0",
	}
	got := deleteConditionSQL(job)
	want := "(status = 1 OR status = 2) AND (is_deleted = 0)"
	if got != want {
		t.Fatalf("deleteConditionSQL() = %s, want %s", got, want)
	}
}

// TestBatchSourcePredicateSQLIncludesTimeWindow 验证批次复制和删除都会在事务内再次约束时间窗口。
func TestBatchSourcePredicateSQLIncludesTimeWindow(t *testing.T) {
	job := jobConfig{
		PrimaryKey: "id",
		TimeColumn: "created_at",
	}
	got := batchSourcePredicateSQL(job)
	want := "`id` IN ? AND `created_at` >= ? AND `created_at` < ?"
	if got != want {
		t.Fatalf("batchSourcePredicateSQL() = %s, want %s", got, want)
	}
}

// TestBuildMinArchivableTimeQueryUsesGORMChain 验证首次归档起点查询不再依赖 Raw SQL，且自定义归档条件仍会生效。
func TestBuildMinArchivableTimeQueryUsesGORMChain(t *testing.T) {
	db := newArchiveDryRunDB(t)
	upperBound := time.Date(2026, time.May, 16, 0, 0, 0, 0, time.Local)
	job := jobConfig{
		TableName:        "admin_log",
		TimeColumn:       "created_at",
		ArchiveCondition: "status = 1 AND action <> 'delete'",
	}
	var row struct {
		MinTime time.Time `gorm:"column:min_time"` // MinTime 表示 DryRun 扫描目标字段，仅用于触发 GORM 生成 SQL
	}
	tx := buildMinArchivableTimeQuery(context.Background(), db, job, upperBound).Find(&row)
	if tx.Error != nil {
		t.Fatalf("buildMinArchivableTimeQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT MIN(`created_at`) AS min_time FROM `admin_log`",
		"WHERE `created_at` < ?",
		"(status = 1 AND action <> 'delete')",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 1 || tx.Statement.Vars[0] != upperBound {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
}

// TestBuildLoadBatchCursorRowsQueryUsesGORMChain 验证归档游标读取查询由 GORM 链式构造，并保留断点续跑条件。
func TestBuildLoadBatchCursorRowsQueryUsesGORMChain(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.Add(time.Hour)
	lastTime := rangeStart.Add(30 * time.Minute)
	job := jobConfig{
		TableName:        "admin_log",
		PrimaryKey:       "id",
		TimeColumn:       "created_at",
		ArchiveCondition: "status = 1",
	}
	segment := &Segment{
		RangeStart:       rangeStart,
		RangeEnd:         rangeEnd,
		LastArchivedID:   100,
		LastArchivedTime: sql.NullTime{Time: lastTime, Valid: true},
	}
	var rows []batchCursorRow
	tx := buildLoadBatchCursorRowsQuery(context.Background(), db, job, segment, 50).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadBatchCursorRowsQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT `id` AS id, `created_at` AS created_at FROM `admin_log`",
		"`created_at` >= ?",
		"`created_at` < ?",
		"(status = 1)",
		"`created_at` > ? OR (`created_at` = ? AND `id` > ?)",
		"ORDER BY `created_at`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 6 {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
}

// TestBuildLoadDeleteCursorRowsQueryUsesGORMChain 验证删除游标读取查询由 GORM 链式构造，并叠加归档和删除条件。
func TestBuildLoadDeleteCursorRowsQueryUsesGORMChain(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.Add(30 * time.Minute)
	job := jobConfig{
		TableName:        "admin_log",
		PrimaryKey:       "id",
		TimeColumn:       "created_at",
		ArchiveCondition: "status = 1",
		DeleteCondition:  "action <> 'keep'",
	}
	segment := &Segment{
		RangeStart: rangeStart,
		RangeEnd:   rangeStart.Add(time.Hour),
	}
	var rows []batchCursorRow
	tx := buildLoadDeleteCursorRowsQuery(context.Background(), db, job, segment, rangeEnd, 80).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadDeleteCursorRowsQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT `id` AS id, `created_at` AS created_at FROM `admin_log`",
		"`created_at` >= ?",
		"`created_at` < ?",
		"(status = 1) AND (action <> 'keep')",
		"ORDER BY `created_at`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 3 {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
	if tx.Statement.Vars[0] != rangeStart || tx.Statement.Vars[1] != rangeEnd || tx.Statement.Vars[2] != 80 {
		t.Fatalf("unexpected var order: %#v", tx.Statement.Vars)
	}
}

// TestBuildLoadBatchCursorRowsQueryStringTimeUsesStringBounds 验证 string 归档游标使用配置格式字符串边界。
func TestBuildLoadBatchCursorRowsQueryStringTimeUsesStringBounds(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.AddDate(0, 0, 1)
	job := jobConfig{
		TableName:        "admin_log",
		PrimaryKey:       "id",
		TimeColumn:       "created_at",
		TimeColumnType:   TimeColumnTypeString,
		ArchiveCondition: "status = 1",
	}
	segment := &Segment{
		RangeStart:       rangeStart,
		RangeEnd:         rangeEnd,
		LastArchivedID:   100,
		LastArchivedTime: sql.NullTime{Time: rangeStart, Valid: true},
	}
	var rows []struct {
		ID        int64  `gorm:"column:id"`         // ID 承接 DryRun 生成字段
		CreatedAt string `gorm:"column:created_at"` // CreatedAt 承接字符串时间字段
	}
	tx := buildLoadBatchCursorRowsQuery(context.Background(), db, job, segment, 50).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadBatchCursorRowsQuery(string) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"`created_at` >= ? AND `created_at` < ?",
		"(status = 1)",
		"`created_at` > ? OR (`created_at` = ? AND `id` > ?)",
		"ORDER BY `created_at`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	wantVars := []any{"2026-05-15 00:00:00", "2026-05-16 00:00:00", "2026-05-15 00:00:00", "2026-05-15 00:00:00", int64(100), 50}
	if len(tx.Statement.Vars) != len(wantVars) {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
	for i, want := range wantVars {
		if tx.Statement.Vars[i] != want {
			t.Fatalf("var[%d] = %#v, want %#v; all vars=%#v", i, tx.Statement.Vars[i], want, tx.Statement.Vars)
		}
	}
}

// TestBuildLoadBatchCursorRowsQueryUnixSecondsUsesNumericBounds 验证 Unix 秒归档游标使用 int64 窗口和断点。
func TestBuildLoadBatchCursorRowsQueryUnixSecondsUsesNumericBounds(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.Add(time.Hour)
	lastTime := rangeStart.Add(30 * time.Minute)
	job := jobConfig{
		TableName:      "order",
		PrimaryKey:     "id",
		TimeColumn:     "created_time",
		TimeColumnType: TimeColumnTypeUnix,
	}
	segment := &Segment{
		RangeStart:       rangeStart,
		RangeEnd:         rangeEnd,
		LastArchivedID:   100,
		LastArchivedTime: sql.NullTime{Time: lastTime, Valid: true},
	}
	var rows []struct {
		ID        int64 `gorm:"column:id"`         // ID 承接 DryRun 生成字段
		CreatedAt int64 `gorm:"column:created_at"` // CreatedAt 承接 Unix int64 字段
	}
	tx := buildLoadBatchCursorRowsQuery(context.Background(), db, job, segment, 50).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadBatchCursorRowsQuery(unix) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"`created_time` >= ?",
		"`created_time` < ?",
		"`created_time` > ? OR (`created_time` = ? AND `id` > ?)",
		"ORDER BY `created_time`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	wantVars := []any{rangeStart.Unix(), rangeEnd.Unix(), lastTime.Unix(), lastTime.Unix(), int64(100), 50}
	if len(tx.Statement.Vars) != len(wantVars) {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
	for i, want := range wantVars {
		if tx.Statement.Vars[i] != want {
			t.Fatalf("var[%d] = %#v, want %#v; all vars=%#v", i, tx.Statement.Vars[i], want, tx.Statement.Vars)
		}
	}
}

// TestBuildLoadDeleteCursorRowsQueryStringTimeUsesStringBounds 验证 string 删除游标同样使用字符串边界和清理条件。
func TestBuildLoadDeleteCursorRowsQueryStringTimeUsesStringBounds(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.AddDate(0, 0, 1)
	job := jobConfig{
		TableName:        "admin_log",
		PrimaryKey:       "id",
		TimeColumn:       "created_at",
		TimeColumnType:   TimeColumnTypeString,
		ArchiveCondition: "status = 1",
		DeleteCondition:  "action <> 'keep'",
	}
	segment := &Segment{RangeStart: rangeStart, RangeEnd: rangeEnd}
	var rows []struct {
		ID        int64  `gorm:"column:id"`         // ID 承接 DryRun 生成字段
		CreatedAt string `gorm:"column:created_at"` // CreatedAt 承接字符串时间字段
	}
	tx := buildLoadDeleteCursorRowsQuery(context.Background(), db, job, segment, rangeEnd, 80).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadDeleteCursorRowsQuery(string) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"`created_at` >= ? AND `created_at` < ?",
		"(status = 1) AND (action <> 'keep')",
		"ORDER BY `created_at`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	wantVars := []any{"2026-05-15 00:00:00", "2026-05-16 00:00:00", 80}
	if len(tx.Statement.Vars) != len(wantVars) {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
	for i, want := range wantVars {
		if tx.Statement.Vars[i] != want {
			t.Fatalf("var[%d] = %#v, want %#v; all vars=%#v", i, tx.Statement.Vars[i], want, tx.Statement.Vars)
		}
	}
}

// TestBuildLoadDeleteCursorRowsQueryUnixSecondsUsesNumericBounds 验证 Unix 秒删除游标使用 int64 窗口。
func TestBuildLoadDeleteCursorRowsQueryUnixSecondsUsesNumericBounds(t *testing.T) {
	db := newArchiveDryRunDB(t)
	rangeStart := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local)
	rangeEnd := rangeStart.Add(time.Hour)
	job := jobConfig{
		TableName:        "order",
		PrimaryKey:       "id",
		TimeColumn:       "created_time",
		TimeColumnType:   TimeColumnTypeUnix,
		ArchiveCondition: "status = 1",
		DeleteCondition:  "action <> 'keep'",
	}
	segment := &Segment{RangeStart: rangeStart, RangeEnd: rangeEnd}
	var rows []struct {
		ID        int64 `gorm:"column:id"`         // ID 承接 DryRun 生成字段
		CreatedAt int64 `gorm:"column:created_at"` // CreatedAt 承接 Unix int64 字段
	}
	tx := buildLoadDeleteCursorRowsQuery(context.Background(), db, job, segment, rangeEnd, 80).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildLoadDeleteCursorRowsQuery(unix) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"`created_time` >= ?",
		"`created_time` < ?",
		"(status = 1) AND (action <> 'keep')",
		"ORDER BY `created_time`,`id` LIMIT ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	wantVars := []any{rangeStart.Unix(), rangeEnd.Unix(), 80}
	if len(tx.Statement.Vars) != len(wantVars) {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
	for i, want := range wantVars {
		if tx.Statement.Vars[i] != want {
			t.Fatalf("var[%d] = %#v, want %#v; all vars=%#v", i, tx.Statement.Vars[i], want, tx.Statement.Vars)
		}
	}
}

// TestBuildMinArchivableTimeQueryStringTimeUsesStringBound 验证 string 首次规划上界不会传入 time.Time 参数。
func TestBuildMinArchivableTimeQueryStringTimeUsesStringBound(t *testing.T) {
	db := newArchiveDryRunDB(t)
	upperBound := time.Date(2026, time.May, 16, 0, 0, 0, 0, time.Local)
	job := jobConfig{
		TableName:        "admin_log",
		TimeColumn:       "created_at",
		TimeColumnType:   TimeColumnTypeString,
		ArchiveCondition: "status = 1",
	}
	var row struct {
		MinTime string `gorm:"column:min_time"` // MinTime 承接 DryRun 聚合字段
	}
	tx := buildMinArchivableTimeQuery(context.Background(), db, job, upperBound).Find(&row)
	if tx.Error != nil {
		t.Fatalf("buildMinArchivableTimeQuery(string) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT MIN(`created_at`) AS min_time FROM `admin_log`",
		"WHERE `created_at` < ?",
		"(status = 1)",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 1 || tx.Statement.Vars[0] != "2026-05-16 00:00:00" {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
}

// TestBuildMinArchivableTimeQueryUnixSecondsUsesNumericBound 验证 Unix 秒首次规划使用 int64 上界并忽略非正时间。
func TestBuildMinArchivableTimeQueryUnixSecondsUsesNumericBound(t *testing.T) {
	db := newArchiveDryRunDB(t)
	upperBound := time.Date(2026, time.May, 16, 0, 0, 0, 0, time.Local)
	job := jobConfig{
		TableName:      "order",
		TimeColumn:     "created_time",
		TimeColumnType: TimeColumnTypeUnix,
	}
	var row struct {
		MinTime int64 `gorm:"column:min_time"` // MinTime 承接 DryRun 聚合字段
	}
	tx := buildMinArchivableTimeQuery(context.Background(), db, job, upperBound).Find(&row)
	if tx.Error != nil {
		t.Fatalf("buildMinArchivableTimeQuery(unix) Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT MIN(`created_time`) AS min_time FROM `order`",
		"WHERE `created_time` < ? AND `created_time` > ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 2 || tx.Statement.Vars[0] != upperBound.Unix() || tx.Statement.Vars[1] != int64(0) {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
}

// TestBuildStringTimeHistoryMatchQueryUsesGORMChain 验证 string 冷热表一致性校验不再使用 Raw SQL。
func TestBuildStringTimeHistoryMatchQueryUsesGORMChain(t *testing.T) {
	db := newArchiveDryRunDB(t)
	job := jobConfig{
		TableName:  "admin_log",
		PrimaryKey: "id",
		TimeColumn: "created_at",
	}
	var count int64
	tx := buildStringTimeHistoryMatchQuery(context.Background(), db, job, "admin_log_archive_202605", []int64{1, 2}).Count(&count)
	if tx.Error != nil {
		t.Fatalf("buildStringTimeHistoryMatchQuery Count() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"FROM `admin_log` AS hot",
		"JOIN `admin_log_archive_202605` AS history ON history.`id` = hot.`id`",
		"hot.`id` IN",
		"history.`created_at` <> hot.`created_at`",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) == 0 {
		t.Fatalf("expected id vars, got none")
	}
}

// TestBuildHistoryCleanupItemsQueryUsesGORMChain 验证历史表保留策略查询由 GORM 生成，并只选择全量 deleted 的历史表。
func TestBuildHistoryCleanupItemsQueryUsesGORMChain(t *testing.T) {
	db := newArchiveDryRunDB(t)
	job := jobConfig{Name: "admin_log"}
	var rows []historyTableItem
	tx := buildHistoryCleanupItemsQuery(context.Background(), db, job).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("buildHistoryCleanupItemsQuery Find() error = %v", tx.Error)
	}
	sqlText := tx.Statement.SQL.String()
	for _, want := range []string{
		"SELECT `history_table_name` AS history_table_name, MIN(`range_start`) AS first_range_start FROM `archive_segment`",
		"`job_name` = ?",
		"`history_table_name` <> ?",
		"GROUP BY `history_table_name`",
		"HAVING SUM(CASE WHEN `status` <> ? THEN 1 ELSE 0 END) = 0",
		"ORDER BY `first_range_start` DESC",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("expected %q in SQL: %s", want, sqlText)
		}
	}
	if len(tx.Statement.Vars) != 3 {
		t.Fatalf("unexpected vars: %#v", tx.Statement.Vars)
	}
}

// TestArchiveSQLTemplates 验证归档批次复制和历史表 DDL 模板只替换受控标识符。
func TestArchiveSQLTemplates(t *testing.T) {
	job := jobConfig{
		TableName:        "admin_log",
		PrimaryKey:       "id",
		TimeColumn:       "created_at",
		ArchiveCondition: "status = 1",
	}
	segment := &Segment{HistoryTableName: "admin_log_archive_202605"}
	insertSQL := archiveBatchInsertSQL(job, segment, "`id`, `created_at`, `status`")
	for _, want := range []string{
		"INSERT IGNORE INTO `admin_log_archive_202605`",
		"(`id`, `created_at`, `status`)",
		"SELECT `id`, `created_at`, `status`",
		"FROM `admin_log`",
		"WHERE `id` IN ?",
		"AND `created_at` >= ?",
		"AND `created_at` < ? AND (status = 1)",
	} {
		if !strings.Contains(insertSQL, want) {
			t.Fatalf("expected %q in archive batch SQL: %s", want, insertSQL)
		}
	}
	if !strings.Contains(archiveDropHistoryTableTemplate, "代码资产") {
		t.Fatalf("archive drop template should keep asset header: %s", archiveDropHistoryTableTemplate)
	}
	if got := archiveDropHistoryTableSQL("admin_log_archive_202605"); !strings.Contains(got, "DROP TABLE IF EXISTS `admin_log_archive_202605`") || strings.Contains(got, "代码资产") {
		t.Fatalf("archiveDropHistoryTableSQL() = %s", got)
	}
	if !strings.Contains(archiveCreateHistoryTableTemplate, "代码资产") {
		t.Fatalf("archive create template should keep asset header: %s", archiveCreateHistoryTableTemplate)
	}
	if got := archiveCreateHistoryTableSQL("admin_log_archive_202605", "admin_log"); !strings.Contains(got, "CREATE TABLE IF NOT EXISTS `admin_log_archive_202605` LIKE `admin_log`") || strings.Contains(got, "代码资产") {
		t.Fatalf("archiveCreateHistoryTableSQL() = %s", got)
	}
}

// TestNormalizeArchiveSQLConditionRejectsUnsafeFragments 验证自定义条件只能表达安全谓词片段。
func TestNormalizeArchiveSQLConditionRejectsUnsafeFragments(t *testing.T) {
	cases := []string{
		"status = 1; DROP TABLE admin_log",
		"id IN (SELECT id FROM other_table)",
		"status = ?",
		"name = 'abc",
	}
	for _, item := range cases {
		if _, err := normalizeArchiveSQLCondition(item); err == nil {
			t.Fatalf("normalizeArchiveSQLCondition(%q) expected error", item)
		}
	}
}

// TestNormalizeArchiveSQLConditionAllowsKeywordsInsideStrings 验证业务枚举值里的 SQL 关键字不会被误判。
func TestNormalizeArchiveSQLConditionAllowsKeywordsInsideStrings(t *testing.T) {
	got, err := normalizeArchiveSQLCondition("action = 'delete' AND remark <> \"select\"")
	if err != nil {
		t.Fatalf("normalizeArchiveSQLCondition() error = %v", err)
	}
	if got != "action = 'delete' AND remark <> \"select\"" {
		t.Fatalf("normalizeArchiveSQLCondition() = %s", got)
	}
}

// TestNormalizedJobsCapsBatchSizes 验证归档批次配置会被夹到安全上限，避免生成过大的 IN 条件和事务。
func TestNormalizedJobsCapsBatchSizes(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:            "admin_log",
					Enabled:         true,
					Database:        string(svc.DatabaseMain),
					TableName:       "admin_log",
					BatchSize:       maxArchiveBatchSize + 1,
					DeleteBatchSize: maxArchiveBatchSize + 2,
				},
			},
		},
	}, svc.Dependencies{})

	jobs := NewService(svcCtx).normalizedJobs()
	if len(jobs) != 1 {
		t.Fatalf("normalizedJobs() len = %d, want 1", len(jobs))
	}
	if jobs[0].BatchSize != maxArchiveBatchSize {
		t.Fatalf("BatchSize = %d, want %d", jobs[0].BatchSize, maxArchiveBatchSize)
	}
	if jobs[0].DeleteBatchSize != maxArchiveBatchSize {
		t.Fatalf("DeleteBatchSize = %d, want %d", jobs[0].DeleteBatchSize, maxArchiveBatchSize)
	}
}

// TestArchiveIndexHasTimePrefix 验证归档访问路径只接受时间列左前缀索引。
func TestArchiveIndexHasTimePrefix(t *testing.T) {
	if !archiveIndexHasTimePrefix([]string{"created_at", "id"}, "created_at") {
		t.Fatal("期望 created_at 左前缀索引可用于归档窗口扫描")
	}
	if !archiveIndexHasTimePrefix([]string{"created_at"}, "created_at") {
		t.Fatal("期望 created_at 单列索引可用于归档窗口扫描")
	}
	if archiveIndexHasTimePrefix([]string{"id", "created_at"}, "created_at") {
		t.Fatal("id 在前的索引不能支撑按 created_at 窗口扫描")
	}
}

// TestArchiveBatchDelayBounds 验证归档批次保护性等待会使用默认值并受硬上限保护。
func TestArchiveBatchDelayBounds(t *testing.T) {
	defaultService := NewService(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	if got := defaultService.batchDelay(); got != defaultBatchDelay {
		t.Fatalf("batchDelay() = %s, want %s", got, defaultBatchDelay)
	}

	cappedService := NewService(svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			BatchDelayMilliseconds: int((maxBatchDelay + time.Second) / time.Millisecond),
		},
	}, svc.Dependencies{}))
	if got := cappedService.batchDelay(); got != maxBatchDelay {
		t.Fatalf("batchDelay() = %s, want %s", got, maxBatchDelay)
	}
}

// TestResolveTargetsDeduplicatesAndIgnoresUnknown 验证归档目标过滤会去重、忽略未知目标，并保持已启用任务的稳定顺序。
func TestResolveTargetsDeduplicatesAndIgnoresUnknown(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:      "admin_log",
					Enabled:   true,
					Database:  string(svc.DatabaseMain),
					TableName: "admin_log",
				},
				{
					Name:      "admin_message",
					Enabled:   true,
					Database:  string(svc.DatabaseMain),
					TableName: "admin_message",
				},
				{
					Name:      "disabled_job",
					Enabled:   false,
					Database:  string(svc.DatabaseMain),
					TableName: "disabled_job",
				},
			},
		},
	}, svc.Dependencies{})

	runs := NewService(svcCtx).resolveTargets([]string{
		"admin_message",
		"unknown_job",
		"admin_log",
		"admin_message",
		" ",
	})
	if len(runs) != 2 {
		t.Fatalf("resolveTargets() len = %d, want 2", len(runs))
	}
	if runs[0].Job.Name != "admin_log" || runs[1].Job.Name != "admin_message" {
		t.Fatalf("期望返回稳定排序后的已启用目标，实际为 %#v", runs)
	}
	if runs[0].Mode != archiveRunModeAll || runs[1].Mode != archiveRunModeAll {
		t.Fatalf("期望无后缀 target 兼容执行 all，实际为 %#v", runs)
	}
}

// TestResolveTargetsSupportsModeSuffix 验证 target 后缀可以把归档和删除拆成不同周期任务。
func TestResolveTargetsSupportsModeSuffix(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:      "admin_log",
					Enabled:   true,
					Database:  string(svc.DatabaseMain),
					TableName: "admin_log",
				},
			},
		},
	}, svc.Dependencies{})

	runs := NewService(svcCtx).resolveTargets([]string{"admin_log#delete"})
	if len(runs) != 1 {
		t.Fatalf("resolveTargets() len = %d, want 1", len(runs))
	}
	if runs[0].Mode != archiveRunModeDelete {
		t.Fatalf("resolveTargets()[0].Mode = %s, want %s", runs[0].Mode, archiveRunModeDelete)
	}

	mergedRuns := NewService(svcCtx).resolveTargets([]string{"admin_log#archive", "admin_log#delete"})
	if len(mergedRuns) != 1 || mergedRuns[0].Mode != archiveRunModeAll {
		t.Fatalf("期望同一 job 的 archive/delete 后缀合并为 all，实际为 %#v", mergedRuns)
	}
}

// TestAdminLogQueryJobFallbackToHotTable 验证管理员日志查询在未配置归档 job 时会回退到热表配置。
func TestAdminLogQueryJobFallbackToHotTable(t *testing.T) {
	service := NewService(svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	job := service.adminLogQueryJob()
	if job.Name != JobNameAdminLog {
		t.Fatalf("adminLogQueryJob().Name = %s, want %s", job.Name, JobNameAdminLog)
	}
	if job.Database != svc.DatabaseMain {
		t.Fatalf("adminLogQueryJob().Database = %s, want %s", job.Database, svc.DatabaseMain)
	}
	if job.TableName != model.TableNameAdminLog {
		t.Fatalf("adminLogQueryJob().TableName = %s, want %s", job.TableName, model.TableNameAdminLog)
	}
}

// TestAdminLogQueryJobUsesConfiguredJob 验证管理员日志查询仍优先复用已配置的归档 job 热表信息。
func TestAdminLogQueryJobUsesConfiguredJob(t *testing.T) {
	service := NewService(svc.NewServiceContext(config.Config{
		Archive: config.ArchiveConfig{
			Jobs: []config.ArchiveJobConfig{
				{
					Name:         JobNameAdminLog,
					Enabled:      true,
					Database:     string(svc.DatabaseMain),
					TableName:    model.TableNameAdminLog,
					QueryWriteDB: true,
				},
			},
		},
	}, svc.Dependencies{}))
	job := service.adminLogQueryJob()
	if !job.QueryWriteDB {
		t.Fatalf("adminLogQueryJob().QueryWriteDB = false, want true")
	}
	if job.TableName != model.TableNameAdminLog {
		t.Fatalf("adminLogQueryJob().TableName = %s, want %s", job.TableName, model.TableNameAdminLog)
	}
}

// TestBuildAdminLogSubQueryUsesTemplate 验证管理员日志历史子查询由模板生成，并保持参数绑定顺序稳定。
func TestBuildAdminLogSubQueryUsesTemplate(t *testing.T) {
	uid := 1001
	startTime := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.Local)
	endTime := time.Date(2026, time.May, 2, 0, 0, 0, 0, time.Local)
	lowerBound := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.Local)
	upperBound := time.Date(2026, time.May, 1, 13, 0, 0, 0, time.Local)
	sqlText, args := buildAdminLogSubQuery("admin_log_archive_202605", &types.AdminLogQueryReq{
		TraceID: "trace-001",
		UserID:  &uid,
		Action:  "查询管理员操作日志",
	}, &startTime, &endTime, &lowerBound, &upperBound)

	for _, want := range []string{
		"SELECT id, user_id, user_name",
		"FROM `admin_log_archive_202605`",
		"trace_id = ?",
		"user_id = ?",
		"action = ?",
		"created_at >= ?",
		"created_at <= ?",
		"created_at < ?",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("期望 SQL 包含 %q，实际为 %s", want, sqlText)
		}
	}
	if strings.Contains(sqlText, "user_name = ?") {
		t.Fatalf("未传用户名时不应生成 user_name 条件，实际为 %s", sqlText)
	}
	if len(args) != 7 {
		t.Fatalf("参数数量 = %d, want 7, args=%v", len(args), args)
	}
	if args[0] != "trace-001" || args[1] != uid || args[2] != "查询管理员操作日志" {
		t.Fatalf("业务筛选参数顺序不正确: %v", args[:3])
	}
	if args[3] != startTime || args[4] != endTime || args[5] != lowerBound || args[6] != upperBound {
		t.Fatalf("时间边界参数顺序不正确: %v", args[3:])
	}
}
