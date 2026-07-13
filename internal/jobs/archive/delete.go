package archive

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"admin/internal/task/stats"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

// deleteSegmentResult 描述单个删除区间本轮执行结果，用于 auto 模式追赶空窗口和稀疏窗口。
type deleteSegmentResult struct {
	RowsDeleted  int64         // 本轮删除的热表行数
	Elapsed      time.Duration // 本轮处理该区间的耗时
	Progressed   bool          // 本轮是否删除或完成了一个区间
	LimitReached bool          // 是否达到单轮删除批次硬上限
}

// leaseHeartbeat 在长批次执行期间持续续租；续租失败会取消处理上下文。
type leaseHeartbeat struct {
	ctx      context.Context    // 删除处理使用的上下文
	cancel   context.CancelFunc // 续租失败时取消当前删除批次
	stop     chan struct{}      // 正常停止续租信号
	done     chan struct{}      // 续租协程退出信号
	stopOnce sync.Once          // 保证停止信号只关闭一次
	err      error              // 续租协程的终态错误
}

// deleteArchivedSegments 按删除窗口领取已归档区间，并分批删除热表数据。
// 删除只处理 status=done 的区间，且每批删除前会校验对应主键已存在于历史表，避免归档缺口导致数据丢失。
func (s *Service) deleteArchivedSegments(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB, workerID string) error {
	processed := 0
	var lastResult *deleteSegmentResult
	for {
		if !shouldContinueDeleteRun(job, processed, lastResult) {
			return nil
		}
		segment, err := s.claimNextDeleteSegment(ctx, job, controlDB, workerID)
		if err != nil {
			return errors.Tag(err)
		}
		if segment == nil {
			return nil
		}
		result, err := s.processDeleteSegment(ctx, job, sourceDB, controlDB, segment, workerID)
		if err != nil {
			finalCtx, cancelFinal := context.WithTimeout(context.Background(), archiveFinalStateTimeout)
			markErr := s.markSegmentDeleteFailed(finalCtx, controlDB, segment, workerID, err)
			cancelFinal()
			if markErr != nil {
				return errors.Join(errors.Tag(err), errors.Wrap(markErr, "归档区间删除失败状态写回失败"))
			}
			return errors.Tag(err)
		}
		if !result.Progressed {
			return nil
		}
		if result.LimitReached {
			return nil
		}
		lastResult = &result
		processed++
	}
}

