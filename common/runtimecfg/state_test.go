package runtimecfg

import (
	"testing"

	"admin/internal/config"
)

func TestSetTrimsAppID(t *testing.T) {
	prev := Get()
	Set(config.Config{AppID: " 215 "})
	t.Cleanup(func() {
		Restore(prev)
	})
	if got := AppID(); got != "215" {
		t.Fatalf("AppID() = %q, want 215", got)
	}
}

func TestGetReturnsEmptyBeforeSet(t *testing.T) {
	prev := Get()
	Set(config.Config{})
	t.Cleanup(func() {
		Restore(prev)
	})
	if got := Get().AppID; got != "" {
		t.Fatalf("Get().AppID = %q, want empty", got)
	}
}

func TestRestoreSnapshot(t *testing.T) {
	prev := Get()
	Set(config.Config{AppID: "new-app"})
	Restore(Snapshot{AppID: "old-app"})
	t.Cleanup(func() {
		Restore(prev)
	})
	if got := AppID(); got != "old-app" {
		t.Fatalf("AppID() = %q, want old-app", got)
	}
}
