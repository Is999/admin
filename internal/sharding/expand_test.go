package sharding

import (
	"reflect"
	"testing"
)

// TestExpandMovesKeepsOldBucketStarts 验证扩容只迁移新出现的桶起点。
func TestExpandMovesKeepsOldBucketStarts(t *testing.T) {
	got, err := ExpandMoves("user", 2, 4)
	if err != nil {
		t.Fatalf("ExpandMoves() error = %v", err)
	}
	want := []Move{
		{Source: "user", Target: "user_b0256", BucketStart: 256, BucketEnd: 511},
		{Source: "user_b0512", Target: "user_b0768", BucketStart: 768, BucketEnd: 1023},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandMoves() = %+v, want %+v", got, want)
	}
}

// TestExpandMovesRejectsShrink 验证迁移计划不会误执行缩容或相同分片数。
func TestExpandMovesRejectsShrink(t *testing.T) {
	for _, counts := range [][2]int{{2, 2}, {4, 2}} {
		if _, err := ExpandMoves("user", counts[0], counts[1]); err == nil {
			t.Fatalf("ExpandMoves(%d, %d) 应失败", counts[0], counts[1])
		}
	}
}

// TestExpandMovesRejectsExcessiveRatio 验证单次只允许翻倍，避免同一源表并发复制争锁。
func TestExpandMovesRejectsExcessiveRatio(t *testing.T) {
	for _, counts := range [][2]int{{1, 4}, {2, 8}} {
		if _, err := ExpandMoves("user", counts[0], counts[1]); err == nil {
			t.Fatalf("期望单次 %d→%d 扩容被拒绝", counts[0], counts[1])
		}
	}
}
