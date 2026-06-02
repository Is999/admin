package logic

import (
	"context"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"
)

// TestCacheLockKeyUsesAppNamespace 验证对应场景符合预期。
func TestCacheLockKeyUsesAppNamespace(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	base := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	got := base.cacheLockKey("app:site-a:admin:info:1")
	want := "app:site-a:cache:rebuild:lock:admin:info:1"
	if got != want {
		t.Fatalf("cacheLockKey(app) = %q, want %q", got, want)
	}
}

// TestCacheLockKeyTrimsTableCacheNamespace 验证对应场景符合预期。
func TestCacheLockKeyTrimsTableCacheNamespace(t *testing.T) {
	useRuntimeAppID(t, "site-a")
	base := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	got := base.cacheLockKey("app:site-a:table:role_tree")
	want := "app:site-a:cache:rebuild:lock:role_tree"
	if got != want {
		t.Fatalf("cacheLockKey(table-cache) = %q, want %q", got, want)
	}
}
