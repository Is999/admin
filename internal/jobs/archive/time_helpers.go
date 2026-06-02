package archive

import (
	"fmt"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// buildHistoryTableName 根据命名模板或拆分粒度生成目标历史表名。
func buildHistoryTableName(job jobConfig, segmentStart time.Time, segmentEnd time.Time) string {
	prefix := strings.TrimSpace(job.HistoryTablePrefix)
	if prefix == "" {
		prefix = fmt.Sprintf("%s_archive", job.TableName)
	}
	if strings.TrimSpace(job.HistoryTableNameRule) != "" {
		return renderHistoryTableNameRule(job.HistoryTableNameRule, prefix, job.TableName, segmentStart, segmentEnd)
	}
	switch job.SplitUnit {
	case SplitUnitNone:
		return prefix
	case SplitUnitYear:
		return fmt.Sprintf("%s_%04d", prefix, segmentStart.Year())
	case SplitUnitQuarter:
		return fmt.Sprintf("%s_%04dq%d", prefix, segmentStart.Year(), quarterOf(segmentStart))
	case SplitUnitWeek:
		year, week := segmentStart.ISOWeek()
		return fmt.Sprintf("%s_%04dw%02d", prefix, year, week)
	case SplitUnitDay:
		return fmt.Sprintf("%s_%s", prefix, segmentStart.Format("20060102"))
	case SplitUnitCustomDays:
		return fmt.Sprintf("%s_%s", prefix, segmentStart.Format("20060102"))
	default:
		return fmt.Sprintf("%s_%s", prefix, segmentStart.Format("200601"))
	}
}

// renderHistoryTableNameRule 渲染历史表命名模板。
// 模板只做固定占位替换，输出仍会在规划区间时按 MySQL 标识符规则校验，避免动态表名注入。
func renderHistoryTableNameRule(rule string, prefix string, tableName string, segmentStart time.Time, segmentEnd time.Time) string {
	replacements := map[string]string{
		"{prefix}":      prefix,
		"{table}":       tableName,
		"{yyyy}":        segmentStart.Format("2006"),
		"{yyyymm}":      segmentStart.Format("200601"),
		"{yyyymmdd}":    segmentStart.Format("20060102"),
		"{range_start}": segmentStart.Format("20060102150405"),
		"{range_end}":   segmentEnd.Format("20060102150405"),
		"{quarter}":     fmt.Sprintf("q%d", quarterOf(segmentStart)),
	}
	year, week := segmentStart.ISOWeek()
	replacements["{isoweek}"] = fmt.Sprintf("%04dw%02d", year, week)
	result := strings.TrimSpace(rule)
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
	}
	return result
}

// alignInitialArchiveCursor 返回首次规划的归档区间起点。
func alignInitialArchiveCursor(t time.Time, job jobConfig) time.Time {
	if job.hasArchiveSecondWindow() {
		// 窗口化任务按窗口边界起步，避免首条数据在月中时从月初跑大量空区间。
		return alignWindowEnd(t, job.ArchiveWindowSeconds)
	}
	return alignSegmentStart(t, job.SplitUnit, job.CustomDays)
}

// alignSegmentStart 把任意时间对齐到当前拆分粒度的区间起点。
func alignSegmentStart(t time.Time, splitUnit string, customDays int) time.Time {
	switch splitUnit {
	case SplitUnitNone:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	case SplitUnitYear:
		return time.Date(t.Year(), time.January, 1, 0, 0, 0, 0, t.Location())
	case SplitUnitQuarter:
		month := ((int(t.Month())-1)/3)*3 + 1
		return time.Date(t.Year(), time.Month(month), 1, 0, 0, 0, 0, t.Location())
	case SplitUnitWeek:
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := startOfDay(t).AddDate(0, 0, -(weekday - 1))
		return start
	case SplitUnitDay:
		return startOfDay(t)
	case SplitUnitCustomDays:
		base := time.Date(1970, time.January, 1, 0, 0, 0, 0, t.Location())
		days := positiveOr(customDays, 90)
		diff := int(startOfDay(t).Sub(base).Hours() / 24)
		offset := diff - diff%days
		return base.AddDate(0, 0, offset)
	default:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	}
}

