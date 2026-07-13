package archive

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

// archiveBatch 分批复制当前区间数据，并在每个子批次提交后推进 checkpoint。
func (s *Service) archiveBatch(ctx context.Context, sourceDB *gorm.DB, controlDB *gorm.DB, job jobConfig, segment *Segment, rows []batchCursorRow, workerID string) error {
	if len(rows) == 0 {
		return nil
	}
	columnList, err := archiveTableColumnList(ctx, sourceDB, job.TableName)
	if err != nil {
		return errors.Tag(err)
	}
	chunkSize := job.BatchSize
	if chunkSize <= 0 {
		chunkSize = len(rows)
	}
	for start := 0; start < len(rows); start += chunkSize {
		end := start + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		ids := make([]int64, 0, len(chunk))
		for _, row := range chunk {
			ids = append(ids, row.ID)
		}
		lastRow := chunk[len(chunk)-1]
		if err := s.archiveBatchChunk(ctx, sourceDB, job, segment, ids, columnList); err != nil {
			return errors.Tag(err)
		}
		if err := s.updateSegmentCheckpoint(ctx, controlDB, segment, len(ids), lastRow.CreatedAt, lastRow.ID, workerID); err != nil {
			return errors.Tag(err)
		}
		segment.LastArchivedID = lastRow.ID
		segment.LastArchivedTime = sql.NullTime{Time: lastRow.CreatedAt, Valid: true}
		segment.RowsArchived += int64(len(ids))
		if end < len(rows) {
			// 子批次已独立提交并推进 checkpoint 后再限速，避免小事务连续写入历史表压垮 MySQL。
			if err := waitArchiveBatch(ctx, s.batchDelay()); err != nil {
				return errors.Tag(err)
			}
		}
	}
	return nil
}

// archiveBatchChunk 在源库事务内完成单个子批次的复制和校验。
// 该事务只覆盖当前批次主键集合，降低千万级历史归档时的行锁持有范围。
func (s *Service) archiveBatchChunk(ctx context.Context, sourceDB *gorm.DB, job jobConfig, segment *Segment, ids []int64, columnList string) error {
	if len(ids) == 0 {
		return nil
	}
	if segment == nil {
		return errors.Errorf("归档区间为空: job=%s table=%s", job.Name, job.TableName)
	}
	return sourceDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		// 历史表写入使用 INSERT IGNORE，依赖主键天然防重，支持任务重试和补跑。
		// 时间窗口和自定义归档条件在复制事务内再次校验，避免游标读取后数据状态变化导致越界行进入历史表。
		insertSQL := archiveBatchInsertSQL(job, segment, columnList)
		insertArgs := append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)
		if err := tx.Exec(insertSQL, insertArgs...).Error; err != nil {
			return errors.Tag(err)
		}

		var copiedCount int64
		if err := tx.Table(segment.HistoryTableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)...).
			Count(&copiedCount).Error; err != nil {
			return errors.Tag(err)
		}
		if copiedCount != int64(len(ids)) {
			return errors.Errorf("历史表校验失败: table=%s want=%d got=%d", segment.HistoryTableName, len(ids), copiedCount)
		}
		if job.isStringTimeColumn() {
			if err := s.ensureStringTimeHistoryMatchesHot(ctx, tx, job, segment.HistoryTableName, ids); err != nil {
				return errors.Tag(err)
			}
		}

		return nil
	})
}

// ensureHistoryTable 确保当前归档区间对应的历史表已经创建。
func (s *Service) ensureHistoryTable(ctx context.Context, db *gorm.DB, job jobConfig, historyTable string) error {
	if tableExists(ctx, db, historyTable) {
		return nil
	}
	return db.WithContext(ctx).Exec(archiveCreateHistoryTableSQL(historyTable, job.TableName)).Error
}

