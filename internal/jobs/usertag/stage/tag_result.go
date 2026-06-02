package stage

import (
	"admin/internal/jobs/usertag/hook"
	"admin/internal/jobs/usertag/queryplan"
	"admin/internal/jobs/usertag/repository"
	"admin/internal/jobs/usertag/runtimectx"
	"admin/internal/jobs/usertag/types"
)

// PrepareStage 负责清理当前工作流运行期状态，并在 full 模式清空预建临时表。
type PrepareStage struct {
	Base                           // 阶段基础信息，提供统一阶段名
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

// FinalizeStage 负责 full 模式最终切表。
type FinalizeStage struct {
	Base                           // 阶段基础信息，提供统一阶段名
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

// DispatchHooksStage 负责派发标签得到和失去事件 hook。
type DispatchHooksStage struct {
	Base                           // 阶段基础信息，提供统一阶段名
	repo *repository.TagRepository // 标签结果仓储
}

// NewDispatchHooksStage 创建事件 hook 派发阶段。
func NewDispatchHooksStage(repo *repository.TagRepository) *DispatchHooksStage {
	return &DispatchHooksStage{Base: Base{StageName: types.NodeDispatchHooks}, repo: repo}
}

// Plans 返回事件 hook 派发阶段查询计划。
func (s *DispatchHooksStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行当前分片事件 hook 派发。
func (s *DispatchHooksStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if !ctx.Options.EventHookEnabled {
		return Result{Skipped: true}, nil
	}
	count, err := s.repo.DrainEventOutboxShard(ctx.Context, ctx.Options, hook.DefaultRegistry().Dispatch)
	if err != nil {
		return Result{}, ctx.Wrap(err, "派发标签事件 hook 失败")
	}
	return Result{Updated: int64(count), Skipped: count == 0}, nil
}
