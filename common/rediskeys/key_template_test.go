package keys

import "testing"

func TestKeyTemplatePrefix(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
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
			if got := KeyTemplatePrefix(tt.template); got != tt.want {
				t.Fatalf("KeyTemplatePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
