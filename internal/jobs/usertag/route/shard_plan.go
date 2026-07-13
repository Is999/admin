package route

import (
	"admin/common/idgen"
	"admin/internal/model"
)

const (
	// defaultShardTotal 是工作流任务拆分兜底分片数。
	defaultShardTotal = 1
)

// Shard 表示一次工作流执行中的分片信息。
type Shard struct {
	Index int // 分片下标
	Total int // 分片总数
}

// Condition 表示可交给 GORM Where 使用的安全条件。
type Condition struct {
	Expr string // SQL 条件表达式
	Args []any  // SQL 条件参数
}

// ShardPlan 统一 usertag 的 UID 分片、运行期索引分片和标签结果分片口径。
type ShardPlan struct {
	ShardTotal       int // 工作流默认分片数
	ResultShardTotal int // 标签结果物理分片数
}

// NewShardPlan 创建使用单个逻辑结果表的分片计划。
func NewShardPlan(shardTotal int) ShardPlan {
	return NewShardPlanWithResult(shardTotal, defaultShardTotal)
}

// NewShardPlanWithResult 创建工作流和标签结果分片计划。
func NewShardPlanWithResult(shardTotal, resultShardTotal int) ShardPlan {
	return ShardPlan{
		ShardTotal:       positiveOr(shardTotal, defaultShardTotal),
		ResultShardTotal: positiveOr(resultShardTotal, defaultShardTotal),
	}
}

// NormalizeShard 规范化分片信息，避免负数或越界分片进入查询层。
func (p ShardPlan) NormalizeShard(index, total int) Shard {
	total = positiveOr(total, p.ShardTotal)
	index %= total
	if index < 0 {
		index += total
	}
	return Shard{Index: index, Total: total}
}

// UIDShard 返回 UID 固定逻辑桶映射到工作分片后的编号。
func (p ShardPlan) UIDShard(uid int64, shardTotal int) int {
	total := positiveOr(shardTotal, p.ShardTotal)
	return idgen.ShardNo(uid) * total / idgen.ShardMod
}

// UIDBucket 返回和 user.shard_no 完全一致的 0-1023 固定逻辑桶。
func (p ShardPlan) UIDBucket(uid int64) int {
	return idgen.ShardNo(uid)
}

// ResultTable 返回 UID 对应的标签结果表名。
func (p ShardPlan) ResultTable(uid int64) (string, error) {
	return model.UserTagPhysicalTableName(p.UIDBucket(uid), p.ResultShardTotal)
}

// IndexedUIDCondition 返回携带固定 shard_no 连续范围的 UID 表分片条件。
func (p ShardPlan) IndexedUIDCondition(shard Shard) Condition {
	normalized := p.NormalizeShard(shard.Index, shard.Total)
	if normalized.Total <= 1 {
		return Condition{}
	}
	start, end := p.shardBucketRange(normalized)
	if start == end {
		return Condition{Expr: "shard_no = ?", Args: []any{start}}
	}
	return Condition{Expr: "shard_no BETWEEN ? AND ?", Args: []any{start, end}}
}

// shardBucketRange 返回工作分片负责的闭区间固定桶范围。
func (p ShardPlan) shardBucketRange(shard Shard) (int, int) {
	start := divideCeil(shard.Index*idgen.ShardMod, shard.Total)
	end := divideCeil((shard.Index+1)*idgen.ShardMod, shard.Total) - 1
	return start, end
}

// positiveOr 返回正数配置，非正数时使用兜底值。
func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// divideCeil 返回正整数除法的向上取整结果。
func divideCeil(value, divisor int) int {
	return (value + divisor - 1) / divisor
}
