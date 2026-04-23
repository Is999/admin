package keys

import "testing"

func TestAppScopedKey(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  string
	}{
		{
			name:  "scopes logical key",
			appID: "site-a",
			key:   "admin:info:1",
			want:  "app:site-a:admin:info:1",
		},
		{
			name: "uses default app id",
			key:  "login:captcha:abc",
			want: "app:default:login:captcha:abc",
		},
		{
			name:  "keeps scoped key unchanged",
			appID: "site-b",
			key:   "app:site-a:admin:info:1",
			want:  "app:site-a:admin:info:1",
		},
		{
			name:  "keeps table cache key unchanged",
			appID: "site-a",
			key:   "app:site-a:admin_role:1",
			want:  "app:site-a:admin_role:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppScopedKey(tt.appID, tt.key); got != tt.want {
				t.Fatalf("AppScopedKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrimAppScopedPrefix(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "trims scoped key",
			key:  "app:site-a:admin:info:1",
			want: "admin:info:1",
		},
		{
			name: "keeps logical key",
			key:  "admin:info:1",
			want: "admin:info:1",
		},
		{
			name: "keeps incomplete prefix",
			key:  "app:site-a",
			want: "app:site-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimAppScopedPrefix(tt.key); got != tt.want {
				t.Fatalf("TrimAppScopedPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
