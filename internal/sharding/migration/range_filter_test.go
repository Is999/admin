package migration

import (
	"testing"
	"time"

	"admin/common/idgen"

	"github.com/Shopify/ghostferry"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
)

// TestBucketRangeFilterContains 验证固定桶范围使用闭区间。
func TestBucketRangeFilterContains(t *testing.T) {
	bucketRange := BucketRange{Start: 256, End: 511}
	for _, bucket := range []int64{256, 300, 511} {
		if !bucketRange.Contains(bucket) {
			t.Fatalf("bucket %d should be included", bucket)
		}
	}
	for _, bucket := range []int64{0, 255, 512, 1023} {
		if bucketRange.Contains(bucket) {
			t.Fatalf("bucket %d should be excluded", bucket)
		}
	}
}

// TestBucketRangeFilterRoutesEachSource 验证一个复制实例可以为多张源表使用独立桶区间。
func TestBucketRangeFilterRoutesEachSource(t *testing.T) {
	filter := BucketRangeFilter{
		UIDColumn:   "id",
		ShardColumn: "shard_no",
		Ranges: map[string]BucketRange{
			"user":       {Start: 256, End: 511},
			"user_b0512": {Start: 768, End: 1023},
		},
	}
	if err := filter.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, err := filter.rangeForTable("user_b0512"); err != nil || got.Start != 768 {
		t.Fatalf("rangeForTable() = (%+v, %v)", got, err)
	}
}

// TestBucketRangeFilterRejectsWrongBinlogRoute 验证复制期间新增的错误桶数据不会被静默跳过。
func TestBucketRangeFilterRejectsWrongBinlogRoute(t *testing.T) {
	uid := int64(1024)
	filter := BucketRangeFilter{
		UIDColumn:   "id",
		ShardColumn: "shard_no",
		Ranges:      map[string]BucketRange{"user": {Start: 0, End: 1023}},
	}
	event := newInsertEvent(t, uid, int64(idgen.ShardNo(uid)))
	if applicable, err := filter.ApplicableEvent(event); err != nil || !applicable {
		t.Fatalf("正确固定桶事件结果 = (%t, %v)", applicable, err)
	}
	event = newInsertEvent(t, uid, uid%idgen.ShardMod)
	if _, err := filter.ApplicableEvent(event); err == nil {
		t.Fatal("期望复制期间的错误固定桶事件被拒绝")
	}
}

// TestBucketValueRejectsInvalidData 验证非法固定桶不会被静默跳过。
func TestBucketValueRejectsInvalidData(t *testing.T) {
	if _, _, err := bucketValue([]any{int64(1024)}, 0); err == nil {
		t.Fatal("期望越界固定桶返回错误")
	}
	if _, _, err := bucketValue([]any{"bad"}, 0); err == nil {
		t.Fatal("期望非整数固定桶返回错误")
	}
}

// newInsertEvent 构造包含业务用户 ID 和固定桶的 Ghostferry 插入事件。
func newInsertEvent(t *testing.T, uid int64, shardNo int64) ghostferry.DMLEvent {
	t.Helper()
	table := &ghostferry.TableSchema{Table: &schema.Table{
		Schema: "business",
		Name:   "user",
		Columns: []schema.TableColumn{
			{Name: "id"},
			{Name: "shard_no"},
		},
	}}
	base := ghostferry.NewDMLEventBase(table, mysql.Position{}, mysql.Position{}, nil, time.Time{})
	events, err := ghostferry.NewBinlogInsertEvents(base, &replication.RowsEvent{
		Rows: [][]any{{uid, shardNo}},
	})
	if err != nil {
		t.Fatalf("NewBinlogInsertEvents() error = %v", err)
	}
	return events[0]
}
