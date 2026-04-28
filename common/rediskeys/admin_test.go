package keys

import "testing"

// TestAdminRedisKeyTemplatesStayLogical 验证 key 常量只保存业务段，不提前拼 app_id 前缀。
func TestAdminRedisKeyTemplatesStayLogical(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "admin info", got: AdminInfo, want: "admin:info:%d"},
		{name: "admin info pattern", got: AdminInfoPattern, want: "admin:info:{adminID}"},
		{name: "logout token", got: AdminLogoutToken, want: "admin:logout_token:%d"},
		{name: "login mfa flag", got: LoginCheckMFAFlag, want: "login_check_mfa_flag:%d"},
		{name: "mfa ticket", got: AdminMFATwoStepTicket, want: "admin:mfa:two_step:%d:%s"},
		{name: "mfa ticket pattern", got: AdminMFATwoStepTicketPattern, want: "admin:mfa:two_step:{adminID}:{ticketKey}"},
		{name: "mfa index", got: AdminMFATwoStepIndex, want: "admin:mfa:two_step:index:%d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("Redis key template = %q, want %q", tt.got, tt.want)
			}
			if HasAppScopedPrefix(tt.got) {
				t.Fatalf("Redis key template %q should not contain app scope prefix", tt.got)
			}
		})
	}
}

// TestAdminRedisKeysUseAppScope 验证管理员相关直接 Redis key 都带 app_id 命名空间。
func TestAdminRedisKeysUseAppScope(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "admin info", got: AdminInfoRedisKey("site-a", 7), want: "app:site-a:admin:info:7"},
		{name: "admin info pattern", got: AdminInfoPatternRedisKey("site-a"), want: "app:site-a:admin:info:{adminID}"},
		{name: "logout token", got: AdminLogoutTokenRedisKey("site-a", 7), want: "app:site-a:admin:logout_token:7"},
		{name: "login mfa flag", got: LoginCheckMFAFlagRedisKey("site-a", 7), want: "app:site-a:login_check_mfa_flag:7"},
		{name: "mfa ticket", got: AdminMFATwoStepTicketRedisKey("site-a", 7, " ticket-1 "), want: "app:site-a:admin:mfa:two_step:7:ticket-1"},
		{name: "mfa index", got: AdminMFATwoStepIndexRedisKey("site-a", 7), want: "app:site-a:admin:mfa:two_step:index:7"},
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
	if got := AdminInfoRedisKey("", 7); got != "" {
		t.Fatalf("AdminInfoRedisKey(empty app) = %q, want empty", got)
	}
	if got := AdminMFATwoStepTicketRedisKey("", 7, "ticket-1"); got != "" {
		t.Fatalf("AdminMFATwoStepTicketRedisKey(empty app) = %q, want empty", got)
	}
}
