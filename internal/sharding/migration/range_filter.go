// Package migration 提供应用内物理拆表的在线复制和收口能力。
package migration

import (
	"fmt"
	"math"

	"admin/common/idgen"
	"admin/internal/sharding"

	"github.com/Is999/go-utils/errors"
	sq "github.com/Masterminds/squirrel"
	"github.com/Shopify/ghostferry"
)

// BucketRange 表示一张源物理表需要复制的固定桶闭区间。
type BucketRange struct {
	Start int64 // 起始桶，包含
	End   int64 // 结束桶，包含
}

// BucketRangeFilter 按源物理表复制指定固定桶区间，并同步筛选对应 binlog 事件。
type BucketRangeFilter struct {
	UIDColumn   string                 // 业务用户 ID 字段
	ShardColumn string                 // 固定逻辑桶字段
	Ranges      map[string]BucketRange // 源物理表到迁移桶区间
}

// BuildSelect 构造带固定桶范围和唯一游标的有界批量查询。
func (f BucketRangeFilter) BuildSelect(columns []string, table *ghostferry.TableSchema, lastKey ghostferry.PaginationKey, batchSize uint64) (sq.SelectBuilder, error) {
	bucketRange, err := f.rangeForTable(table.Name)
	if err != nil {
		return sq.SelectBuilder{}, err
	}
	paginationColumn := ghostferry.QuoteField(table.GetPaginationColumn().Name)
	shardColumn := ghostferry.QuoteField(f.ShardColumn)
	return sq.Select(columns...).
		From(ghostferry.QuotedTableName(table)).
		Where(sq.Gt{paginationColumn: lastKey.SQLValue()}).
		Where(sq.Expr(fmt.Sprintf("%s BETWEEN ? AND ?", shardColumn), bucketRange.Start, bucketRange.End)).
		OrderBy(paginationColumn).
		Limit(batchSize), nil
}

// ApplicableEvent 判断 binlog 事件是否属于当前迁移桶范围。
func (f BucketRangeFilter) ApplicableEvent(event ghostferry.DMLEvent) (bool, error) {
	bucketRange, err := f.rangeForTable(event.Table())
	if err != nil {
		return false, err
	}
	uidIndex := -1
	shardIndex := -1
	for index, column := range event.TableSchema().Columns {
		if column.Name == f.UIDColumn {
			uidIndex = index
		}
		if column.Name == f.ShardColumn {
			shardIndex = index
		}
	}
	if uidIndex < 0 {
		return false, errors.Errorf("表 %s 缺少业务用户 ID 字段 %s", event.Table(), f.UIDColumn)
	}
	if shardIndex < 0 {
		return false, errors.Errorf("表 %s 缺少固定桶字段 %s", event.Table(), f.ShardColumn)
	}
	oldBucket, oldExists, err := eventBucket(event.OldValues(), uidIndex, shardIndex)
	if err != nil {
		return false, errors.Wrapf(err, "校验旧行固定桶失败 table=%s", event.Table())
	}
	newBucket, newExists, err := eventBucket(event.NewValues(), uidIndex, shardIndex)
	if err != nil {
		return false, errors.Wrapf(err, "校验新行固定桶失败 table=%s", event.Table())
	}
	if oldExists && newExists && oldBucket != newBucket {
		return false, errors.Errorf("迁移期间禁止修改固定桶 table=%s old=%d new=%d", event.Table(), oldBucket, newBucket)
	}
	return oldExists && bucketRange.Contains(oldBucket) || newExists && bucketRange.Contains(newBucket), nil
}

