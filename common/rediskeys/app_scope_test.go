package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// scopedTestKey 返回指定 app_id 下的完整测试 key。
func scopedTestKey(appID string, key string) string {
	return keys.ScopeRoot + appID + ":" + key
}

func TestWithPrefix(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  string
	}{
		{
			name:  "scopes logical key",
			appID: "site-a",
			key:   keys.AdminInfoLogicalKey(1),
			want:  scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)),
		},
		{
			name:  "keeps current app scoped key unchanged",
			appID: "site-a",
			key:   scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)),
			want:  scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)),
		},
		{
			name:  "rejects other app scoped key",
			appID: "site-b",
			key:   scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)),
			want:  "",
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
			useAppID(t, tt.appID)
			if got := keys.WithPrefix(tt.key); got != tt.want {
				t.Fatalf("keys.WithPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasPrefix(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "scoped key", key: scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)), want: true},
		{name: "empty logical key", key: "app:site-a:", want: false},
		{name: "missing logical separator", key: "app:site-a", want: false},
		{name: "missing app id", key: keys.ScopeRoot + ":" + keys.AdminInfoLogicalKey(1), want: false},
		{name: "logical key", key: keys.AdminInfoLogicalKey(1), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keys.HasPrefix(tt.key); got != tt.want {
				t.Fatalf("keys.HasPrefix() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestOwner(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		want   string
		wantOK bool
	}{
		{name: "scoped key", key: scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)), want: "site-a", wantOK: true},
		{name: "table cache key", key: "app:site-a:table:role_tree", want: "site-a", wantOK: true},
		{name: "empty logical key", key: "app:site-a:", want: "", wantOK: false},
		{name: "missing logical separator", key: "app:site-a", want: "", wantOK: false},
		{name: "missing app id", key: keys.ScopeRoot + ":" + keys.AdminInfoLogicalKey(1), want: "", wantOK: false},
		{name: "logical key", key: keys.AdminInfoLogicalKey(1), want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := keys.Owner(tt.key)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("keys.Owner() = %q, %t, want %q, %t", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsForeignKey(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  bool
	}{
		{name: "current app key", appID: "site-a", key: scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)), want: false},
		{name: "other app key", appID: "site-a", key: scopedTestKey("site-b", keys.AdminInfoLogicalKey(1)), want: true},
		{name: "other app table key", appID: "site-a", key: "app:site-b:table:role_tree", want: true},
		{name: "logical key", appID: "site-a", key: keys.AdminInfoLogicalKey(1), want: false},
		{name: "empty app id", appID: "", key: scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)), want: true},
		{name: "incomplete prefix", appID: "site-a", key: "app:site-b", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useAppID(t, tt.appID)
			if got := keys.IsForeignKey(tt.key); got != tt.want {
				t.Fatalf("keys.IsForeignKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestWithPrefixWithEmptyAppIDFailsClosed(t *testing.T) {
	useAppID(t, "")
	if got := keys.Prefix(); got != "" {
		t.Fatalf("keys.Prefix(empty) = %q, want empty", got)
	}
	if got := keys.WithPrefix("login:captcha:abc"); got != "" {
		t.Fatalf("keys.WithPrefix(empty app, logical key) = %q, want empty", got)
	}
	if got := keys.WithPrefix("app:site-a:login:captcha:abc"); got != "" {
		t.Fatalf("keys.WithPrefix(empty app, scoped key) = %q, want empty", got)
	}
}

func TestTrimPrefix(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "trims scoped key",
			key:  scopedTestKey("site-a", keys.AdminInfoLogicalKey(1)),
			want: keys.AdminInfoLogicalKey(1),
		},
		{
			name: "keeps logical key",
			key:  keys.AdminInfoLogicalKey(1),
			want: keys.AdminInfoLogicalKey(1),
		},
		{
			name: "keeps incomplete prefix",
			key:  "app:site-a",
			want: "app:site-a",
		},
		{
			name: "keeps missing app id prefix",
			key:  keys.ScopeRoot + ":" + keys.AdminInfoLogicalKey(1),
			want: keys.ScopeRoot + ":" + keys.AdminInfoLogicalKey(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keys.TrimPrefix(tt.key); got != tt.want {
				t.Fatalf("keys.TrimPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