// nextSegmentBoundary 返回当前区间起点对应的下一个排他边界。
func nextSegmentBoundary(start time.Time, splitUnit string, customDays int) time.Time {
	switch splitUnit {
	case SplitUnitNone:
		return start.AddDate(0, 1, 0)
	case SplitUnitYear:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(1, 0, 0)
	case SplitUnitQuarter:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(0, 3, 0)
	case SplitUnitWeek:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(0, 0, 7)
	case SplitUnitDay:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(0, 0, 1)
	case SplitUnitCustomDays:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(0, 0, positiveOr(customDays, 90))
	default:
		return alignSegmentStart(start, splitUnit, customDays).AddDate(0, 1, 0)
	}
}

// nextArchiveSegmentBoundary 返回当前归档区间起点的下一个排他边界。
// 配置 archive_window_seconds 时，区间粒度由调度窗口决定；否则沿用历史表拆分粒度作为区间粒度。
func nextArchiveSegmentBoundary(start time.Time, job jobConfig) time.Time {
	if job.hasArchiveSecondWindow() {
		next := start.Add(time.Duration(job.ArchiveWindowSeconds) * time.Second)
		if job.SplitUnit != SplitUnitNone {
			// 窗口化归档必须受历史表拆分边界约束。
			splitStart := alignSegmentStart(start, job.SplitUnit, job.CustomDays)
			splitEnd := nextSegmentBoundary(splitStart, job.SplitUnit, job.CustomDays)
			if splitEnd.After(start) && next.After(splitEnd) {
				return splitEnd
			}
		}
		return next
	}
	return nextSegmentBoundary(start, job.SplitUnit, job.CustomDays)
}

// alignWindowEnd 将时间对齐到窗口结束边界。
// 例如 10:17 按 15 分钟对齐为 10:15，用于“每 15 分钟处理 N 天前对应 15 分钟窗口”。
func alignWindowEnd(t time.Time, windowSeconds int) time.Time {
	if windowSeconds <= 0 {
		return t
	}
	unix := t.Unix()
	aligned := unix - unix%int64(windowSeconds)
	return time.Unix(aligned, 0).In(t.Location())
}

// normalizeArchiveWindowSeconds 校验并裁剪归档/删除窗口秒数。
func normalizeArchiveWindowSeconds(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	if seconds > maxArchiveWindowSeconds {
		return maxArchiveWindowSeconds
	}
	return seconds
}

// capArchiveWindowsPerRun 裁剪单轮最多窗口数，避免误配置让一个周期任务吞掉过多历史区间。
func capArchiveWindowsPerRun(value int) int {
	if value <= 0 {
		return 0
	}
	if value > maxArchiveWindowsPerRun {
		return maxArchiveWindowsPerRun
	}
	return value
}

// normalizeArchiveWindowMode 标准化归档窗口推进模式，未配置时默认自动追赶稀疏窗口。
func normalizeArchiveWindowMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ArchiveWindowModeAuto:
		return ArchiveWindowModeAuto
	case ArchiveWindowModeFixed:
		return ArchiveWindowModeFixed
	default:
		return ""
	}
}

// normalizeArchiveAutoMaxWindows 归一化 auto 模式单轮追赶窗口上限。
func normalizeArchiveAutoMaxWindows(job jobConfig) int {
	if job.ArchiveWindowMode != ArchiveWindowModeAuto || (!job.hasArchiveSecondWindow() && !job.hasDeleteSecondWindow()) {
		return job.ArchiveMaxWindowsPerRun
	}
	limit := job.ArchiveAutoMaxWindows
	if limit <= 0 {
		limit = defaultArchiveAutoMaxWindows
	}
	baseLimit := job.ArchiveMaxWindowsPerRun
	if job.hasDeleteSecondWindow() && job.DeleteMaxWindowsPerRun > baseLimit {
		baseLimit = job.DeleteMaxWindowsPerRun
	}
	if baseLimit > 0 && limit < baseLimit {
		limit = baseLimit
	}
	return capArchiveWindowsPerRun(limit)
}

