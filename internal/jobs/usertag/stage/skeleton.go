package stage

import (
	"admin/internal/jobs/usertag/queryplan"
	"admin/internal/jobs/usertag/runtimectx"
	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
)

// SkeletonStage 表示只承载工作流编排的用户标签骨架阶段。
// 该阶段不读取具体业务事实表、不写标签结果，只验证节点注册、调度和状态流转。
type SkeletonStage struct {
	Base // 基础阶段信息
}

// NewCollectScopeStage 创建候选范围收集骨架阶段。
func NewCollectScopeStage() *SkeletonStage {
	return newSkeletonStage(types.NodeCollectScope, []Dependency{
		{Name: "user_tag_runtime_uid", Reason: "承载本次 workflow 的候选 UID 集合"},
		{Name: "user_tag_runtime_checkpoint", Reason: "记录范围收集游标和节点进度"},
	})
}

// NewEvaluateTagsStage 创建标签规则评估骨架阶段。
func NewEvaluateTagsStage() *SkeletonStage {
	return newSkeletonStage(types.NodeEvaluateTags, []Dependency{
		{Name: "user_tag_runtime_uid", Reason: "按候选 UID 边界执行规则评估"},
		{Name: "user_tag_%d_tmp", Reason: "full 模式写入临时结果分片表"},
	})
}

// NewResolveChangesStage 创建标签得失差异解析骨架阶段。
func NewResolveChangesStage() *SkeletonStage {
	return newSkeletonStage(types.NodeResolveChanges, []Dependency{
		{Name: "user_tag_%d", Reason: "读取当前标签基线用于差异比对"},
		{Name: "user_tag_event_outbox", Reason: "承载后续 gain/lost hook 事件"},
	})
}

// NewPersistResultsStage 创建标签结果持久化骨架阶段。
func NewPersistResultsStage() *SkeletonStage {
	return newSkeletonStage(types.NodePersistResults, []Dependency{
		{Name: "user_tag_%d", Reason: "delta/targeted/recalculate 写入最终结果"},
		{Name: "user_tag_event_outbox", Reason: "写入标签得到和失去事件"},
	})
}

// newSkeletonStage 创建只返回跳过态的用户标签骨架阶段。
func newSkeletonStage(name string, deps []Dependency) *SkeletonStage {
	return &SkeletonStage{Base: Base{StageName: name, Deps: deps}}
}

// Plans 返回骨架阶段查询计划，骨架阶段不声明业务查询。
func (s *SkeletonStage) Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error) {
	return nil, nil
}

// Run 执行骨架阶段。
func (s *SkeletonStage) Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) {
	if s == nil || s.StageName == "" {
		return Result{}, errors.Errorf("用户标签骨架阶段名称不能为空")
	}
	return Result{Skipped: true}, nil
}
