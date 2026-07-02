package route

import (
	"fmt"
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// defaultShardTotal 是工作流任务拆分兜底分片数。
	defaultShardTotal = 1
	// defaultRuntimeShardTotal 是运行期 UID 索引默认分片数。
	defaultRuntimeShardTotal = 1024
	// defaultResultShardTotal 是用户标签结果表默认物理分片数。
	defaultResultShardTotal = 1
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
	ShardTotal        int // 工作流默认分片数
	RuntimeShardTotal int // 运行期 UID 索引分片数量
	ResultShardTotal  int // 标签结果物理分表数量
}

// NewShardPlan 创建分片计划，非法配置统一回退到默认值。
func NewShardPlan(shardTotal, runtimeShardTotal int) ShardPlan {
	return NewShardPlanWithResult(shardTotal, runtimeShardTotal, defaultResultShardTotal)
}

// NewShardPlanWithResult 创建包含结果物理分表数量的分片计划。
func NewShardPlanWithResult(shardTotal, runtimeShardTotal, resultShardTotal int) ShardPlan {
	return ShardPlan{
		ShardTotal:        positiveOr(shardTotal, defaultShardTotal),
		RuntimeShardTotal: positiveOr(runtimeShardTotal, defaultRuntimeShardTotal),
		ResultShardTotal:  positiveOr(resultShardTotal, defaultResultShardTotal),
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

// UIDShard 返回 UID 在指定分片总数下的稳定分片。
func (p ShardPlan) UIDShard(uid int64, shardTotal int) int {
	total := positiveOr(shardTotal, p.ShardTotal)
	shard := int(uid % int64(total))
	if shard < 0 {
		shard += total
	}
	return shard
}

// TagShardsForWorkflow 返回当前工作流分片需要处理的最终标签物理分表。
func (p ShardPlan) TagShardsForWorkflow(shardIndex, shardTotal int) []int {
	return p.physicalShardsForWorkflow(shardIndex, shardTotal, p.ResultShardTotal)
}

// UIDModuloCondition 返回 UID 取模分片条件。
func (p ShardPlan) UIDModuloCondition(column string, shard Shard) (Condition, error) {
	if err := validateColumn(column); err != nil {
		return Condition{}, errors.Tag(err)
	}
	normalized := p.NormalizeShard(shard.Index, shard.Total)
	return Condition{
		Expr: fmt.Sprintf("MOD(%s, ?) = ?", column),
		Args: []any{normalized.Total, normalized.Index},
	}, nil
}

// IndexedUIDCondition 返回 runtime_uid 这类单表索引分片条件。
// 工作流分片能映射到 runtime_shard_total 时优先走 shard_no，无法整除时回退 UID 取模。
func (p ShardPlan) IndexedUIDCondition(uidColumn, shardColumn string, shard Shard) (Condition, error) {
	if err := validateColumn(uidColumn); err != nil {
		return Condition{}, errors.Tag(err)
	}
	if err := validateColumn(shardColumn); err != nil {
		return Condition{}, errors.Tag(err)
	}
	normalized := p.NormalizeShard(shard.Index, shard.Total)
	if normalized.Total <= 1 {
		return Condition{}, nil
	}
	if normalized.Total == p.RuntimeShardTotal {
		return Condition{Expr: fmt.Sprintf("%s = ?", shardColumn), Args: []any{normalized.Index}}, nil
	}
	if p.RuntimeShardTotal > normalized.Total && p.RuntimeShardTotal%normalized.Total == 0 {
		return Condition{Expr: fmt.Sprintf("%s IN ?", shardColumn), Args: []any{p.indexedShardValues(normalized)}}, nil
	}
	return Condition{
		Expr: fmt.Sprintf("MOD(%s, ?) = ?", uidColumn),
		Args: []any{normalized.Total, normalized.Index},
	}, nil
}

// indexedShardValues 返回当前工作流分片覆盖的 runtime shard_no 集合。
func (p ShardPlan) indexedShardValues(shard Shard) []int {
	values := make([]int, 0, p.RuntimeShardTotal/shard.Total)
	for shardNo := shard.Index; shardNo < p.RuntimeShardTotal; shardNo += shard.Total {
		values = append(values, shardNo)
	}
	return values
}

// validateColumn 校验列名只包含简单标识符和点号，避免把外部输入拼进 SQL。
func validateColumn(column string) error {
	column = strings.TrimSpace(column)
	if column == "" {
		return errors.Errorf("分片列不能为空")
	}
	for _, r := range column {
		if r == '_' || r == '.' || r == '`' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		return errors.Errorf("分片列包含非法字符 column=%s", column)
	}
	return nil
}

// physicalShardsForWorkflow 计算当前工作流分片覆盖的物理分表。
// 只有 physical_shard 与 workflow_shard 在 gcd(workflow_total, physical_total) 下同余时，该物理表才可能包含当前分片 UID。
func (p ShardPlan) physicalShardsForWorkflow(shardIndex, shardTotal, physicalTotal int) []int {
	physicalTotal = positiveOr(physicalTotal, defaultResultShardTotal)
	workflow := p.NormalizeShard(shardIndex, shardTotal)
	divisor := gcd(workflow.Total, physicalTotal)
	out := make([]int, 0, physicalTotal)
	for shard := 0; shard < physicalTotal; shard++ {
		if shard%divisor == workflow.Index%divisor {
			out = append(out, shard)
		}
	}
	return out
}

// gcd 返回两个正整数的最大公约数。
func gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if a == 0 {
		return positiveOr(b, 1)
	}
	if b == 0 {
		return positiveOr(a, 1)
	}
	for b != 0 {
		a, b = b, a%b
	}
	return positiveOr(a, 1)
}

// positiveOr 返回正数配置，非正数时使用兜底值。
func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
