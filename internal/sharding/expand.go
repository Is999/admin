package sharding

import (
	"sort"

	"github.com/Is999/go-utils/errors"
)

const (
	// MaxExpansionRatio 固定为翻倍扩容，确保每张源表每轮只对应一个迁移区间。
	MaxExpansionRatio = 2
)

// Move 表示扩容时从旧物理表迁往新物理表的一段固定桶。
type Move struct {
	Source      string `json:"source"`       // 旧物理表
	Target      string `json:"target"`       // 新物理表
	BucketStart int    `json:"bucket_start"` // 迁移起始桶，包含
	BucketEnd   int    `json:"bucket_end"`   // 迁移结束桶，包含
}

// ExpandMoves 返回从旧分片数扩容到新分片数所需迁移的固定桶区间。
func ExpandMoves(firstTable string, fromCount, toCount int) ([]Move, error) {
	fromPlan, err := NewPlan(firstTable, fromCount)
	if err != nil {
		return nil, errors.Tag(err)
	}
	toPlan, err := NewPlan(firstTable, toCount)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if toCount <= fromCount || toCount%fromCount != 0 {
		return nil, errors.Errorf("目标物理分片数必须是当前分片数的更大整数倍 from=%d to=%d", fromCount, toCount)
	}
	if toCount/fromCount > MaxExpansionRatio {
		return nil, errors.Errorf("单次物理扩容最多放大 %d 倍，请分阶段执行 from=%d to=%d", MaxExpansionRatio, fromCount, toCount)
	}
	moves := make([]Move, 0, toCount-fromCount)
	for _, target := range toPlan.Tables() {
		source, sourceErr := fromPlan.TableForBucket(target.BucketStart)
		if sourceErr != nil {
			return nil, errors.Tag(sourceErr)
		}
		if source.Name == target.Name {
			continue
		}
		moves = append(moves, Move{
			Source:      source.Name,
			Target:      target.Name,
			BucketStart: target.BucketStart,
			BucketEnd:   target.BucketEnd,
		})
	}
	sort.Slice(moves, func(i, j int) bool {
		return moves[i].BucketStart < moves[j].BucketStart
	})
	return moves, nil
}