// normalizeArchiveAutoLightRows 归一化 auto 模式轻量窗口行数阈值。
func normalizeArchiveAutoLightRows(value int) int {
	if value <= 0 {
		return defaultArchiveAutoLightRows
	}
	if value > maxArchiveBatchSize {
		return maxArchiveBatchSize
	}
	return value
}

// normalizeArchiveAutoLightElapsed 归一化 auto 模式轻量窗口耗时阈值。
func normalizeArchiveAutoLightElapsed(milliseconds int) time.Duration {
	if milliseconds <= 0 {
		return defaultArchiveAutoLightElapsed
	}
	elapsed := time.Duration(milliseconds) * time.Millisecond
	if elapsed > maxArchiveAutoLightElapsed {
		return maxArchiveAutoLightElapsed
	}
	return elapsed
}

// archivePlanWindowLimit 返回本轮最多预规划窗口数；auto 模式会多规划一些待追赶窗口。
func archivePlanWindowLimit(job jobConfig) int {
	if job.ArchiveWindowMode == ArchiveWindowModeAuto && job.hasArchiveSecondWindow() {
		return positiveOr(job.ArchiveAutoMaxWindows, job.ArchiveMaxWindowsPerRun)
	}
	return job.ArchiveMaxWindowsPerRun
}

// shouldContinueArchiveRun 判断当前 worker 是否继续领取归档窗口。
func shouldContinueArchiveRun(job jobConfig, processed int, lastResult *archiveSegmentResult) bool {
	baseLimit := job.ArchiveMaxWindowsPerRun
	if baseLimit <= 0 {
		return true
	}
	if processed < baseLimit {
		return true
	}
	if job.ArchiveWindowMode != ArchiveWindowModeAuto || !job.hasArchiveSecondWindow() {
		return false
	}
	autoLimit := positiveOr(job.ArchiveAutoMaxWindows, baseLimit)
	if processed >= autoLimit || lastResult == nil {
		return false
	}
	return isLightArchiveSegment(job, *lastResult)
}

// isLightArchiveSegment 判断窗口是否足够轻量，auto 模式只有轻量窗口才继续追赶水位。
func isLightArchiveSegment(job jobConfig, result archiveSegmentResult) bool {
	lightRows := positiveOr(job.ArchiveAutoLightRows, defaultArchiveAutoLightRows)
	lightElapsed := job.ArchiveAutoLightElapsed
	if lightElapsed <= 0 {
		lightElapsed = defaultArchiveAutoLightElapsed
	}
	return result.RowsArchived <= int64(lightRows) && result.Elapsed <= lightElapsed
}

// shouldContinueDeleteRun 判断当前 worker 是否继续领取删除窗口。
// 删除复用归档 auto 规则，让历史空窗口和稀疏窗口能快速追赶，遇到重窗口仍回到基础限额。
func shouldContinueDeleteRun(job jobConfig, processed int, lastResult *deleteSegmentResult) bool {
	baseLimit := job.DeleteMaxWindowsPerRun
	if baseLimit <= 0 {
		return true
	}
	if processed < baseLimit {
		return true
	}
	if job.ArchiveWindowMode != ArchiveWindowModeAuto || !job.hasDeleteSecondWindow() {
		return false
	}
	autoLimit := positiveOr(job.ArchiveAutoMaxWindows, baseLimit)
	if processed >= autoLimit || lastResult == nil {
		return false
	}
	return isLightDeleteSegment(job, *lastResult)
}

// isLightDeleteSegment 判断删除窗口是否足够轻量，避免 auto 模式连续清理重窗口。
func isLightDeleteSegment(job jobConfig, result deleteSegmentResult) bool {
	if !result.Progressed {
		return false
	}
	lightRows := positiveOr(job.ArchiveAutoLightRows, defaultArchiveAutoLightRows)
	deleteBatchSize := positiveOr(job.DeleteBatchSize, defaultDeleteBatchSize)
	if lightRows > deleteBatchSize {
		lightRows = deleteBatchSize
	}
	lightElapsed := job.ArchiveAutoLightElapsed
	if lightElapsed <= 0 {
		lightElapsed = defaultArchiveAutoLightElapsed
	}
	return result.RowsDeleted <= int64(lightRows) && result.Elapsed <= lightElapsed
}

