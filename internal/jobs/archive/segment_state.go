package archive

import (
	"context"
	"database/sql"
	"strings"
	"time"

	redislock "admin/internal/infra/redsync"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

// updateSegmentCheckpoint 原子推进区间游标、累计归档行数并续租。
func (s *Service) updateSegmentCheckpoint(ctx context.Context, controlDB *gorm.DB, segment *Segment, archivedRows int, lastTime time.Time, lastID int64, workerID string) error {
	if segment == nil || archivedRows <= 0 {
		return nil
	}
	now := time.Now()
	result := controlDB.WithContext(ctx).Clauses(dbresolver.Write).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusRunning, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"last_archived_id":   lastID,
			"last_archived_time": sql.NullTime{Time: lastTime, Valid: true},
			"rows_archived":      gorm.Expr("rows_archived + ?", archivedRows),
			"worker_id":          workerID,
			"lease_expires_at":   sql.NullTime{Time: now.Add(s.leaseTTL()), Valid: true},
			"updated_at":         now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间 checkpoint 更新失败，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// advanceWatermark 按连续完成的区间推进归档水位线。
func (s *Service) advanceWatermark(ctx context.Context, job jobConfig, controlDB *gorm.DB) error {
	lockKey, err := s.archiveJobWatermarkKey(job.Name)
	if err != nil {
		return errors.Tag(err)
	}
	return redislock.WithLock(ctx, s.redisClient(), lockKey, s.lockTTL(), func(lockCtx context.Context) error {
		var watermarkTime *time.Time
		current, err := s.loadWatermark(lockCtx, controlDB, job.Name)
		if err != nil {
			return errors.Tag(err)
		}
		cursor := time.Time{}
		hasCursor := false
		if current != nil && current.WatermarkTime.Valid {
			cursor = current.WatermarkTime.Time
			hasCursor = true
		}

		for {
			query := controlDB.WithContext(lockCtx).
				Where("job_name = ?", job.Name).
				Order("range_start ASC").
				Limit(watermarkScanBatchSize)
			if hasCursor {
				// 已推进过的历史区间不再反复读取，降低控制表长期运行后的扫描成本。
				query = query.Where("range_end > ?", cursor)
			}
			var segments []Segment
			if err := query.Find(&segments).Error; err != nil {
				return errors.Tag(err)
			}
			if len(segments) == 0 {
				break
			}
			progressed := false
			for _, item := range segments {
				if !isArchivedSegmentStatus(item.Status) {
					return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
				}
				if hasCursor && item.RangeStart.After(cursor) {
					// 区间必须连续或重叠完成才能推进 watermark，避免跳过中间断点。
					return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
				}
				if hasCursor && !item.RangeEnd.After(cursor) {
					// 已经被当前水位线覆盖的重叠区间直接跳过。
					continue
				}
				currentEnd := item.RangeEnd
				watermarkTime = &currentEnd
				cursor = currentEnd
				hasCursor = true
				progressed = true
			}
			if len(segments) < watermarkScanBatchSize || !progressed {
				break
			}
		}
		return s.saveWatermarkIfAdvanced(lockCtx, controlDB, job, current, watermarkTime)
	})
}

// saveWatermarkIfAdvanced 在目标水位线确实向前推进时落库。
func (s *Service) saveWatermarkIfAdvanced(ctx context.Context, controlDB *gorm.DB, job jobConfig, current *Watermark, watermarkTime *time.Time) error {
	if watermarkTime == nil {
		return nil
	}
	if current != nil && current.WatermarkTime.Valid && !watermarkTime.After(current.WatermarkTime.Time) {
		return nil
	}
	record := Watermark{
		JobName:         job.Name,
		SourceTableName: job.TableName,
		WatermarkTime:   sql.NullTime{Time: *watermarkTime, Valid: true},
		UpdatedAt:       time.Now(),
	}
	return controlDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"table_name", "watermark_time", "updated_at"}),
	}).Create(&record).Error
}

// cleanupHistoryTables 在历史表数量超过上限时删除最老历史表，控制表数量和元数据成本。