// Validate 校验固定桶范围。
func (f BucketRangeFilter) Validate() error {
	if err := sharding.ValidateIdentifier(f.UIDColumn); err != nil {
		return errors.Wrap(err, "业务用户 ID 字段无效")
	}
	if err := sharding.ValidateIdentifier(f.ShardColumn); err != nil {
		return errors.Wrap(err, "固定桶字段无效")
	}
	if len(f.Ranges) == 0 {
		return errors.New("固定桶迁移范围不能为空")
	}
	for table, bucketRange := range f.Ranges {
		if err := sharding.ValidateIdentifier(table); err != nil {
			return errors.Wrapf(err, "源物理表无效 table=%s", table)
		}
		if err := bucketRange.Validate(); err != nil {
			return errors.Wrapf(err, "源物理表固定桶范围无效 table=%s", table)
		}
	}
	return nil
}

// Contains 判断固定桶是否属于当前闭区间。
func (r BucketRange) Contains(bucket int64) bool {
	return bucket >= r.Start && bucket <= r.End
}

// Validate 校验固定桶闭区间。
func (r BucketRange) Validate() error {
	if r.Start < 0 || r.End < r.Start || r.End >= sharding.BucketTotal {
		return errors.Errorf("固定桶范围必须位于 0..%d start=%d end=%d", sharding.BucketTotal-1, r.Start, r.End)
	}
	return nil
}

// rangeForTable 返回指定源物理表的迁移桶区间。
func (f BucketRangeFilter) rangeForTable(table string) (BucketRange, error) {
	bucketRange, exists := f.Ranges[table]
	if !exists {
		return BucketRange{}, errors.Errorf("源物理表缺少固定桶迁移范围 table=%s", table)
	}
	return bucketRange, nil
}

// eventBucket 校验一行 binlog 数据的 UID 与固定桶公式并返回固定桶。
func eventBucket(values []any, uidIndex int, shardIndex int) (int64, bool, error) {
	if values == nil {
		return 0, false, nil
	}
	bucket, exists, err := bucketValue(values, shardIndex)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, nil
	}
	expected, err := uidBucketValue(values, uidIndex)
	if err != nil {
		return 0, false, err
	}
	if bucket != expected {
		return 0, false, errors.Errorf("固定桶公式错误 shard_no=%d expected=%d", bucket, expected)
	}
	return bucket, true, nil
}

// uidBucketValue 从 Ghostferry 行数据读取正整数 UID 并计算固定桶。
func uidBucketValue(values []any, index int) (int64, error) {
	if index < 0 || index >= len(values) {
		return 0, errors.Errorf("业务用户 ID 字段下标越界 index=%d columns=%d", index, len(values))
	}
	if value, ok := ghostferry.Uint64Value(values[index]); ok {
		if value == 0 || value > math.MaxInt64 {
			return 0, errors.Errorf("业务用户 ID 超出正 int64 范围 value=%d", value)
		}
		return int64(idgen.ShardNo(int64(value))), nil
	}
	if value, ok := ghostferry.Int64Value(values[index]); ok {
		if value <= 0 {
			return 0, errors.Errorf("业务用户 ID 必须为正数 value=%d", value)
		}
		return int64(idgen.ShardNo(value)), nil
	}
	return 0, errors.Errorf("业务用户 ID 不是整数 type=%T", values[index])
}

// bucketValue 从 Ghostferry 行数据中读取整数固定桶。
func bucketValue(values []any, index int) (int64, bool, error) {
	if values == nil {
		return 0, false, nil
	}
	if index < 0 || index >= len(values) {
		return 0, false, errors.Errorf("固定桶字段下标越界 index=%d columns=%d", index, len(values))
	}
	if value, ok := ghostferry.Uint64Value(values[index]); ok {
		if value > 1023 {
			return 0, true, errors.Errorf("固定桶越界 value=%d", value)
		}
		return int64(value), true, nil
	}
	if value, ok := ghostferry.Int64Value(values[index]); ok {
		if value < 0 || value > 1023 {
			return 0, true, errors.Errorf("固定桶越界 value=%d", value)
		}
		return value, true, nil
	}
	return 0, true, errors.Errorf("固定桶不是整数 type=%T", values[index])
}
