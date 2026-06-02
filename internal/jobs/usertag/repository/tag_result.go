package repository

import (
	"context"
	"regexp"
	"strings"
	"time"

	"admin/internal/jobs/usertag/types"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	userTagRuntimeUIDRetention        = 7 * 24 * time.Hour     // runtime_uid 只保留近期 workflow 候选集合
	userTagRuntimeCheckpointRetention = 7 * 24 * time.Hour     // checkpoint 只保留近期 workflow 节点进度
	userTagEventOutboxDoneRetention   = 7 * 24 * time.Hour     // 已成功派发事件保留时间
	userTagEventOutboxDeadRetention   = 30 * 24 * time.Hour    // 死信事件保留时间
	userTagEventOutboxStaleRetention  = 7 * 24 * time.Hour     // 长期未派发事件超时后回收
	userTagEventOutboxRunningTimeout  = 15 * time.Minute       // running 事件超过该时间视为 worker 异常退出
	userTagEventOutboxMaxAttempt      = 6                      // 单条事件最大派发尝试次数
	userTagRuntimeResetBatchSize      = 1000                   // 当前 workflow 清理单批行数
	userTagRuntimeResetBatchDelay     = 20 * time.Millisecond  // 当前 workflow 清理批次间隔
	userTagRuntimeCleanupBatchSize    = 500                    // 历史运行期表清理单批行数
	userTagRuntimeCleanupBatchDelay   = 100 * time.Millisecond // 历史运行期表清理批次间隔
	userTagRuntimeCleanupTimeBudget   = 55 * time.Minute       // 单次独立清理任务总时间预算
)

// EventDispatcher 表示标签得失事件派发函数。
type EventDispatcher func(context.Context, []types.TagChange) error

// TagRepository 提供用户标签工作流骨架所需的结果表、运行期表和事件 outbox 访问能力。
type TagRepository struct {
	deps RuntimeDeps // 运行依赖
}

// NewTagRepository 创建用户标签结果仓储。
func NewTagRepository(deps RuntimeDeps) *TagRepository {
	return &TagRepository{deps: deps}
}

// PrepareResultTables 清空 full 模式临时标签结果表。
func (r *TagRepository) PrepareResultTables(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun || opts.SyncSnapshotOnly || opts.Mode != types.ModeFull {
		return nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return errors.Tag(err)
	}
	for _, shard := range r.deps.ShardPlan.TagShardsForWorkflow(opts.ShardIndex, opts.ShardTotal) {
		currentTable := model.UserTagShardTableName(shard)
		tmpTable := model.UserTagTmpShardTableName(shard)
		if err := r.ensureResultShardTable(ctx, logDB, currentTable); err != nil {
			return errors.Wrapf(err, "创建用户标签结果表失败 table=%s", currentTable)
		}
		if err := logDB.WithContext(ctx).Exec(userTagCreateLikeTableSQL(tmpTable, currentTable)).Error; err != nil {
			return errors.Wrapf(err, "创建用户标签临时表失败 table=%s", tmpTable)
		}
		if err := logDB.WithContext(ctx).Exec(userTagTruncateTableSQL(tmpTable)).Error; err != nil {
			return errors.Wrapf(err, "清空用户标签临时表失败 table=%s", tmpTable)
		}
	}
	return nil
}

// FinalizeResultTables 在 full 所有节点成功后交换 tmp 和线上结果表。
func (r *TagRepository) FinalizeResultTables(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun || opts.SyncSnapshotOnly || opts.Mode != types.ModeFull {
		return nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return errors.Tag(err)
	}
	for _, shard := range r.deps.ShardPlan.TagShardsForWorkflow(opts.ShardIndex, opts.ShardTotal) {
		currentTable := model.UserTagShardTableName(shard)
		tmpTable := model.UserTagTmpShardTableName(shard)
		swapTable := model.UserTagSwapShardTableName(shard)
		renameItems := []string{
			quoteIdent(currentTable) + " TO " + quoteIdent(swapTable),
			quoteIdent(tmpTable) + " TO " + quoteIdent(currentTable),
			quoteIdent(swapTable) + " TO " + quoteIdent(tmpTable),
		}
		if err := logDB.WithContext(ctx).Exec(userTagRenameTableSQL(renameItems)).Error; err != nil {
			return errors.Wrapf(err, "切换用户标签结果表失败 shard=%d", shard)
		}
	}
	return nil
}

