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
)

const (
	userTagRuntimeUIDRetention       = 7 * 24 * time.Hour     // runtime_uid 只保留近期 workflow 运行期集合
	userTagKafkaOutboxDoneRetention  = 7 * 24 * time.Hour     // 已成功推送 outbox 保留时间
	userTagKafkaOutboxDeadRetention  = 30 * 24 * time.Hour    // 死信 outbox 保留时间
	userTagKafkaOutboxStaleRetention = 7 * 24 * time.Hour     // 长期未被 drain 的非终态 outbox 超时后转死信
	userTagRuntimeResetBatchSize     = 1000                   // 当前 workflow 清理单批行数
	userTagRuntimeResetBatchDelay    = 20 * time.Millisecond  // 当前 workflow 清理批次间隔
	userTagRuntimeCleanupBatchSize   = 500                    // 历史运行期表清理单批行数
	userTagRuntimeCleanupBatchDelay  = 100 * time.Millisecond // 历史运行期表清理批次间隔
	userTagRuntimeCleanupTimeBudget  = 55 * time.Minute       // 单次独立清理任务总时间预算
)

// TagRepository 提供用户标签工作流骨架所需的结果表、运行期表和同步表访问能力。
type TagRepository struct {
	deps RuntimeDeps // 运行依赖
}

// NewTagRepository 创建用户标签结果仓储。
func NewTagRepository(deps RuntimeDeps) *TagRepository {
	return &TagRepository{deps: deps}
}