// startOfDay 返回当前时区下当天零点。
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// parseArchiveStringTime 按配置 layout 严格解析字符串时间列。
func parseArchiveStringTime(value string, layout string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("归档字符串时间不能为空")
	}
	layout = archiveStringTimeFormat(layout)
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "归档字符串时间格式非法: layout=%s value=%s", layout, value)
	}
	if parsed.Format(layout) != value {
		return time.Time{}, errors.Errorf("归档字符串时间非法: layout=%s value=%s", layout, value)
	}
	return parsed, nil
}

// parseArchiveUnixTime 解析 Unix int64 时间列，拒绝非正值避免归档窗口落到无效历史区间。
func parseArchiveUnixTime(value int64, unit string) (time.Time, error) {
	if value <= 0 {
		return time.Time{}, errors.Errorf("归档 Unix 时间必须大于 0: value=%d", value)
	}
	switch unit {
	case TimeColumnUnixUnitMilliseconds:
		return time.UnixMilli(value).In(time.Local), nil
	case TimeColumnUnixUnitSeconds, "":
		return time.Unix(value, 0).In(time.Local), nil
	default:
		return time.Time{}, errors.Errorf("归档 Unix 时间单位非法: unit=%s", unit)
	}
}

// parseArchiveStartAt 解析首次归档起点配置，支持日期或完整时间格式。
func parseArchiveStartAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("归档起点不能为空")
	}
	if len(value) == len("2006-01-02") {
		return parseArchiveStringTime(value, "2006-01-02")
	}
	parsed, err := time.ParseInLocation(time.DateTime, value, time.Local)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "归档起点格式非法: value=%s", value)
	}
	return parsed, nil
}

// formatArchiveStringTimeArg 把时间边界格式化为字符串时间列的查询参数。
func formatArchiveStringTimeArg(job jobConfig, value time.Time) string {
	return value.Format(archiveStringTimeFormat(job.TimeColumnFormat))
}

// archiveTimeArg 按时间列类型生成查询参数，保证数字时间列不会绑定 DATETIME。
func archiveTimeArg(job jobConfig, value time.Time) any {
	if job.isStringTimeColumn() {
		return formatArchiveStringTimeArg(job, value)
	}
	if job.isUnixSecondsTimeColumn() {
		return archiveUnixTimeArg(job, value)
	}
	return value
}

// archiveUnixTimeArg 把时间边界转换为 Unix int64 查询参数。
func archiveUnixTimeArg(job jobConfig, value time.Time) int64 {
	if job.TimeColumnUnixUnit == TimeColumnUnixUnitMilliseconds {
		return value.UnixMilli()
	}
	return value.Unix()
}

// archiveStringTimeFormat 返回字符串时间列的有效 Go layout。
func archiveStringTimeFormat(layout string) string {
	layout = strings.TrimSpace(layout)
	if layout == "" {
		return defaultArchiveStringTimeLayout
	}
	return layout
}

// validateArchiveStringTimeFormat 校验字符串时间列 layout 至少能表达日期并可反向解析。
func validateArchiveStringTimeFormat(layout string) error {
	layout = archiveStringTimeFormat(layout)
	probe := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.Local)
	parsed, err := time.ParseInLocation(layout, probe.Format(layout), time.Local)
	if err != nil {
		return errors.Wrapf(err, "字符串时间格式非法: layout=%s", layout)
	}
	if parsed.Year() != probe.Year() || parsed.Month() != probe.Month() || parsed.Day() != probe.Day() {
		return errors.Errorf("字符串时间格式必须包含可解析的年月日: layout=%s", layout)
	}
	return nil
}

// archiveStringTimeFormatHasClock 判断字符串时间格式是否包含时分秒粒度。
func archiveStringTimeFormatHasClock(layout string) bool {
	layout = archiveStringTimeFormat(layout)
	probe := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.Local)
	parsed, err := time.ParseInLocation(layout, probe.Format(layout), time.Local)
	if err != nil {
		return false
	}
	return parsed.Hour() == probe.Hour() || parsed.Minute() == probe.Minute() || parsed.Second() == probe.Second()
}

// quarterOf 返回时间点所在季度编号，范围为 1~4。
func quarterOf(t time.Time) int {
	return (int(t.Month())-1)/3 + 1
}
