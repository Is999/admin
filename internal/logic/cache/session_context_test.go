package cache

import (
	"context"
	"testing"

	"admin/internal/types"
)

// TestAdminSessionContextRoundTrip 验证鉴权会话可在同一请求链路复用。
func TestAdminSessionContextRoundTrip(t *testing.T) {
	session := &types.AdminSession{ID: 7, UserName: "admin"}
	ctx := WithAdminSession(context.Background(), session)
	got, ok := AdminSessionFromContext(ctx)
	if !ok || got != session {
		t.Fatalf("AdminSessionFromContext() = %+v, %v", got, ok)
	}
}