// claimNextDeleteSegment 领取下一个达到删除上界的已归档区间。
// 这里复用 archive_segment 的状态和租约，确保同一区间不会被多个删除 worker 同时处理。
func (s *Service) claimNextDeleteSegment(ctx context.Context, job jobConfig, controlDB *gorm.DB, workerID string) (*Segment, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(s.leaseTTL())
	upperBound, ok := s.deleteUpperBound(ctx, job)
	if !ok {
		return nil, nil
	}
	var claimed Segment
	err := controlDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("job_name = ?", job.Name).
			Where("range_start < ?", upperBound).
			Where("(status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?))", statusDone, statusDeleting, now).
			Order("range_start ASC").
			Take(&claimed).Error
		if err != nil {
			return errors.Tag(err)
		}
		claimed.Status = statusDeleting
		claimed.WorkerID = workerID
		claimed.LeaseExpiresAt = sql.NullTime{Time: leaseExpiresAt, Valid: true}
		claimed.AttemptCount++
		claimed.ErrorMessage = ""
		claimed.UpdatedAt = now
		return tx.Model(&Segment{}).
			Where("id = ?", claimed.ID).
			Updates(map[string]any{
				"status":           claimed.Status,
				"worker_id":        claimed.WorkerID,
				"lease_expires_at": claimed.LeaseExpiresAt,
				"attempt_count":    claimed.AttemptCount,
				"error_message":    claimed.ErrorMessage,
				"updated_at":       claimed.UpdatedAt,
			}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &claimed, nil
}

// processDeleteSegment 删除单个已归档区间内当前删除窗口的热表数据。
// Progressed 表示本轮是否确实删除或完成了一个区间，调用方据此避免在未到删除上界时重复抢占同一区间。
func (s *Service) processDeleteSegment(ctx context.Context, job jobConfig, sourceDB *gorm.DB, controlDB *gorm.DB, segment *Segment, workerID string) (result deleteSegmentResult, err error) {
	startedAt := time.Now()
	defer func() {
		result.Elapsed = time.Since(startedAt)
	}()
	if segment == nil {
		return result, nil
	}
	heartbeat := startLeaseHeartbeat(ctx, s.deleteLeaseRenewInterval(), func(renewCtx context.Context) error {
		return s.renewSegmentDeleteLease(renewCtx, controlDB, segment, workerID)
	})
	ctx = heartbeat.Context()
	defer func() {
		if heartbeatErr := heartbeat.Close(); heartbeatErr != nil {
			if err == nil {
				err = errors.Tag(heartbeatErr)
			} else {
				err = errors.Wrapf(err, "归档区间删除失败且租约续租未完成: %v", heartbeatErr)
			}
		}
	}()
	if !tableExists(ctx, sourceDB, segment.HistoryTableName) {
		return result, errors.Errorf("归档历史表不存在，禁止删除热表数据: job=%s table=%s", job.Name, segment.HistoryTableName)
	}
	deleteBatchSize := job.DeleteBatchSize
	if deleteBatchSize <= 0 {
		deleteBatchSize = defaultDeleteBatchSize
	}
	upperBound, ok := s.deleteUpperBound(ctx, job)
	if !ok {
		if err := heartbeat.Stop(); err != nil {
			return result, errors.Tag(err)
		}
		if err := s.markSegmentDeletePartial(ctx, controlDB, segment, workerID); err != nil {
			return result, errors.Tag(err)
		}
		return result, nil
	}
	deleteRangeEnd := segment.RangeEnd
	if upperBound.Before(deleteRangeEnd) {
		deleteRangeEnd = upperBound
	}
	if !deleteRangeEnd.After(segment.RangeStart) {
		if err := heartbeat.Stop(); err != nil {
			return result, errors.Tag(err)
		}
		if err := s.markSegmentDeletePartial(ctx, controlDB, segment, workerID); err != nil {
			return result, errors.Tag(err)
		}
		return result, nil
	}
	if job.hasDeleteSecondWindow() {
		rows, err := s.loadDeleteCursorRows(ctx, sourceDB, job, segment, deleteRangeEnd)
		if err != nil {
			return result, errors.Tag(err)
		}
		if len(rows) == 0 {
			if deleteRangeEnd.Before(segment.RangeEnd) {
				if err := heartbeat.Stop(); err != nil {
					return result, errors.Tag(err)
				}
				if err := s.markSegmentDeletePartial(ctx, controlDB, segment, workerID); err != nil {
					return result, errors.Tag(err)
				}
				return result, nil
			}
			if err := heartbeat.Stop(); err != nil {
				return result, errors.Tag(err)
			}
			if err := s.markSegmentDeleted(ctx, controlDB, segment, workerID); err != nil {
				return result, errors.Tag(err)
			}
			result.Progressed = true
			return result, nil
		}
		// 删除窗口从当前区间最早剩余热数据所在窗口开始，确保按固定时间片逐步清理大表。
		windowStart := alignWindowEnd(rows[0].CreatedAt, job.DeleteWindowSeconds)
		windowEnd := windowStart.Add(time.Duration(job.DeleteWindowSeconds) * time.Second)
		if windowEnd.Before(deleteRangeEnd) {
			deleteRangeEnd = windowEnd
		}
	}
	processedBatches := 0
	for {
		if err := ctx.Err(); err != nil {
			return result, errors.Tag(err)
		}
		rows, err := s.loadDeleteCursorRows(ctx, sourceDB, job, segment, deleteRangeEnd)
		if err != nil {
			return result, errors.Tag(err)
		}
		if len(rows) > 0 {
			taskstats.RecordRead(ctx, archiveTraceName(job.Name, archiveRunModeDelete, taskstats.DetailPartRows), int64(len(rows)))
		}
		if len(rows) == 0 {
			if deleteRangeEnd.Before(segment.RangeEnd) {
				if err := heartbeat.Stop(); err != nil {
					return result, errors.Tag(err)
				}
				if err := s.markSegmentDeletePartial(ctx, controlDB, segment, workerID); err != nil {
					return result, errors.Tag(err)
				}
				result.Progressed = result.RowsDeleted > 0
				return result, nil
			}
			if err := heartbeat.Stop(); err != nil {
				return result, errors.Tag(err)
			}
			if err := s.markSegmentDeleted(ctx, controlDB, segment, workerID); err != nil {
				return result, errors.Tag(err)
			}
			result.Progressed = true
			return result, nil
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		// 每批写操作前再次校验 fencing，避免进程长暂停后旧 worker 先于心跳协程恢复执行。
		if err = s.renewSegmentDeleteLease(ctx, controlDB, segment, workerID); err != nil {
			return result, errors.Tag(err)
		}
		if err = s.deleteBatchChunk(ctx, sourceDB, job, segment, deleteRangeEnd, ids); err != nil {
			return result, errors.Tag(err)
		}
		taskstats.RecordDelete(ctx, archiveTraceName(job.Name, archiveRunModeDelete, taskstats.DetailPartRows), int64(len(rows)))
		result.RowsDeleted += int64(len(rows))
		processedBatches++
		if processedBatches >= maxDeleteBatchesPerRun {
			if err := heartbeat.Stop(); err != nil {
				return result, errors.Tag(err)
			}
			if err := s.markSegmentDeletePartial(ctx, controlDB, segment, workerID); err != nil {
				return result, errors.Tag(err)
			}
			result.Progressed = true
			result.LimitReached = true
			return result, nil
		}
		if len(rows) >= deleteBatchSize {
			if err = waitArchiveBatch(ctx, s.batchDelay()); err != nil {
				return result, errors.Tag(err)
			}
		}
	}
}

// Context 返回受续租失败信号控制的处理上下文。
func (h *leaseHeartbeat) Context() context.Context {
	if h == nil || h.ctx == nil {
		return context.Background()
	}
	return h.ctx
}

// Stop 停止续租并等待正在执行的续租结束，但保留处理上下文供终态写入使用。
func (h *leaseHeartbeat) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		close(h.stop)
	})
	<-h.done
	return errors.Tag(h.err)
}

