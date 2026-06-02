package admin

import (
	"context"
	"testing"

	"admin/common/codes"
)

// TestAdminNameAlreadyExistsResultMessage 验证对应场景符合预期。
func TestAdminNameAlreadyExistsResultMessage(t *testing.T) {
	result := adminNameAlreadyExistsResult("alice", errAdminNameAlreadyExists)
	if result.Code != codes.UserAlreadyExists {
		t.Fatalf("code = %d, want %d", result.Code, codes.UserAlreadyExists)
	}
	if got, want := result.ResolveMessage(context.Background()), "用户[alice]已存在"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
