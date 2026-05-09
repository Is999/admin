package svc

import (
	"context"
	"testing"

	"admin/internal/config"
)

func TestScopedWithContextCopiesConfigSnapshot(t *testing.T) {
	svcCtx := NewServiceContext(config.Config{AppID: "root"}, Dependencies{})
	svcCtx.configValue.Store(config.Config{AppID: "request"})

	scoped := svcCtx.ScopedWithContext(context.Background())
	if scoped == nil {
		t.Fatal("ScopedWithContext() = nil")
	}
	if got := scoped.CurrentConfig().AppID; got != "request" {
		t.Fatalf("scoped AppID = %q, want request", got)
	}
}
