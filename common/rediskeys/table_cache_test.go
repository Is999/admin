package keys

import "testing"

func TestTableCachePrefix(t *testing.T) {
	if got, want := TableCachePrefix("site-a"), "app:site-a:table:"; got != want {
		t.Fatalf("TableCachePrefix() = %q, want %q", got, want)
	}
}

func TestIsTableCacheKey(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  bool
	}{
		{name: "table cache key", appID: "site-a", key: "app:site-a:table:role_tree", want: true},
		{name: "other app table cache key", appID: "site-a", key: "app:site-b:table:role_tree", want: false},
		{name: "direct app key", appID: "site-a", key: "app:site-a:admin:info:1", want: false},
		{name: "logical table segment", appID: "site-a", key: "table:role_tree", want: false},
		{name: "incomplete table prefix", appID: "site-a", key: "app:site-a:table", want: false},
		{name: "empty app id", appID: "", key: "app:site-a:table:role_tree", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTableCacheKey(tt.appID, tt.key); got != tt.want {
				t.Fatalf("IsTableCacheKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestTrimTableCachePrefix(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  string
	}{
		{
			name:  "trims table cache key",
			appID: "site-a",
			key:   "app:site-a:table:admin_role_ids:7",
			want:  "admin_role_ids:7",
		},
		{
			name:  "keeps other app table cache key",
			appID: "site-a",
			key:   "app:site-b:table:admin_role_ids:7",
			want:  "app:site-b:table:admin_role_ids:7",
		},
		{
			name:  "keeps direct app key",
			appID: "site-a",
			key:   "app:site-a:admin:info:7",
			want:  "app:site-a:admin:info:7",
		},
		{
			name:  "keeps logical key",
			appID: "site-a",
			key:   "admin_role_ids:7",
			want:  "admin_role_ids:7",
		},
		{
			name:  "keeps incomplete table prefix",
			appID: "site-a",
			key:   "app:site-a:table",
			want:  "app:site-a:table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimTableCachePrefix(tt.appID, tt.key); got != tt.want {
				t.Fatalf("TrimTableCachePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
