package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// TestAdminRedisKeyTemplatesStayLogical 验证 key 常量只保存业务段，不提前拼 app_id 前缀。
func TestAdminRedisKeyTemplatesStayLogical(t *testing.T) {
	tests := []struct {
		name string // name 表示测试场景名称。
		got  string // got 表示实际结果。
		want string // want 表示期望结果。
	}{
		{name: "admin session", got: keys.AdminSession, want: "admin:session:%d"},
		{name: "admin session pattern", got: keys.AdminSessionPattern, want: "admin:session:{adminID}"},
		{name: "security cache barrier", got: keys.SecurityCacheSyncBarrier, want: "security:cache_sync:barrier"},
		{name: "security cache lock", got: keys.SecurityCacheSyncLock, want: "security:cache_sync:lock"},
		{name: "login mfa flag", got: keys.LoginCheckMFAFlag, want: "login_check_mfa_flag:%d"},
		{name: "mfa two step", got: keys.AdminMFATwoStep, want: "admin:mfa:two_step:%d"},
		{name: "mfa two step pattern", got: keys.AdminMFATwoStepPattern, want: "admin:mfa:two_step:{adminID}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("Redis key template = %q, want %q", tt.got, tt.want)
			}
			if keys.HasPrefix(tt.got) {
				t.Fatalf("Redis key template %q should not contain app scope prefix", tt.got)
			}
		})
	}
}

// TestAdminRedisKeysUseAppScope 验证管理员相关直接 Redis key 都带 app_id 命名空间。
func TestAdminRedisKeysUseAppScope(t *testing.T) {
	useAppID(t, "site-a")
	tests := []struct {
		name string // name 表示测试场景名称。
		got  string // got 表示实际结果。
		want string // want 表示期望结果。
	}{
		{name: "admin session", got: keys.AdminSessionRedisKey(7), want: "app:site-a:admin:session:7"},
		{name: "admin session pattern", got: keys.AdminSessionPatternRedisKey(), want: "app:site-a:admin:session:{adminID}"},
		{name: "security cache barrier", got: keys.SecurityCacheSyncBarrierRedisKey(), want: "app:site-a:security:cache_sync:barrier"},
		{name: "security cache lock", got: keys.SecurityCacheSyncLockRedisKey(), want: "app:site-a:security:cache_sync:lock"},
		{name: "login mfa flag", got: keys.LoginCheckMFAFlagRedisKey(7), want: "app:site-a:login_check_mfa_flag:7"},
		{name: "mfa two step", got: keys.AdminMFATwoStepRedisKey(7), want: "app:site-a:admin:mfa:two_step:7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("Redis key = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestAdminRedisKeysFailClosedWithoutAppID 验证缺少 app_id 时不会生成裸业务 key。
func TestAdminRedisKeysFailClosedWithoutAppID(t *testing.T) {
	useAppID(t, "")
	if got := keys.AdminSessionRedisKey(7); got != "" {
		t.Fatalf("keys.AdminSessionRedisKey(empty app) = %q, want empty", got)
	}
	if got := keys.AdminMFATwoStepRedisKey(7); got != "" {
		t.Fatalf("keys.AdminMFATwoStepRedisKey(empty app) = %q, want empty", got)
	}
}