// PrepareResultTables 清空 full 模式临时标签结果表。
func (r *TagRepository) PrepareResultTables(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun || opts.Mode != types.ModeFull {
		return nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return errors.Tag(err)
	}
	for _, shard := range r.deps.ShardPlan.TagShardsForWorkflow(opts.ShardIndex, opts.ShardTotal) {
		currentTable := model.UserTagShardTableName(shard)
		tmpTable := model.UserTagTmpShardTableName(shard)
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
	if opts.DryRun || opts.Mode != types.ModeFull {
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

// ResetRuntimeState 清理当前工作流的运行期 UID 和 Kafka outbox。
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
	if err := r.resetKafkaOutboxRowsByWorkflow(ctx, logDB, workflowID); err != nil {
		return errors.Tag(err)
	}
	return nil
}

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

func (r *TagRepository) resetKafkaOutboxRowsByWorkflow(ctx context.Context, logDB *gorm.DB, workflowID string) error {
	for {
		ids := make([]int64, 0, userTagRuntimeResetBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagKafkaOutbox{}).
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
		if err := logDB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.UserTagKafkaOutbox{}).Error; err != nil {
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
	if err := r.cleanupKafkaOutboxRows(cleanupCtx, logDB, []model.UserTagKafkaOutboxState{model.UserTagKafkaOutboxStateDone}, now.Add(-userTagKafkaOutboxDoneRetention)); err != nil {
		return errors.Wrapf(err, "清理 done outbox 失败")
	}
	if err := r.cleanupKafkaOutboxRows(cleanupCtx, logDB, []model.UserTagKafkaOutboxState{model.UserTagKafkaOutboxStateDead}, now.Add(-userTagKafkaOutboxDeadRetention)); err != nil {
		return errors.Wrapf(err, "清理 dead outbox 失败")
	}
	return r.cleanupKafkaOutboxRows(cleanupCtx, logDB, []model.UserTagKafkaOutboxState{model.UserTagKafkaOutboxStatePending, model.UserTagKafkaOutboxStateRetry, model.UserTagKafkaOutboxStateRunning}, now.Add(-userTagKafkaOutboxStaleRetention))
}

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

func (r *TagRepository) cleanupKafkaOutboxRows(ctx context.Context, logDB *gorm.DB, states []model.UserTagKafkaOutboxState, cutoff time.Time) error {
	for {
		ids := make([]int64, 0, userTagRuntimeCleanupBatchSize)
		if err := logDB.WithContext(ctx).Model(&model.UserTagKafkaOutbox{}).
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
		if err := logDB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.UserTagKafkaOutbox{}).Error; err != nil {
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
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = userTagRuntimeCleanupBatchSize
	}
	uids := make([]int64, 0, batchSize)
	query := logDB.WithContext(ctx).Table(model.TableNameUserTagRuntimeUID).
		Select("uid").
		Where("workflow_id = ? AND uid > ?", opts.WorkflowID, lastUID).
		Where(condition.Expr, condition.Args...).
		Order("uid ASC").
		Limit(batchSize)
	if err := query.Find(&uids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return uids, nil
}

// BuildKafkaOutboxForUIDs 是用户标签变更 outbox 的预留构造入口。
func (r *TagRepository) BuildKafkaOutboxForUIDs(ctx context.Context, opts types.RuntimeOptions, uids []int64) (int, error) {
	return 0, nil
}

// RetryKafkaOutboxAbnormalRows 周期扫描并重推异常 Kafka outbox。
func (r *TagRepository) RetryKafkaOutboxAbnormalRows(ctx context.Context, opts types.RuntimeOptions, push func([]model.UserTagMessage) error) (int, error) {
	return r.DrainKafkaOutboxShard(ctx, opts, push)
}

// DrainKafkaOutboxShard 推送当前工作流 outbox，并在推送成功后标记完成。
func (r *TagRepository) DrainKafkaOutboxShard(ctx context.Context, opts types.RuntimeOptions, push func([]model.UserTagMessage) error) (int, error) {
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	rows := make([]model.UserTagKafkaOutbox, 0, opts.BatchSize)
	query := logDB.WithContext(ctx).Model(&model.UserTagKafkaOutbox{}).
		Where("state IN ?", []model.UserTagKafkaOutboxState{model.UserTagKafkaOutboxStatePending, model.UserTagKafkaOutboxStateRetry}).
		Order("id ASC").
		Limit(positiveBatchSize(opts.BatchSize))
	if strings.TrimSpace(opts.WorkflowID) != "" {
		query = query.Where("workflow_id = ?", opts.WorkflowID)
	}
	if opts.ShardTotal > 0 {
		shard := r.deps.ShardPlan.NormalizeShard(opts.ShardIndex, opts.ShardTotal)
		query = query.Where("shard_no = ?", shard.Index)
	}
	if err := query.Find(&rows).Error; err != nil {
		return 0, errors.Tag(err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	messages := make([]model.UserTagMessage, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		messages = append(messages, model.UserTagMessage{
			ActionType: row.ActionType,
			UID:        row.UID,
			TagID:      row.TagType,
			EventID:    row.WorkflowID,
			Source:     "admin.user_tag",
		})
	}
	if push != nil {
		if err := push(messages); err != nil {
			_ = logDB.WithContext(ctx).Model(&model.UserTagKafkaOutbox{}).
				Where("id IN ?", ids).
				Updates(map[string]any{"state": model.UserTagKafkaOutboxStateRetry, "attempt": gorm.Expr("attempt + 1"), "last_error": truncateString(err.Error(), 1000), "updated_at": time.Now()}).Error
			return 0, errors.Tag(err)
		}
	}
	now := time.Now()
	if err := logDB.WithContext(ctx).Model(&model.UserTagKafkaOutbox{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"state": model.UserTagKafkaOutboxStateDone, "sent_at": &now, "updated_at": now}).Error; err != nil {
		return 0, errors.Tag(err)
	}
	return len(rows), nil
}

// DiscardKafkaOutboxShard 清理当前 workflow 分片内已生成但不需要推送的 outbox。
func (r *TagRepository) DiscardKafkaOutboxShard(ctx context.Context, opts types.RuntimeOptions) (int64, error) {
	if strings.TrimSpace(opts.WorkflowID) == "" {
		return 0, nil
	}
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	query := logDB.WithContext(ctx).Where("workflow_id = ?", opts.WorkflowID)
	if opts.ShardTotal > 0 {
		shard := r.deps.ShardPlan.NormalizeShard(opts.ShardIndex, opts.ShardTotal)
		query = query.Where("shard_no = ?", shard.Index)
	}
	result := query.Delete(&model.UserTagKafkaOutbox{})
	return result.RowsAffected, errors.Tag(result.Error)
}

// RebuildSyncSnapshotShard 重建当前执行分片负责的同步快照。
func (r *TagRepository) RebuildSyncSnapshotShard(ctx context.Context, opts types.RuntimeOptions) (int, error) {
	logDB, err := r.logDB()
	if err != nil {
		return 0, errors.Tag(err)
	}
	total := 0
	for _, shard := range r.deps.ShardPlan.TagShardsForWorkflow(opts.ShardIndex, opts.ShardTotal) {
		sourceTable := model.UserTagShardTableName(shard)
		targetTable := model.UserTagSyncShardTableName(shard)
		if err := logDB.WithContext(ctx).Exec(userTagCreateLikeTableSQL(targetTable, sourceTable)).Error; err != nil {
			return total, errors.Wrapf(err, "创建用户标签同步快照表失败 table=%s", targetTable)
		}
		if err := logDB.WithContext(ctx).Exec(userTagTruncateTableSQL(targetTable)).Error; err != nil {
			return total, errors.Wrapf(err, "清空用户标签同步快照表失败 table=%s", targetTable)
		}
		result := logDB.WithContext(ctx).Exec(
			"INSERT INTO " + quoteIdent(targetTable) + " (uid, tag_type, tag_source, tag_data, tag_category, created_at, updated_at) " +
				"SELECT uid, tag_type, tag_source, tag_data, tag_category, created_at, updated_at FROM " + quoteIdent(sourceTable),
		)
		if result.Error != nil {
			return total, errors.Wrapf(result.Error, "重建用户标签同步快照失败 table=%s", targetTable)
		}
		total += int(result.RowsAffected)
	}
	return total, nil
}

// WorkflowShardUIDs 返回当前 workflow 分片负责的 UID 集合。
func (r *TagRepository) WorkflowShardUIDs(opts types.RuntimeOptions, uids []int64) []int64 {
	return filterUIDsByShard(uniqueInt64s(uids), opts.ShardIndex, opts.ShardTotal)
}

func (r *TagRepository) logDB() (*gorm.DB, error) {
	if r == nil || r.deps.DBs.MainDB == nil {
		return nil, errors.Errorf("主库连接为空")
	}
	return r.deps.DBs.MainDB, nil
}

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

func positiveBatchSize(value int) int {
	if value > 0 {
		return value
	}
	return userTagRuntimeCleanupBatchSize
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

var simpleIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quoteIdent(name string) string {
	name = strings.TrimSpace(name)
	if !simpleIdentPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
