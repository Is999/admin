package admin

import (
	"testing"

	"admin/internal/model"
	"admin/internal/types"
)

// TestBuildAdminUpdatesIgnoresStatusAndMFAFields 验证编辑管理员基础资料不会再混入状态和 MFA 专用字段。
func TestBuildAdminUpdatesIgnoresStatusAndMFAFields(t *testing.T) {
	realName := "新姓名"
	req := &types.UpdateAdminReq{
		RealName:     &realName,
		Status:       new(0),
		MfaStatus:    new(1),
		MfaSecureKey: new("JBSWY3DPEHPK3PXP"),
	}
	old := &model.Admin{
		RealName:     "原姓名",
		Status:       1,
		MfaStatus:    0,
		MfaSecureKey: "RCABDVITFNQJJ4VJ",
	}
	updates := buildAdminUpdates(req, old)
	if updates["real_name"] != realName {
		t.Fatalf("buildAdminUpdates() real_name = %v, want %q", updates["real_name"], realName)
	}
	if _, ok := updates["status"]; ok {
		t.Fatalf("buildAdminUpdates() should ignore status updates")
	}
	if _, ok := updates["mfa_status"]; ok {
		t.Fatalf("buildAdminUpdates() should ignore mfa_status updates")
	}
	if _, ok := updates["mfa_secure_key"]; ok {
		t.Fatalf("buildAdminUpdates() should ignore mfa_secure_key updates")
	}
}
