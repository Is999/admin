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
			key:   AdminInfoLogicalKey(1),
			want:  AdminInfoRedisKey("site-a", 1),
		},
		{
			name:  "keeps current app scoped key unchanged",
			appID: "site-a",
			key:   AdminInfoRedisKey("site-a", 1),
			want:  AdminInfoRedisKey("site-a", 1),
		},
		{
			name:  "keeps other app scoped key unchanged",
			appID: "site-b",
			key:   AdminInfoRedisKey("site-a", 1),
			want:  AdminInfoRedisKey("site-a", 1),
		},
		{
			name:  "keeps table cache key unchanged",
			appID: "site-a",
			key:   "app:site-a:table:admin_role:1",
			want:  "app:site-a:table:admin_role:1",
		},
		{
			name:  "scopes incomplete app prefix as logical key",
			appID: "site-b",
			key:   "app:site-a",
			want:  "app:site-b:app:site-a",
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

func TestHasAppScopedPrefix(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "scoped key", key: AdminInfoRedisKey("site-a", 1), want: true},
		{name: "empty logical key", key: "app:site-a:", want: false},
		{name: "missing logical separator", key: "app:site-a", want: false},
		{name: "missing app id", key: AppScopedDataPrefix + ":" + AdminInfoLogicalKey(1), want: false},
		{name: "logical key", key: AdminInfoLogicalKey(1), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAppScopedPrefix(tt.key); got != tt.want {
				t.Fatalf("HasAppScopedPrefix() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAppScopedAppID(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		want   string
		wantOK bool
	}{
		{name: "scoped key", key: AdminInfoRedisKey("site-a", 1), want: "site-a", wantOK: true},
		{name: "table cache key", key: "app:site-a:table:role_tree", want: "site-a", wantOK: true},
		{name: "empty logical key", key: "app:site-a:", want: "", wantOK: false},
		{name: "missing logical separator", key: "app:site-a", want: "", wantOK: false},
		{name: "missing app id", key: AppScopedDataPrefix + ":" + AdminInfoLogicalKey(1), want: "", wantOK: false},
		{name: "logical key", key: AdminInfoLogicalKey(1), want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AppScopedAppID(tt.key)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("AppScopedAppID() = %q, %t, want %q, %t", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsForeignAppScopedKey(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  bool
	}{
		{name: "current app key", appID: "site-a", key: AdminInfoRedisKey("site-a", 1), want: false},
		{name: "other app key", appID: "site-a", key: AdminInfoRedisKey("site-b", 1), want: true},
		{name: "other app table key", appID: "site-a", key: "app:site-b:table:role_tree", want: true},
		{name: "logical key", appID: "site-a", key: AdminInfoLogicalKey(1), want: false},
		{name: "empty app id", appID: "", key: AdminInfoRedisKey("site-a", 1), want: false},
		{name: "incomplete prefix", appID: "site-a", key: "app:site-b", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsForeignAppScopedKey(tt.appID, tt.key); got != tt.want {
				t.Fatalf("IsForeignAppScopedKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAppScopedKeyWithEmptyAppIDFailsClosed(t *testing.T) {
	if got := AppScopedPrefix(""); got != "" {
		t.Fatalf("AppScopedPrefix(empty) = %q, want empty", got)
	}
	if got := AppScopedKey("", "login:captcha:abc"); got != "" {
		t.Fatalf("AppScopedKey(empty app, logical key) = %q, want empty", got)
	}
	if got := AppScopedKey("", "app:site-a:login:captcha:abc"); got != "app:site-a:login:captcha:abc" {
		t.Fatalf("AppScopedKey(empty app, scoped key) = %q", got)
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
			key:  AdminInfoRedisKey("site-a", 1),
			want: AdminInfoLogicalKey(1),
		},
		{
			name: "keeps logical key",
			key:  AdminInfoLogicalKey(1),
			want: AdminInfoLogicalKey(1),
		},
		{
			name: "keeps incomplete prefix",
			key:  "app:site-a",
			want: "app:site-a",
		},
		{
			name: "keeps missing app id prefix",
			key:  AppScopedDataPrefix + ":" + AdminInfoLogicalKey(1),
			want: AppScopedDataPrefix + ":" + AdminInfoLogicalKey(1),
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