// loadWatermark 读取指定归档任务当前的水位线记录。
func (s *Service) loadWatermark(ctx context.Context, db *gorm.DB, jobName string) (*Watermark, error) {
	if db == nil || !tableExists(ctx, db, tableNameWatermark) {
		return nil, nil
	}
	var item Watermark
	err := db.WithContext(ctx).Where("job_name = ?", jobName).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &item, nil
}

// markSegmentDone 把当前领取轮次标记为完成，并清理运行中的租约信息。
func (s *Service) markSegmentDone(ctx context.Context, db *gorm.DB, segment *Segment, workerID string) error {
	if segment == nil {
		return errors.New("归档区间为空，无法标记完成")
	}
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusRunning, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    "",
			"completed_at":     sql.NullTime{Time: now, Valid: true},
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间完成状态更新失败，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// markSegmentDeleted 把已归档区间标记为热表删除完成。
// deleted 状态表示该时间窗已经只保留在历史表中，后续历史表 TTL 清理才允许统计这类区间。
func (s *Service) markSegmentDeleted(ctx context.Context, db *gorm.DB, segment *Segment, workerID string) error {
	if segment == nil {
		return errors.New("归档区间为空，无法标记删除完成")
	}
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusDeleting, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"status":           statusDeleted,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    "",
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间删除状态更新失败，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// markSegmentDeletePartial 释放当前删除租约，并保持区间为已归档状态。
// 当 delete_window_seconds 只允许本轮删除大区间中的一个时间片时，剩余数据交给下一次删除调度继续处理。
func (s *Service) markSegmentDeletePartial(ctx context.Context, db *gorm.DB, segment *Segment, workerID string) error {
	if segment == nil {
		return errors.New("归档区间为空，无法标记部分删除")
	}
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusDeleting, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间部分删除状态更新失败，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// markSegmentFailed 把当前领取轮次标记为失败，并写入裁剪后的错误摘要以便后续排障与重试。
func (s *Service) markSegmentFailed(ctx context.Context, db *gorm.DB, segment *Segment, workerID string, cause error) error {
	if segment == nil {
		return errors.New("归档区间为空，无法标记失败")
	}
	message := truncateArchiveError(cause)
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusRunning, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"status":           statusFailed,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    message,
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间失败状态写入被拒绝，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// markSegmentDeleteFailed 把删除失败区间恢复为 done，并保留错误摘要供下一轮删除任务重试。
// 删除失败不能把已归档区间改成 failed，否则归档 worker 会误以为需要重新搬迁该区间。
func (s *Service) markSegmentDeleteFailed(ctx context.Context, db *gorm.DB, segment *Segment, workerID string, cause error) error {
	if segment == nil {
		return errors.New("归档区间为空，无法标记删除失败")
	}
	message := truncateArchiveError(cause)
	now := time.Now()
	result := db.WithContext(ctx).Model(&Segment{}).
		Where("id = ?", segment.ID).
		Where("status = ? AND worker_id = ? AND attempt_count = ?", statusDeleting, workerID, segment.AttemptCount).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at > ?", now).
		Updates(map[string]any{
			"status":           statusDone,
			"worker_id":        "",
			"lease_expires_at": sql.NullTime{},
			"error_message":    message,
			"updated_at":       now,
		})
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Errorf("归档区间删除失败状态更新被拒绝，租约已过期或被其他 worker 接管: segment_id=%d worker_id=%s attempt=%d", segment.ID, workerID, segment.AttemptCount)
	}
	return nil
}

// truncateArchiveError 返回可安全写入 utf8mb4 varchar(500) 的错误摘要。
func truncateArchiveError(cause error) string {
	if cause == nil {
		return ""
	}
	message := strings.ToValidUTF8(cause.Error(), "�")
	runes := []rune(message)
	if len(runes) > archiveErrorMessageMaxRunes {
		runes = runes[:archiveErrorMessageMaxRunes]
	}
	return string(runes)
}

// target 支持 `job#archive` / `job#delete` 后缀，用于给归档和删除分别配置不同周期。
