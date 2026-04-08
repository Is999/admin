package excel

import "testing"

func TestRecommendShardCount(t *testing.T) {
	if got := RecommendShardCount(0, 200, 4, 2); got != 1 {
		t.Fatalf("RecommendShardCount 空数据返回不正确: got=%d", got)
	}
	if got := RecommendShardCount(5000, 200, 4, 2); got != 4 {
		t.Fatalf("RecommendShardCount 并发上限控制失败: got=%d", got)
	}
	if got := RecommendShardCount(300, 200, 8, 2); got != 1 {
		t.Fatalf("RecommendShardCount 小数据分片数不正确: got=%d", got)
	}
}

func TestBuildOrderedIDShards(t *testing.T) {
	shards := BuildOrderedIDShards(10, 109, 4)
	if len(shards) != 4 {
		t.Fatalf("BuildOrderedIDShards 分片数量不正确: got=%d", len(shards))
	}
	if shards[0].Meta.StartID != 10 || shards[0].Meta.EndID != 34 {
		t.Fatalf("首个分片范围不正确: got=%+v", shards[0].Meta)
	}
	if shards[3].Meta.StartID != 85 || shards[3].Meta.EndID != 0 {
		t.Fatalf("最后一个分片范围不正确: got=%+v", shards[3].Meta)
	}
}