// RebuildReadSnapshotShard 重建当前执行分片负责的只读标签快照。
func (r *TagRepository) RebuildReadSnapshotShard(ctx context.Context, opts types.RuntimeOptions) (int, error) {
	if opts.DryRun || opts.Mode != types.ModeFull {
		return 0, nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	total := 0
	for _, shard := range r.deps.ShardPlan.TagShardsForWorkflow(opts.ShardIndex, opts.ShardTotal) {
		sourceTable := model.UserTagShardTableName(shard)
		targetTable := model.UserTagSyncShardTableName(shard)
		if err := r.ensureResultShardTable(ctx, logDB, sourceTable); err != nil {
			return total, errors.Wrapf(err, "创建用户标签结果表失败 table=%s", sourceTable)
		}
		if err := logDB.WithContext(ctx).Exec(userTagCreateLikeTableSQL(targetTable, sourceTable)).Error; err != nil {
			return total, errors.Wrapf(err, "创建用户标签只读快照表失败 table=%s", targetTable)
		}
		if err := logDB.WithContext(ctx).Exec(userTagTruncateTableSQL(targetTable)).Error; err != nil {
			return total, errors.Wrapf(err, "清空用户标签只读快照表失败 table=%s", targetTable)
		}
		result := logDB.WithContext(ctx).Exec(
			"INSERT INTO " + quoteIdent(targetTable) + " (uid, shard_no, tag_type, tag_source, tag_data, tag_category, created_at, updated_at) " +
				"SELECT uid, shard_no, tag_type, tag_source, tag_data, tag_category, created_at, updated_at FROM " + quoteIdent(sourceTable),
		)
		if result.Error != nil {
			return total, errors.Wrapf(result.Error, "重建用户标签只读快照失败 table=%s", targetTable)
		}
		total += int(result.RowsAffected)
	}
	return total, nil
}

// ResetRuntimeState 清理当前工作流的运行期 UID、checkpoint 和事件 outbox。
func (r *TagRepository) ResetRuntimeState(ctx context.Context, workflowID string) error {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return errors.Tag(err)
	}
	if err := r.resetRuntimeUIDRowsByWorkflow(ctx, logDB, workflowID); err != nil {
		return errors.Tag(err)
	}
	if err := r.resetRuntimeCheckpointRowsByWorkflow(ctx, logDB, workflowID); err != nil {
		return errors.Tag(err)
	}
	if err := r.resetEventOutboxRowsByWorkflow(ctx, logDB, workflowID); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// resetRuntimeUIDRowsByWorkflow 小批清理当前 workflow 的运行期 UID。
func (r *TagRepository) resetRuntimeUIDRowsByWorkflow(ctx context.Context, logDB *gorm.DB, workflowID string) error {
	for {
		uids := make([]int64, 0, userTagRuntimeResetBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagRuntimeUID{}).
			Select("uid").
			Where("workflow_id = ?", workflowID).
			Order("uid ASC").
			Limit(userTagRuntimeResetBatchSize).
			Find(&uids).Error; err != nil {
			return errors.Tag(err)
		}
		if len(uids) == 0 {
			return nil
		}
		if err := logDB.WithContext(ctx).
			Where("workflow_id = ? AND uid IN ?", workflowID, uids).
			Delete(&model.UserTagRuntimeUID{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(uids) < userTagRuntimeResetBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeResetBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// resetRuntimeCheckpointRowsByWorkflow 小批清理当前 workflow 的节点进度。
func (r *TagRepository) resetRuntimeCheckpointRowsByWorkflow(ctx context.Context, logDB *gorm.DB, workflowID string) error {
	for {
		rows := make([]model.UserTagRuntimeCheckpoint, 0, userTagRuntimeResetBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagRuntimeCheckpoint{}).
			Select("node", "shard_no").
			Where("workflow_id = ?", workflowID).
			Order("node ASC, shard_no ASC").
			Limit(userTagRuntimeResetBatchSize).
			Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		tuples := make([][]any, 0, len(rows))
		for _, row := range rows {
			tuples = append(tuples, []any{workflowID, row.Node, row.ShardNo})
		}
		if err := logDB.WithContext(ctx).
			Where("(workflow_id, node, shard_no) IN ?", tuples).
			Delete(&model.UserTagRuntimeCheckpoint{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) < userTagRuntimeResetBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeResetBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// resetEventOutboxRowsByWorkflow 小批清理当前 workflow 的事件 outbox。
func (r *TagRepository) resetEventOutboxRowsByWorkflow(ctx context.Context, logDB *gorm.DB, workflowID string) error {
	for {
		ids := make([]int64, 0, userTagRuntimeResetBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
			Select("id").
			Where("workflow_id = ?", workflowID).
			Order("id ASC").
			Limit(userTagRuntimeResetBatchSize).
			Find(&ids).Error; err != nil {
			return errors.Tag(err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := logDB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.UserTagEventOutbox{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(ids) < userTagRuntimeResetBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeResetBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// CleanupStaleRuntimeTables 按保留期机会型回收历史运行期辅助表。
func (r *TagRepository) CleanupStaleRuntimeTables(ctx context.Context, now time.Time) error {
	logDB, err := r.logDB()
	if err != nil {
		return errors.Tag(err)
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, userTagRuntimeCleanupTimeBudget)
	defer cancel()
	if err := r.cleanupRuntimeUIDRows(cleanupCtx, logDB, now.Add(-userTagRuntimeUIDRetention)); err != nil {
		return errors.Wrapf(err, "清理 runtime_uid 失败")
	}
	if err := r.cleanupRuntimeCheckpointRows(cleanupCtx, logDB, now.Add(-userTagRuntimeCheckpointRetention)); err != nil {
		return errors.Wrapf(err, "清理 runtime_checkpoint 失败")
	}
	if err := r.cleanupEventOutboxRows(cleanupCtx, logDB, []model.UserTagEventOutboxState{model.UserTagEventOutboxStateDone}, now.Add(-userTagEventOutboxDoneRetention)); err != nil {
		return errors.Wrapf(err, "清理 done event_outbox 失败")
	}
	if err := r.cleanupEventOutboxRows(cleanupCtx, logDB, []model.UserTagEventOutboxState{model.UserTagEventOutboxStateDead}, now.Add(-userTagEventOutboxDeadRetention)); err != nil {
		return errors.Wrapf(err, "清理 dead event_outbox 失败")
	}
	return r.cleanupEventOutboxRows(cleanupCtx, logDB, []model.UserTagEventOutboxState{
		model.UserTagEventOutboxStatePending,
		model.UserTagEventOutboxStateRetry,
		model.UserTagEventOutboxStateRunning,
	}, now.Add(-userTagEventOutboxStaleRetention))
}

// cleanupRuntimeUIDRows 按创建时间回收历史运行期 UID。
func (r *TagRepository) cleanupRuntimeUIDRows(ctx context.Context, logDB *gorm.DB, cutoff time.Time) error {
	for {
		rows := make([]model.UserTagRuntimeUID, 0, userTagRuntimeCleanupBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagRuntimeUID{}).
			Where("created_at < ?", cutoff).
			Order("created_at ASC, uid ASC").
			Limit(userTagRuntimeCleanupBatchSize).
			Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		pairs := make([][]any, 0, len(rows))
		for _, row := range rows {
			pairs = append(pairs, []any{row.WorkflowID, row.UID})
		}
		if err := logDB.WithContext(ctx).Where("(workflow_id, uid) IN ?", pairs).Delete(&model.UserTagRuntimeUID{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) < userTagRuntimeCleanupBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeCleanupBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// cleanupRuntimeCheckpointRows 按更新时间回收历史节点进度。
func (r *TagRepository) cleanupRuntimeCheckpointRows(ctx context.Context, logDB *gorm.DB, cutoff time.Time) error {
	for {
		rows := make([]model.UserTagRuntimeCheckpoint, 0, userTagRuntimeCleanupBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagRuntimeCheckpoint{}).
			Where("updated_at < ?", cutoff).
			Order("updated_at ASC, workflow_id ASC").
			Limit(userTagRuntimeCleanupBatchSize).
			Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		tuples := make([][]any, 0, len(rows))
		for _, row := range rows {
			tuples = append(tuples, []any{row.WorkflowID, row.Node, row.ShardNo})
		}
		if err := logDB.WithContext(ctx).Where("(workflow_id, node, shard_no) IN ?", tuples).Delete(&model.UserTagRuntimeCheckpoint{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) < userTagRuntimeCleanupBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeCleanupBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// cleanupEventOutboxRows 按状态和更新时间回收历史事件 outbox。
func (r *TagRepository) cleanupEventOutboxRows(ctx context.Context, logDB *gorm.DB, states []model.UserTagEventOutboxState, cutoff time.Time) error {
	for {
		ids := make([]int64, 0, userTagRuntimeCleanupBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
			Select("id").
			Where("state IN ? AND updated_at < ?", states, cutoff).
			Order("updated_at ASC, id ASC").
			Limit(userTagRuntimeCleanupBatchSize).
			Find(&ids).Error; err != nil {
			return errors.Tag(err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := logDB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.UserTagEventOutbox{}).Error; err != nil {
			return errors.Tag(err)
		}
		if len(ids) < userTagRuntimeCleanupBatchSize {
			return nil
		}
		if err := waitUserTagRuntimeBatch(ctx, userTagRuntimeCleanupBatchDelay); err != nil {
			return errors.Tag(err)
		}
	}
}

// WalkRuntimeUIDBatches 按运行期 UID 游标遍历当前分片。
func (r *TagRepository) WalkRuntimeUIDBatches(ctx context.Context, opts types.RuntimeOptions, fn func([]int64) error) error {
	if fn == nil {
		return nil
	}
	lastUID := int64(0)
	for {
		uids, err := r.runtimeUIDBatch(ctx, opts, lastUID)
		if err != nil {
			return errors.Tag(err)
		}
		if len(uids) == 0 {
			return nil
		}
		if err := fn(uids); err != nil {
			return errors.Tag(err)
		}
		lastUID = uids[len(uids)-1]
		if len(uids) < opts.BatchSize {
			return nil
		}
	}
}

// runtimeUIDBatch 读取当前 workflow 分片内的一批候选 UID。
func (r *TagRepository) runtimeUIDBatch(ctx context.Context, opts types.RuntimeOptions, lastUID int64) ([]int64, error) {
	logDB, err := r.logDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	shard := r.deps.ShardPlan.NormalizeShard(opts.ShardIndex, opts.ShardTotal)
	condition, err := r.deps.ShardPlan.IndexedUIDCondition("uid", "shard_no", shard)
	if err != nil {
		return nil, errors.Tag(err)
	}
	batchSize := positiveBatchSize(opts.BatchSize)
	uids := make([]int64, 0, batchSize)
	query := logDB.WithContext(ctx).Table(model.TableNameUserTagRuntimeUID).
		Select("uid").
		Where("workflow_id = ? AND uid > ?", opts.WorkflowID, lastUID).
		Order("uid ASC").
		Limit(batchSize)
	if condition.Expr != "" {
		query = query.Where(condition.Expr, condition.Args...)
	}
	if err := query.Find(&uids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return uids, nil
}

// RetryEventOutboxAbnormalRows 周期扫描并重派异常事件 outbox。
func (r *TagRepository) RetryEventOutboxAbnormalRows(ctx context.Context, opts types.RuntimeOptions, dispatch EventDispatcher) (int, error) {
	if opts.DryRun || opts.SyncSnapshotOnly {
		return 0, nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	if err := r.recoverRunningEventOutboxRows(ctx, logDB, opts, time.Now().Add(-userTagEventOutboxRunningTimeout)); err != nil {
		return 0, errors.Tag(err)
	}
	return r.DrainEventOutboxShard(ctx, opts, dispatch)
}

// DrainEventOutboxShard 派发当前条件命中的事件 outbox，并在成功后标记完成。
func (r *TagRepository) DrainEventOutboxShard(ctx context.Context, opts types.RuntimeOptions, dispatch EventDispatcher) (int, error) {
	if opts.DryRun || opts.SyncSnapshotOnly {
		return 0, nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	rows, err := r.claimEventOutboxRows(ctx, logDB, opts)
	if err != nil {
		return 0, errors.Tag(err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if dispatch == nil {
		dispatchErr := errors.Errorf("用户标签事件 outbox 派发函数未注册 workflow_id=%s", strings.TrimSpace(opts.WorkflowID))
		if markErr := r.markEventOutboxFailed(ctx, logDB, rows, dispatchErr); markErr != nil {
			return 0, errors.Wrapf(markErr, "标记用户标签事件 outbox 重试失败 original=%s", dispatchErr.Error())
		}
		return 0, errors.Tag(dispatchErr)
	}
	events := make([]types.TagChange, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		events = append(events, eventOutboxRowToChange(row))
	}
	if err := dispatch(ctx, events); err != nil {
		if markErr := r.markEventOutboxFailed(ctx, logDB, rows, err); markErr != nil {
			return 0, errors.Wrapf(markErr, "标记用户标签事件 outbox 重试失败 original=%s", err.Error())
		}
		return 0, errors.Tag(err)
	}
	now := time.Now()
	if err := logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"state":         model.UserTagEventOutboxStateDone,
			"dispatched_at": &now,
			"next_retry_at": nil,
			"locked_by":     "",
			"last_error":    "",
			"updated_at":    now,
		}).Error; err != nil {
		return 0, errors.Tag(err)
	}
	return len(rows), nil
}

// claimEventOutboxRows 领取一批当前可派发的事件 outbox。
func (r *TagRepository) claimEventOutboxRows(ctx context.Context, logDB *gorm.DB, opts types.RuntimeOptions) ([]model.UserTagEventOutbox, error) {
	now := time.Now()
	rows := make([]model.UserTagEventOutbox, 0, positiveBatchSize(opts.BatchSize))
	err := logDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.UserTagEventOutbox{}).
			Where("state IN ?", []model.UserTagEventOutboxState{model.UserTagEventOutboxStatePending, model.UserTagEventOutboxStateRetry}).
			Where("(next_retry_at IS NULL OR next_retry_at <= ?)", now).
			Order("id ASC").
			Limit(positiveBatchSize(opts.BatchSize)).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		var err error
		query, err = r.applyEventOutboxScope(query, opts)
		if err != nil {
			return errors.Tag(err)
		}
		if err = query.Find(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		result := tx.Model(&model.UserTagEventOutbox{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"state":      model.UserTagEventOutboxStateRunning,
				"attempt":    gorm.Expr("attempt + 1"),
				"locked_at":  &now,
				"locked_by":  eventOutboxLocker(opts),
				"updated_at": now,
			})
		if result.Error != nil {
			return errors.Tag(result.Error)
		}
		if result.RowsAffected != int64(len(rows)) {
			return errors.Errorf("用户标签事件 outbox 领取数量不一致 expect=%d actual=%d", len(rows), result.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// recoverRunningEventOutboxRows 回收超时 running 事件，避免 worker 异常退出后永久卡住。
func (r *TagRepository) recoverRunningEventOutboxRows(ctx context.Context, logDB *gorm.DB, opts types.RuntimeOptions, cutoff time.Time) error {
	ids := make([]int64, 0, positiveBatchSize(opts.BatchSize))
	query := logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
		Select("id").
		Where("state = ?", model.UserTagEventOutboxStateRunning).
		Where("(locked_at IS NULL OR locked_at < ?)", cutoff).
		Order("id ASC").
		Limit(positiveBatchSize(opts.BatchSize))
	var err error
	query, err = r.applyEventOutboxScope(query, opts)
	if err != nil {
		return errors.Tag(err)
	}
	if err = query.Find(&ids).Error; err != nil {
		return errors.Tag(err)
	}
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	if err = logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
		Where("id IN ? AND state = ?", ids, model.UserTagEventOutboxStateRunning).
		Updates(map[string]any{
			"state":         model.UserTagEventOutboxStateRetry,
			"next_retry_at": nil,
			"locked_by":     "",
			"last_error":    "running 事件租约超时，已回收等待重派",
			"updated_at":    now,
		}).Error; err != nil {
		return errors.Tag(err)
	}
	return nil
}

// applyEventOutboxScope 为事件 outbox 查询追加 workflow、分片和 UID 边界。
func (r *TagRepository) applyEventOutboxScope(query *gorm.DB, opts types.RuntimeOptions) (*gorm.DB, error) {
	if strings.TrimSpace(opts.WorkflowID) != "" {
		query = query.Where("workflow_id = ?", opts.WorkflowID)
	}
	if opts.ShardTotal > 1 {
		shard := r.deps.ShardPlan.NormalizeShard(opts.ShardIndex, opts.ShardTotal)
		condition, err := r.deps.ShardPlan.IndexedUIDCondition("uid", "shard_no", shard)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if condition.Expr != "" {
			query = query.Where(condition.Expr, condition.Args...)
		}
	}
	if len(opts.UIDs) > 0 {
		query = query.Where("uid IN ?", opts.UIDs)
	}
	return query, nil
}

// ensureResultShardTable 按需创建用户标签结果物理分表。
func (r *TagRepository) ensureResultShardTable(ctx context.Context, db *gorm.DB, table string) error {
	if db == nil {
		return errors.New("用户标签结果表数据库连接为空")
	}
	if table == model.UserTagShardTableName(0) {
		return nil
	}
	return errors.Tag(db.WithContext(ctx).Exec(userTagCreateLikeTableSQL(table, model.UserTagShardTableName(0))).Error)
}

// markEventOutboxFailed 标记事件派发失败并设置下一次重试时间。
func (r *TagRepository) markEventOutboxFailed(ctx context.Context, logDB *gorm.DB, rows []model.UserTagEventOutbox, runErr error) error {
	now := time.Now()
	for _, row := range rows {
		attempt := row.Attempt + 1
		state := model.UserTagEventOutboxStateRetry
		var nextRetryAt *time.Time
		if attempt >= userTagEventOutboxMaxAttempt {
			state = model.UserTagEventOutboxStateDead
		} else {
			next := now.Add(time.Duration(attempt) * time.Minute)
			nextRetryAt = &next
		}
		if err := logDB.WithContext(ctx).Model(&model.UserTagEventOutbox{}).
			Where("id = ?", row.ID).
			Updates(map[string]any{
				"state":         state,
				"attempt":       attempt,
				"next_retry_at": nextRetryAt,
				"locked_by":     "",
				"last_error":    truncateString(runErr.Error(), 1000),
				"updated_at":    now,
			}).Error; err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// eventOutboxRowToChange 把 outbox 行转换为 hook 入参。
func eventOutboxRowToChange(row model.UserTagEventOutbox) types.TagChange {
	return types.TagChange{
		EventID:    row.EventID,
		WorkflowID: row.WorkflowID,
		UID:        row.UID,
		TagType:    row.TagType,
		Action:     row.Action,
		Source:     int(row.TagSource),
		Payload:    row.Payload,
	}
}

// eventOutboxLocker 返回当前 outbox 领取者标识。
func eventOutboxLocker(opts types.RuntimeOptions) string {
	workflowID := strings.TrimSpace(opts.WorkflowID)
	if workflowID == "" {
		return "event_outbox_retry_scan"
	}
	return workflowID
}

// WorkflowShardUIDs 返回当前 workflow 分片负责的 UID 集合。
func (r *TagRepository) WorkflowShardUIDs(opts types.RuntimeOptions, uids []int64) []int64 {
	return filterUIDsByShard(uniqueInt64s(uids), opts.ShardIndex, opts.ShardTotal)
}

// logDB 返回用户标签写库连接。
func (r *TagRepository) logDB() (*gorm.DB, error) {
	if r == nil || r.deps.DBs.MainDB == nil {
		return nil, errors.Errorf("主库连接为空")
	}
	return r.deps.DBs.MainDB, nil
}

// waitUserTagRuntimeBatch 在批量清理之间等待，避免长时间压住数据库。
func waitUserTagRuntimeBatch(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Tag(ctx.Err())
	case <-timer.C:
		return nil
	}
}

// positiveBatchSize 返回安全批次大小。
func positiveBatchSize(value int) int {
	if value > 0 {
		return value
	}
	return userTagRuntimeCleanupBatchSize
}

// truncateString 截断超长错误摘要。
func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

// simpleIdentPattern 限制动态表名和列名只能使用安全标识符。
var simpleIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// quoteIdent 返回安全 MySQL 标识符。
func quoteIdent(name string) string {
	name = strings.TrimSpace(name)
	if !simpleIdentPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
