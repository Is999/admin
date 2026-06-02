package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// TestTableCachePrefix 验证对应场景符合预期。
func TestTableCachePrefix(t *testing.T) {
	useAppID(t, "site-a")
	if got, want := keys.TableCachePrefix(), "app:site-a:table:"; got != want {
		t.Fatalf("keys.TableCachePrefix() = %q, want %q", got, want)
	}
}

// TestIsTableCacheKey 验证对应场景符合预期。
func TestIsTableCacheKey(t *testing.T) {
	tests := []struct {
		name  string // name 表示测试场景名称。
		appID string // appID 表示测试应用 ID。
		key   string // key 表示待验证 key。
		want  bool   // want 表示期望结果。
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
			useAppID(t, tt.appID)
			if got := keys.IsTableCacheKey(tt.key); got != tt.want {
				t.Fatalf("keys.IsTableCacheKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestTrimTableCachePrefix 验证对应场景符合预期。
func TestTrimTableCachePrefix(t *testing.T) {
	tests := []struct {
		name  string // name 表示测试场景名称。
		appID string // appID 表示测试应用 ID。
		key   string // key 表示待验证 key。
		want  string // want 表示期望结果。
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
			useAppID(t, tt.appID)
			if got := keys.TrimTableCachePrefix(tt.key); got != tt.want {
				t.Fatalf("keys.TrimTableCachePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
