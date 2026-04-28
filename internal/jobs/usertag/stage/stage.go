package stage

import (
	"admin/internal/jobs/usertag/queryplan"
	"admin/internal/jobs/usertag/runtimectx"
)

// Dependency 描述阶段依赖的数据域或前置阶段。
type Dependency struct {
	Name   string // 依赖名称
	Reason string // 依赖原因，便于文档和日志排查
}

// Result 描述阶段执行结果。
type Result struct {
	Scanned int64 // 扫描记录数
	Updated int64 // 更新记录数
	Skipped bool  // 是否按标签依赖跳过
}

// Stage 是 usertag 可组合计算阶段的统一接口。
type Stage interface {
	Name() string                                                      // 阶段名称
	Dependencies() []Dependency                                        // 阶段依赖
	Plans(ctx *runtimectx.Context) ([]queryplan.Plan, error)           // 阶段声明的查询计划
	Run(ctx *runtimectx.Context, plans map[string]any) (Result, error) // 执行阶段逻辑
}

// Base 提供阶段名称和依赖的基础实现。
type Base struct {
	StageName string       // 阶段名称
	Deps      []Dependency // 阶段依赖列表
}

// Name 返回阶段名称。
func (b Base) Name() string {
	return b.StageName
}

// Dependencies 返回阶段依赖。
func (b Base) Dependencies() []Dependency {
	return append([]Dependency(nil), b.Deps...)
}