// ensureArchiveAccessPath 校验热表是否具备按时间列增序扫描的索引访问路径。
// 归档和删除依赖 time_column 半开窗口索引，缺少左前缀索引时失败。
func (s *Service) ensureArchiveAccessPath(ctx context.Context, db *gorm.DB, job jobConfig) error {
	if db == nil {
		return errors.Errorf("归档源库连接为空: job=%s database=%s", job.Name, job.Database)
	}
	indexes, err := db.WithContext(ctx).Migrator().GetIndexes(job.TableName)
	if err != nil {
		return errors.Tag(err)
	}
	for _, index := range indexes {
		if archiveIndexHasTimePrefix(index.Columns(), job.TimeColumn) {
			return nil
		}
	}
	return errors.Errorf("归档热表缺少时间列左前缀索引，请先由 DBA 补充生产索引: job=%s table=%s recommended_index=(%s,%s)",
		job.Name, job.TableName, job.TimeColumn, job.PrimaryKey)
}

// loadBatchCursorRows 读取当前区间下一批待归档记录的游标信息。
// 排序固定为“时间列升序 + 主键升序”，便于稳定断点续跑。
func (s *Service) loadBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	if job.isStringTimeColumn() {
		return s.loadStringTimeBatchCursorRows(ctx, db, job, segment)
	}
	if job.isUnixSecondsTimeColumn() {
		return s.loadUnixSecondsBatchCursorRows(ctx, db, job, segment)
	}
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	rows := make([]batchCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// loadUnixSecondsBatchCursorRows 读取 Unix int64 时间列的批次游标，并转换为控制表时间。
func (s *Service) loadUnixSecondsBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	type unixSecondsCursorRow struct {
		ID        int64 `gorm:"column:id"`         // 当前批次主键
		CreatedAt int64 `gorm:"column:created_at"` // 当前批次 Unix int64 时间
	}
	rawRows := make([]unixSecondsCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rawRows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	rows := make([]batchCursorRow, 0, len(rawRows))
	for _, raw := range rawRows {
		createdAt, err := parseArchiveUnixTime(raw.CreatedAt, job.TimeColumnUnixUnit)
		if err != nil {
			return nil, errors.Wrapf(err, "归档 Unix 时间非法: table=%s column=%s unit=%s id=%d value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, raw.ID, raw.CreatedAt)
		}
		rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
	}
	return rows, nil
}

// loadStringTimeBatchCursorRows 读取字符串时间列的批次游标，并按配置格式完成严格校验。
func (s *Service) loadStringTimeBatchCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment) ([]batchCursorRow, error) {
	batchSize := job.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	type dateStringCursorRow struct {
		ID        int64  `gorm:"column:id"`         // 当前批次主键
		CreatedAt string `gorm:"column:created_at"` // 当前批次字符串时间
	}
	rawRows := make([]dateStringCursorRow, 0, batchSize)
	if err := buildLoadBatchCursorRowsQuery(ctx, db, job, segment, batchSize).Scan(&rawRows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	rows := make([]batchCursorRow, 0, len(rawRows))
	for _, raw := range rawRows {
		createdAt, err := parseArchiveStringTime(raw.CreatedAt, job.TimeColumnFormat)
		if err != nil {
			return nil, errors.Wrapf(err, "归档字符串时间非法: table=%s column=%s id=%d value=%s", job.TableName, job.TimeColumn, raw.ID, raw.CreatedAt)
		}
		rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
	}
	return rows, nil
}

// buildLoadBatchCursorRowsQuery 构造归档游标读取查询。
// 查询按时间半开窗口和主键断点稳定排序，使用 GORM clause 表达式避免在 Go 文件中拼完整 SELECT SQL。
func buildLoadBatchCursorRowsQuery(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, batchSize int) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("? AS id, ? AS created_at", clause.Column{Name: job.PrimaryKey}, clause.Column{Name: job.TimeColumn})
	query = applyArchiveTimeRangeWhere(query, job, segment.RangeStart, segment.RangeEnd)
	if condition := archiveConditionSQL(job); condition != "" {
		// 自定义归档条件只影响当前 job 的待归档行集合，时间窗口仍作为主索引边界保证扫描可控。
		query = query.Where("(" + condition + ")")
	}
	if segment.LastArchivedTime.Valid {
		query = applyArchiveCursorResumeWhere(query, job, segment.LastArchivedTime.Time, segment.LastArchivedID)
	}
	return query.
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.TimeColumn}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.PrimaryKey}}).
		Limit(batchSize)
}