// Close 停止续租并释放处理上下文。
func (h *leaseHeartbeat) Close() error {
	if h == nil {
		return nil
	}
	err := h.Stop()
	h.cancel()
	return errors.Tag(err)
}

// startLeaseHeartbeat 启动租约心跳；单次续租被限制在一个心跳周期内。
func startLeaseHeartbeat(parent context.Context, interval time.Duration, renew func(context.Context) error) *leaseHeartbeat {
	if parent == nil {
		parent = context.Background()
	}
	if interval <= 0 {
		interval = time.Second
	}
	ctx, cancel := context.WithCancel(parent)
	heartbeat := &leaseHeartbeat{
		ctx:    ctx,
		cancel: cancel,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(heartbeat.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.stop:
				return
			case <-ticker.C:
				if renew == nil {
					continue
				}
				renewCtx, renewCancel := context.WithTimeout(ctx, interval)
				renewErr := renew(renewCtx)
				renewCancel()
				if renewErr == nil {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				heartbeat.err = errors.Wrap(renewErr, "归档区间删除续租失败")
				heartbeat.cancel()
				return
			}
		}
	}()
	return heartbeat
}

// loadDeleteCursorRows 读取当前区间下一批可删除热表主键。
// 删除游标不需要额外 checkpoint：已删除行会从热表消失，下一轮自然从当前区间剩余最小主键继续。
func (s *Service) loadDeleteCursorRows(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time) ([]batchCursorRow, error) {
	batchSize := job.DeleteBatchSize
	if batchSize <= 0 {
		batchSize = defaultDeleteBatchSize
	}
	if job.isStringTimeColumn() {
		type dateStringCursorRow struct {
			ID        int64  `gorm:"column:id"`         // 当前批次主键
			CreatedAt string `gorm:"column:created_at"` // 当前批次字符串时间
		}
		rawRows := make([]dateStringCursorRow, 0, batchSize)
		if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rawRows).Error; err != nil {
			return nil, errors.Tag(err)
		}
		rows := make([]batchCursorRow, 0, len(rawRows))
		for _, raw := range rawRows {
			createdAt, err := parseArchiveStringTime(raw.CreatedAt, job.TimeColumnFormat)
			if err != nil {
				return nil, errors.Wrapf(err, "待删除归档字符串时间非法: table=%s column=%s id=%d value=%s", job.TableName, job.TimeColumn, raw.ID, raw.CreatedAt)
			}
			rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
		}
		return rows, nil
	}
	if job.isUnixSecondsTimeColumn() {
		type unixSecondsCursorRow struct {
			ID        int64 `gorm:"column:id"`         // 当前批次主键
			CreatedAt int64 `gorm:"column:created_at"` // 当前批次 Unix int64 时间
		}
		rawRows := make([]unixSecondsCursorRow, 0, batchSize)
		if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rawRows).Error; err != nil {
			return nil, errors.Tag(err)
		}
		rows := make([]batchCursorRow, 0, len(rawRows))
		for _, raw := range rawRows {
			createdAt, err := parseArchiveUnixTime(raw.CreatedAt, job.TimeColumnUnixUnit)
			if err != nil {
				return nil, errors.Wrapf(err, "待删除归档 Unix 时间非法: table=%s column=%s unit=%s id=%d value=%d", job.TableName, job.TimeColumn, job.TimeColumnUnixUnit, raw.ID, raw.CreatedAt)
			}
			rows = append(rows, batchCursorRow{ID: raw.ID, CreatedAt: createdAt})
		}
		return rows, nil
	}
	rows := make([]batchCursorRow, 0, batchSize)
	if err := buildLoadDeleteCursorRowsQuery(ctx, db, job, segment, rangeEnd, batchSize).Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// buildLoadDeleteCursorRowsQuery 构造热表删除游标读取查询。
