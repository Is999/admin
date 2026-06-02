package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// TestKeyTemplatePrefix 验证对应场景符合预期。
func TestKeyTemplatePrefix(t *testing.T) {
	tests := []struct {
		name     string // name 表示测试场景名称。
		template string // template 表示待验证 key 模板。
		want     string // want 表示期望结果。
	}{
		{
			name:     "brace placeholder",
			template: "admin_role_ids:{adminID}",
			want:     "admin_role_ids:",
		},
		{
			name:     "printf placeholder",
			template: "admin_permission_ids:%d",
			want:     "admin_permission_ids:",
		},
		{
			name:     "mixed placeholders",
			template: "app:{appID}:task:%s",
			want:     "app:",
		},
		{
			name:     "concrete key",
			template: "user_tag:workflow:write_lock",
			want:     "user_tag:workflow:write_lock",
		},
		{
			name:     "trims surrounding spaces",
			template: "  secret_key_aes:{uuid}:{keyVersion}  ",
			want:     "secret_key_aes:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keys.KeyTemplatePrefix(tt.template); got != tt.want {
				t.Fatalf("keys.KeyTemplatePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