// applyArchiveTimeRangeWhere 按时间列类型追加半开时间窗口条件。
// 字符串和 Unix 整数时间列会先转换为对应存储格式，再绑定查询参数。
func applyArchiveTimeRangeWhere(query *gorm.DB, job jobConfig, rangeStart time.Time, rangeEnd time.Time) *gorm.DB {
	if job.isStringTimeColumn() {
		return query.Where(
			fmt.Sprintf("%s >= ? AND %s < ?", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn)),
			formatArchiveStringTimeArg(job, rangeStart),
			formatArchiveStringTimeArg(job, rangeEnd),
		)
	}
	return query.
		Where(clause.Gte{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, rangeStart)}).
		Where(clause.Lt{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, rangeEnd)})
}

// applyArchiveCursorResumeWhere 追加时间列和主键组成的断点续跑条件。
// 同一业务时间下用主键打破并列；特殊时间列类型需先转换为对应数据库字段类型。
func applyArchiveCursorResumeWhere(query *gorm.DB, job jobConfig, lastTime time.Time, lastID int64) *gorm.DB {
	if job.isStringTimeColumn() {
		lastDate := formatArchiveStringTimeArg(job, lastTime)
		return query.Where(
			fmt.Sprintf("%s > ? OR (%s = ? AND %s > ?)", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn), quoteIdent(job.PrimaryKey)),
			lastDate,
			lastDate,
			lastID,
		)
	}
	if job.isUnixSecondsTimeColumn() {
		lastUnix := archiveUnixTimeArg(job, lastTime)
		return query.Where(
			fmt.Sprintf("%s > ? OR (%s = ? AND %s > ?)", quoteIdent(job.TimeColumn), quoteIdent(job.TimeColumn), quoteIdent(job.PrimaryKey)),
			lastUnix,
			lastUnix,
			lastID,
		)
	}
	// 断点续跑时先比较时间列，再用主键打破同时间戳并列，避免重复或漏扫。
	resumeCondition := clause.Or(
		clause.Gt{Column: clause.Column{Name: job.TimeColumn}, Value: lastTime},
		clause.And(
			clause.Eq{Column: clause.Column{Name: job.TimeColumn}, Value: lastTime},
			clause.Gt{Column: clause.Column{Name: job.PrimaryKey}, Value: lastID},
		),
	)
	return query.Where(resumeCondition)
}

// minArchivableTime 返回热表中“早于归档上界”的最早时间点。
// 首次运行还没有 watermark 时，会以它为起点规划首个归档区间。
func (s *Service) minArchivableTime(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	if job.isStringTimeColumn() {
		return s.minArchivableStringTime(ctx, job, db, upperBound)
	}
	if job.isUnixSecondsTimeColumn() {
		return s.minArchivableUnixSeconds(ctx, job, db, upperBound)
	}
	// minArchivableTimeResult 承接 GORM 聚合结果；空值表示当前归档条件下没有可规划数据。
	type minArchivableTimeResult struct {
		MinTime sql.NullTime `gorm:"column:min_time"` // MinTime 表示满足时间上界和归档条件的最早业务时间
	}
	var row minArchivableTimeResult
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	return row.MinTime.Time, true, nil
}

