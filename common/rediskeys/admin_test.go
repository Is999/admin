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
		{name: "admin info", got: keys.AdminInfo, want: "admin:info:%d"},
		{name: "admin info pattern", got: keys.AdminInfoPattern, want: "admin:info:{adminID}"},
		{name: "logout token", got: keys.AdminLogoutToken, want: "admin:logout_token:%d"},
		{name: "login mfa flag", got: keys.LoginCheckMFAFlag, want: "login_check_mfa_flag:%d"},
		{name: "mfa ticket", got: keys.AdminMFATwoStepTicket, want: "admin:mfa:two_step:%d:%s"},
		{name: "mfa ticket pattern", got: keys.AdminMFATwoStepTicketPattern, want: "admin:mfa:two_step:{adminID}:{ticketKey}"},
		{name: "mfa index", got: keys.AdminMFATwoStepIndex, want: "admin:mfa:two_step:index:%d"},
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
		{name: "admin info", got: keys.AdminInfoRedisKey(7), want: "app:site-a:admin:info:7"},
		{name: "admin info pattern", got: keys.AdminInfoPatternRedisKey(), want: "app:site-a:admin:info:{adminID}"},
		{name: "logout token", got: keys.AdminLogoutTokenRedisKey(7), want: "app:site-a:admin:logout_token:7"},
		{name: "login mfa flag", got: keys.LoginCheckMFAFlagRedisKey(7), want: "app:site-a:login_check_mfa_flag:7"},
		{name: "mfa ticket", got: keys.AdminMFATwoStepTicketRedisKey(7, " ticket-1 "), want: "app:site-a:admin:mfa:two_step:7:ticket-1"},
		{name: "mfa index", got: keys.AdminMFATwoStepIndexRedisKey(7), want: "app:site-a:admin:mfa:two_step:index:7"},
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
	if got := keys.AdminInfoRedisKey(7); got != "" {
		t.Fatalf("keys.AdminInfoRedisKey(empty app) = %q, want empty", got)
	}
	if got := keys.AdminMFATwoStepTicketRedisKey(7, "ticket-1"); got != "" {
		t.Fatalf("keys.AdminMFATwoStepTicketRedisKey(empty app) = %q, want empty", got)
	}
}
