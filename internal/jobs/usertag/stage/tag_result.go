package stage

import (
	"admin_cron/internal/jobs/usertag/queryplan"
	"admin_cron/internal/jobs/usertag/repository"
	"admin_cron/internal/jobs/usertag/runtimectx"
	"admin_cron/internal/jobs/usertag/types"
)

// PrepareStage 负责清理当前工作流运行期状态，并在 full 模式清空预建临时表。
type PrepareStage struct {
	Base
	repo *repository.TagRepository // 标签结果仓储
}

// NewPrepareStage 创建准备阶段。
func NewPrepareStage(repo *repository.TagRepository) *PrepareStage {
	return &PrepareStage{
		Base: Base{StageName: types.NodePrepare},
		repo: repo,
	}
}

// Plans 返回准备阶段查询计划，准备阶段不需要预取查询。
func (s *PrepareStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行准备阶段。
func (s *PrepareStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if err := s.repo.ResetRuntimeState(ctx.Context, ctx.WorkflowID()); err != nil {
		return Result{}, ctx.Wrap(err, "清理运行期状态失败")
	}
	if err := s.repo.PrepareResultTables(ctx.Context, ctx.Options); err != nil {
		return Result{}, ctx.Wrap(err, "准备结果表失败")
	}
	return Result{}, nil
}

// BusinessHookStage 是业务扩展占位阶段。
type BusinessHookStage struct {
	Base
}

// NewBusinessHookStage 创建业务扩展占位阶段。
func NewBusinessHookStage() *BusinessHookStage {
	return &BusinessHookStage{Base: Base{StageName: types.NodeBusinessHook}}
}

// Plans 返回业务扩展占位阶段查询计划。
func (s *BusinessHookStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行业务扩展占位阶段。
func (s *BusinessHookStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if ctx.Options.DryRun {
		return Result{Skipped: true}, nil
	}
	return Result{Skipped: true}, nil
}

// FinalizeStage 负责 full 模式最终切表。
type FinalizeStage struct {
	Base
	repo *repository.TagRepository // 标签结果仓储
}

// NewFinalizeStage 创建最终提交阶段。
func NewFinalizeStage(repo *repository.TagRepository) *FinalizeStage {
	return &FinalizeStage{Base: Base{StageName: types.NodeFinalize}, repo: repo}
}

// Plans 返回最终提交阶段查询计划。
func (s *FinalizeStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行最终提交。
func (s *FinalizeStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if err := s.repo.FinalizeResultTables(ctx.Context, ctx.Options); err != nil {
		return Result{}, ctx.Wrap(err, "切换最终标签表失败")
	}
	return Result{}, nil
}

// SyncKafkaStage 负责 full finalize 后重建同步快照。
type SyncKafkaStage struct {
	Base
	repo *repository.TagRepository // 标签结果仓储
}

// NewSyncKafkaStage 创建同步阶段。
func NewSyncKafkaStage(repo *repository.TagRepository) *SyncKafkaStage {
	return &SyncKafkaStage{Base: Base{StageName: types.NodeSyncKafka}, repo: repo}
}

// Plans 返回同步阶段查询计划。
func (s *SyncKafkaStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行 full 同步快照重建。
func (s *SyncKafkaStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if ctx.Options.Mode != types.ModeFull {
		return Result{Skipped: true}, nil
	}
	count, err := s.repo.RebuildSyncSnapshotShard(ctx.Context, ctx.Options)
	if err != nil {
		return Result{}, ctx.Wrap(err, "重建同步快照失败")
	}
	discarded, err := s.repo.DiscardKafkaOutboxShard(ctx.Context, ctx.Options)
	if err != nil {
		return Result{}, ctx.Wrap(err, "清理全量 Kafka outbox 失败")
	}
	return Result{Updated: int64(count) + discarded}, nil
}