// 查询只扫描可删除半开窗口，并叠加归档和清理条件。
func buildLoadDeleteCursorRowsQuery(ctx context.Context, db *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time, batchSize int) *gorm.DB {
	query := db.WithContext(ctx).
		Table(quoteIdent(job.TableName)).
		Select("? AS id, ? AS created_at", clause.Column{Name: job.PrimaryKey}, clause.Column{Name: job.TimeColumn})
	query = applyArchiveTimeRangeWhere(query, job, segment.RangeStart, rangeEnd)
	if condition := deleteConditionSQL(job); condition != "" {
		// 删除条件包含归档条件和清理附加条件，确保游标阶段和事务内删除阶段使用同一业务边界。
		query = query.Where("(" + condition + ")")
	}
	return query.
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.TimeColumn}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: job.PrimaryKey}}).
		Limit(batchSize)
}

// deleteBatchChunk 在源库事务内校验历史表已存在对应主键后，按主键删除热表数据。
// 校验与删除同事务执行，避免 DBA 手工误删历史表或归档补偿未完成时仍继续清热表。
func (s *Service) deleteBatchChunk(ctx context.Context, sourceDB *gorm.DB, job jobConfig, segment *Segment, rangeEnd time.Time, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if segment == nil {
		return errors.Errorf("归档区间为空: job=%s table=%s", job.Name, job.TableName)
	}
	if !rangeEnd.After(segment.RangeStart) || rangeEnd.After(segment.RangeEnd) {
		return errors.Errorf("删除窗口边界异常: job=%s table=%s range_start=%s range_end=%s segment_end=%s",
			job.Name, job.TableName, segment.RangeStart.Format(time.DateTime), rangeEnd.Format(time.DateTime), segment.RangeEnd.Format(time.DateTime))
	}
	return sourceDB.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		var copiedCount int64
		if err := tx.Table(segment.HistoryTableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, segment.RangeEnd)...)...).
			Count(&copiedCount).Error; err != nil {
			return errors.Tag(err)
		}
		if copiedCount != int64(len(ids)) {
			return errors.Errorf("删除前历史表校验失败: table=%s want=%d got=%d", segment.HistoryTableName, len(ids), copiedCount)
		}
		result := tx.Table(job.TableName).
			Where(batchSourcePredicateSQL(job), append([]any{ids}, rangePredicateArgs(job, segment.RangeStart, rangeEnd)...)...)
		if condition := deleteConditionSQL(job); condition != "" {
			// 清理阶段再次叠加条件和当前删除窗口，避免读取待删主键后业务状态变化导致误删或跨窗口删除。
			result = result.Where("(" + condition + ")")
		}
		deleteResult := result.Delete(nil)
		if deleteResult.Error != nil {
			return errors.Tag(deleteResult.Error)
		}
		if deleteResult.RowsAffected != int64(len(ids)) {
			return errors.Errorf("热表删除校验失败: table=%s want=%d got=%d", job.TableName, len(ids), deleteResult.RowsAffected)
		}
		return nil
	})
}

