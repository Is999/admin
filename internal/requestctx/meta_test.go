package requestctx

import (
	"context"
	"testing"
	"time"
)

// TestRefreshLatencyUsesStartedAt 验证审计落库前可基于请求开始时间刷新当前耗时。
func TestRefreshLatencyUsesStartedAt(t *testing.T) {
	ctx, meta := New(context.Background())
	meta.StartedAt = time.Now().Add(-5 * time.Millisecond)

	RefreshLatency(ctx)

	if meta.LatencyMS <= 0 {
		t.Fatalf("RefreshLatency() latency_ms = %d, want positive", meta.LatencyMS)
	}
}

// TestSetLatencyRoundsSubMillisecondToOne 验证非零亚毫秒请求不会在后台日志里展示为 0ms。
func TestSetLatencyRoundsSubMillisecondToOne(t *testing.T) {
	ctx, meta := New(context.Background())

	SetLatency(ctx, time.Nanosecond)

	if meta.LatencyMS != 1 {
		t.Fatalf("SetLatency(1ns) latency_ms = %d, want 1", meta.LatencyMS)
	}
}