// minArchivableUnixSeconds 返回 Unix int64 列中早于归档上界的最早业务时间。
func (s *Service) minArchivableUnixSeconds(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	type result struct {
		MinTime sql.NullInt64 `gorm:"column:min_time"` // min_time 表示最早 Unix int64 时间
	}
	var row result
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	minTime, err := parseArchiveUnixTime(row.MinTime.Int64, job.TimeColumnUnixUnit)
	if err != nil {
		return time.Time{}, false, errors.Wrapf(err, "归档最早 Unix 时间非法: table=%s column=%s unit=%s value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, row.MinTime.Int64)
	}
	return minTime, true, nil
}

// minArchivableStringTime 返回字符串时间列中早于归档上界的最早业务时间。
func (s *Service) minArchivableStringTime(ctx context.Context, job jobConfig, db *gorm.DB, upperBound time.Time) (time.Time, bool, error) {
	type result struct {
		MinTime sql.NullString `gorm:"column:min_time"` // min_time 表示最早字符串时间
	}
	var row result
	if err := buildMinArchivableTimeQuery(ctx, db, job, upperBound).Scan(&row).Error; err != nil {
		return time.Time{}, false, errors.Tag(err)
	}
	if !row.MinTime.Valid {
		return time.Time{}, false, nil
	}
	minTime, err := parseArchiveStringTime(row.MinTime.String, job.TimeColumnFormat)
	if err != nil {
		return time.Time{}, false, errors.Wrapf(err, "归档最早字符串时间非法: table=%s column=%s value=%s", job.TableName, job.TimeColumn, row.MinTime.String)
	}
	return minTime, true, nil
}

// buildMinArchivableTimeQuery 构造首次归档规划使用的最早时间查询。
// 查询使用 GORM 链式表达式承载 MIN 聚合、时间上界和自定义归档谓词，避免在业务代码中保留完整 Raw SQL。
func buildMinArchivableTimeQuery(ctx context.Context, db *gorm.DB, job jobConfig, upperBound time.Time) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("MIN(?) AS min_time", clause.Column{Name: job.TimeColumn}).
		Where(clause.Lt{Column: clause.Column{Name: job.TimeColumn}, Value: archiveTimeArg(job, upperBound)})
	if job.isUnixSecondsTimeColumn() {
		// Unix 整数列忽略 0 或负数，避免异常历史值把首个归档窗口拖到 1970。
		query = query.Where(clause.Gt{Column: clause.Column{Name: job.TimeColumn}, Value: int64(0)})
	}
	// 首次规划时只从满足归档条件的数据里寻找起点，避免条件过滤后的空表仍生成无意义区间。
	if condition := archiveConditionSQL(job); condition != "" {
		// 自定义归档条件已在配置加载阶段校验为 WHERE 谓词，这里作为 GORM 附加条件接入，避免回退 Raw SQL。
		query = query.Where("(" + condition + ")")
	}
	return query
}

// archiveUpperBound 计算当前安全可归档上界。
// 上界同时受归档延迟和 safe_time 约束，并对齐到窗口结束时间。
func (s *Service) archiveUpperBound(ctx context.Context, job jobConfig, controlDB *gorm.DB) (time.Time, bool) {
	now := time.Now()
	safeTime := now.Add(-time.Duration(s.safeDelayMinutes()) * time.Minute)

	upperBound := now.AddDate(0, 0, -job.ArchiveDelayDays)
	if job.isDateOnlyStringTimeColumn() {
		upperBound = startOfDay(upperBound)
		safeTime = startOfDay(safeTime)
	}
	if safeTime.Before(upperBound) {
		upperBound = safeTime
	}
	if job.hasArchiveSecondWindow() {
		upperBound = alignWindowEnd(upperBound, job.ArchiveWindowSeconds)
	}
	if watermark, err := s.loadWatermark(ctx, controlDB, job.Name); err == nil && watermark != nil && watermark.WatermarkTime.Valid {
		if !upperBound.After(watermark.WatermarkTime.Time) {
			return time.Time{}, false
		}
	}
	return upperBound, true
}

// deleteUpperBound 计算当前安全可删除上界。
// 删除默认按 hot_keep_days 控制，也可通过 delete_delay_days 和 delete_window_seconds 做独立周期。
func (s *Service) deleteUpperBound(ctx context.Context, job jobConfig) (time.Time, bool) {
	now := time.Now()
	safeTime := now.Add(-time.Duration(s.safeDelayMinutes()) * time.Minute)
	upperBound := now.AddDate(0, 0, -job.DeleteDelayDays)
	if job.isDateOnlyStringTimeColumn() {
		upperBound = startOfDay(upperBound)
		safeTime = startOfDay(safeTime)
	}
	if safeTime.Before(upperBound) {
		upperBound = safeTime
	}
	if job.hasDeleteSecondWindow() {
		upperBound = alignWindowEnd(upperBound, job.DeleteWindowSeconds)
	}
	if !upperBound.After(time.Time{}) {
		return time.Time{}, false
	}
	return upperBound, true
}