// updateSegmentCheckpoint 在源库子批次提交成功后推进归档区间 checkpoint。
// 控制库更新不和源库事务强绑定，重试时历史表主键幂等能保证继续向前推进。

// renewSegmentDeleteLease 按领取次数 fencing 续租，过期或被接管的旧 worker 不能恢复租约。
func (s *Service) renewSegmentDeleteLease(ctx context.Context, controlDB *gorm.DB, segment *Segment, workerID string) error {
	if segment == nil {
		return errors.New("归档区间为空，无法续租删除任务")
	}
	now := time.Now()
	result := controlDB.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusDeleting, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"lease_expires_at": sql.NullTime{Time: now.Add(s.leaseTTL()), Valid: true},
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间删除续租失败，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// ensureStringTimeHistoryMatchesHot 校验字符串时间批次冷热表时间值一致。
func (s *Service) ensureStringTimeHistoryMatchesHot(ctx context.Context, tx *gorm.DB, job jobConfig, historyTable string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var mismatchCount int64
	if err := buildStringTimeHistoryMatchQuery(ctx, tx, job, historyTable, ids).Count(&mismatchCount).Error; err != nil {
		return errors.Tag(err)
	}
	if mismatchCount > 0 {
		return errors.Errorf("历史表业务日期校验失败: table=%s mismatch=%d", historyTable, mismatchCount)
	}
	return nil
}

// buildStringTimeHistoryMatchQuery 构造字符串时间冷热表一致性校验查询。
// 热表主键集合来自当前归档批次，历史表名来自已领取区间；仅统计同主键但业务日期不同的脏数据。
func buildStringTimeHistoryMatchQuery(ctx context.Context, db *gorm.DB, job jobConfig, historyTable string, ids []int64) *gorm.DB {
	primaryKey := quoteIdent(job.PrimaryKey)
	timeColumn := quoteIdent(job.TimeColumn)
	return db.WithContext(ctx).
		Table(quoteIdent(job.TableName)+" AS hot").
		Joins(fmt.Sprintf("JOIN %s AS history ON history.%s = hot.%s", quoteIdent(historyTable), primaryKey, primaryKey)).
		Where(fmt.Sprintf("hot.%s IN ?", primaryKey), ids).
		Where(fmt.Sprintf("history.%s <> hot.%s", timeColumn, timeColumn))
}

// advanceWatermark 只按“最长连续完成区间”推进水位线，不允许跳跃推进。
