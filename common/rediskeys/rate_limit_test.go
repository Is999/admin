package keys_test

import (
	"strings"
	"testing"

	keys "admin/common/rediskeys"
)

// TestRateLimitRedisKey 验证限流 key 按应用、业务域和资源隔离。
func TestRateLimitRedisKey(t *testing.T) {
	useAppID(t, "site-a")

	if got := keys.RateLimitRedisKey("audit", "database:log"); got != "app:site-a:rate_limit:audit:database:log" {
		t.Fatalf("RateLimitRedisKey() = %q", got)
	}
	if first, second := keys.RateLimitRedisKey("audit", "database:log"), keys.RateLimitRedisKey("message", "database:log"); first == second {
		t.Fatal("不同业务域不应共享限流 key")
	}
}

// TestRateLimitFixedIntervalRedisKey 验证固定间隔限流使用独立算法段并保持业务隔离。
func TestRateLimitFixedIntervalRedisKey(t *testing.T) {
	useAppID(t, "site-a")

	got := keys.RateLimitFixedIntervalRedisKey("user_tag", "rolling_uid:write")
	if got != "app:site-a:rate_limit:fixed_interval:user_tag:rolling_uid:write" {
		t.Fatalf("RateLimitFixedIntervalRedisKey() = %q", got)
	}
	if got == keys.RateLimitRedisKey("user_tag", "rolling_uid:write") {
		t.Fatal("固定间隔状态不应与 GCRA 配额状态共享 key")
	}
}

// TestRateLimitRedisKeyRejectsInvalidParts 验证空值、动态业务域和控制字符失败闭合。
func TestRateLimitRedisKeyRejectsInvalidParts(t *testing.T) {
	useAppID(t, "site-a")

	tests := []struct {
		name     string // 用例名称
		scope    string // 业务域
		resource string // 资源标识
	}{
		{name: "empty scope", resource: "write"},
		{name: "uppercase scope", scope: "Audit", resource: "write"},
		{name: "scope whitespace", scope: " audit", resource: "write"},
		{name: "scope separator", scope: "audit:log", resource: "write"},
		{name: "long scope", scope: strings.Repeat("a", 65), resource: "write"},
		{name: "empty resource", scope: "audit"},
		{name: "resource control", scope: "audit", resource: "write\nlog"},
		{name: "resource whitespace", scope: "audit", resource: "write log"},
		{name: "resource trailing whitespace", scope: "audit", resource: "write "},
		{name: "resource hash tag", scope: "audit", resource: "write{hot}"},
		{name: "resource slash", scope: "audit", resource: "write/log"},
		{name: "long resource", scope: "audit", resource: strings.Repeat("a", 257)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := keys.RateLimitRedisKey(testCase.scope, testCase.resource); got != "" {
				t.Fatalf("RateLimitRedisKey() = %q, want empty", got)
			}
			if got := keys.RateLimitFixedIntervalRedisKey(testCase.scope, testCase.resource); got != "" {
				t.Fatalf("RateLimitFixedIntervalRedisKey() = %q, want empty", got)
			}
		})
	}
}

// TestRateLimitRedisKeyRequiresAppID 验证缺少应用命名空间时不生成裸 key。
func TestRateLimitRedisKeyRequiresAppID(t *testing.T) {
	useAppID(t, "")
	if got := keys.RateLimitRedisKey("audit", "write"); got != "" {
		t.Fatalf("RateLimitRedisKey() = %q, want empty", got)
	}
	if got := keys.RateLimitFixedIntervalRedisKey("audit", "write"); got != "" {
		t.Fatalf("RateLimitFixedIntervalRedisKey() = %q, want empty", got)
	}
}
