package logic

import (
	"context"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"
)

func TestCacheLockKeyUsesAppNamespace(t *testing.T) {
	base := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	got := base.cacheLockKey("app:site-a:admin:info:1")
	want := "app:site-a:cache:rebuild:lock:admin:info:1"
	if got != want {
		t.Fatalf("cacheLockKey(app) = %q, want %q", got, want)
	}
}

func TestCacheLockKeyTrimsTableCacheNamespace(t *testing.T) {
	base := NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	got := base.cacheLockKey("app:site-a:table:role_tree")
	want := "app:site-a:cache:rebuild:lock:role_tree"
	if got != want {
		t.Fatalf("cacheLockKey(table-cache) = %q, want %q", got, want)
	}
}
