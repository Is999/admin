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

// TestClientIPRegionStateFollowsClientIP 验证空归属地也会记住，且 IP 变化时不会复用旧结果。
func TestClientIPRegionStateFollowsClientIP(t *testing.T) {
	ctx, meta := New(context.Background())
	SetRequest(ctx, "POST", "/api/auth/login", "114.114.114.114")
	SetClientIPRegion(ctx, "114.114.114.114", "")
	if !meta.ClientIPRegionResolved || meta.ClientIPRegion != "" {
		t.Fatalf("空归属地解析状态异常: %+v", meta)
	}

	SetUser(ctx, 1, "tester", "8.8.8.8")
	if meta.ClientIP != "8.8.8.8" || meta.ClientIPRegionResolved || meta.ClientIPRegion != "" {
		t.Fatalf("IP 变化后未清除旧归属地: %+v", meta)
	}
}

// TestNormalizeClientIP 验证客户端地址统一去除 zone、IPv4 映射和无效文本。
func TestNormalizeClientIP(t *testing.T) {
	tests := []struct {
		name string // 测试场景
		ip   string // 原始客户端地址
		want string // 期望的规范地址
	}{
		{name: "ipv4", ip: " 203.0.113.9 ", want: "203.0.113.9"},
		{name: "mapped_ipv4", ip: "::ffff:203.0.113.9", want: "203.0.113.9"},
		{name: "ipv6_zone", ip: "fe80::1%very-long-interface-name", want: "fe80::1"},
		{name: "invalid", ip: "not-an-ip"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeClientIP(test.ip); got != test.want {
				t.Fatalf("NormalizeClientIP(%q)=%q, want %q", test.ip, got, test.want)
			}
		})
	}
}
