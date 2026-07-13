package runtimeconfig

import (
	"database/sql"
	"strconv"
	"time"

	"admin/common/codes"
	"admin/common/i18n"
	"admin/internal/jobs/archive"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// GetArchiveJobProgress 查询归档任务草稿对应的当前运行态水位和区间进度。
func (l *RuntimeConfigLogic) GetArchiveJobProgress(req *types.RuntimeConfigIDReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.GetArchiveJobProgress DB未初始化").ToBizResult()
	}
	var row model.RuntimeArchiveJob
	if err = db.Select("id", "name").Where("id = ?", req.ID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err, "归档任务草稿不存在: %d", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.GetArchiveJobProgress 查询草稿失败").ToBizResult()
	}
	progress, err := archive.NewService(l.Svc, archive.WithControlDatabase(svc.DatabaseMain)).Progress(l.Ctx, row.Name)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.GetArchiveJobProgress 查询执行详情失败").ToBizResult()
	}
	return types.NewBizResult(codes.FetchSuccess).WithData(archiveProgressToResp(row.ID, progress))
}

// archiveProgressToResp 把归档领域进度转换为稳定的接口响应结构。
func archiveProgressToResp(jobID uint64, progress archive.Progress) *types.RuntimeArchiveProgressResp {
	recent := make([]types.RuntimeArchiveSegmentItem, 0, len(progress.RecentSegments))
	for _, segment := range progress.RecentSegments {
		recent = append(recent, archiveSegmentToItem(segment))
	}
	var current *types.RuntimeArchiveSegmentItem
	if progress.CurrentSegment != nil {
		item := archiveSegmentToItem(*progress.CurrentSegment)
		current = &item
	}
	var lagSeconds *int64
	if progress.LagSeconds.Valid {
		value := progress.LagSeconds.Int64
		lagSeconds = &value
	}
	return &types.RuntimeArchiveProgressResp{
		JobID:              jobID,
		JobName:            progress.JobName,
		RuntimeMatched:     progress.RuntimeMatched,
		RuntimeEnabled:     progress.RuntimeEnabled,
		SchemaReady:        progress.SchemaReady,
		Phase:              progress.Phase,
		WatermarkTime:      formatArchiveProgressNullTime(progress.WatermarkTime),
		WatermarkUpdatedAt: formatArchiveProgressNullTime(progress.WatermarkUpdatedAt),
		EligibleUntil:      formatArchiveProgressNullTime(progress.EligibleUntil),
		PlannedUntil:       formatArchiveProgressNullTime(progress.PlannedUntil),
		LagSeconds:         lagSeconds,
		Counts: types.RuntimeArchiveProgressCounts{
			Total:    progress.Counts.Total,
			Pending:  progress.Counts.Pending,
			Running:  progress.Counts.Running,
			Done:     progress.Counts.Done,
			Deleting: progress.Counts.Deleting,
			Deleted:  progress.Counts.Deleted,
			Failed:   progress.Counts.Failed,
		},
		CurrentSegment: current,
		RecentSegments: recent,
		FetchedAt:      formatArchiveProgressTime(progress.FetchedAt),
	}
}

// archiveSegmentToItem 把归档区间转换为接口展示项。
func archiveSegmentToItem(segment archive.ProgressSegment) types.RuntimeArchiveSegmentItem {
	return types.RuntimeArchiveSegmentItem{
		ID:                       segment.ID,
		HistoryTableName:         segment.HistoryTableName,
		RangeStart:               formatArchiveProgressTime(segment.RangeStart),
		RangeEnd:                 formatArchiveProgressTime(segment.RangeEnd),
		Status:                   segment.Status,
		WorkerID:                 segment.WorkerID,
		LeaseExpiresAt:           formatArchiveProgressNullTime(segment.LeaseExpiresAt),
		LastArchivedID:           strconv.FormatInt(segment.LastArchivedID, 10),
		LastArchivedTime:         formatArchiveProgressNullTime(segment.LastArchivedTime),
		RowsArchived:             segment.RowsArchived,
		AttemptCount:             segment.AttemptCount,
		UpdatedAt:                formatArchiveProgressTime(segment.UpdatedAt),
		CompletedAt:              formatArchiveProgressNullTime(segment.CompletedAt),
		EstimatedProgressPercent: segment.EstimatedProgressPercent,
	}
}

// formatArchiveProgressNullTime 格式化可空归档控制表时间。
func formatArchiveProgressNullTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return formatArchiveProgressTime(value.Time)
}

// formatArchiveProgressTime 格式化归档执行详情时间。
func formatArchiveProgressTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.DateTime)
}
