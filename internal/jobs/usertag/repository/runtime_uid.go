package repository

import (
	"context"
	"time"

	"admin_cron/internal/model"
	"admin_cron/internal/svc"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm/clause"
)

// AddRuntimeUIDs 写入本次工作流需要继续计算的 UID 集合。
func (d RuntimeDeps) AddRuntimeUIDs(ctx context.Context, workflowID, source string, uids []int64) error {
	if workflowID == "" || len(uids) == 0 {
		return nil
	}
	db, err := writeDB(d.Service, svc.DatabaseMain)
	if err != nil {
		return errors.Tag(err)
	}
	now := time.Now()
	rows := make([]model.UserTagRuntimeUID, 0, len(uids))
	seen := make(map[int64]struct{}, len(uids)) // seen 用于本批去重，避免重复主键冲突浪费写入
	for _, uid := range uids {
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		rows = append(rows, model.UserTagRuntimeUID{
			WorkflowID: workflowID,
			UID:        uid,
			ShardNo:    int8(d.ShardPlan.UIDShard(uid, d.ShardPlan.RuntimeShardTotal)),
			Source:     source,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).Table(model.TableNameUserTagRuntimeUID).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "workflow_id"}, {Name: "uid"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"shard_no",
				"source",
				"updated_at",
			}),
		}).
		CreateInBatches(rows, 1000).Error
}
