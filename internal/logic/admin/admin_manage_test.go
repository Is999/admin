package admin

import (
	"testing"

	"admin/internal/model"
	"admin/internal/types"
)

// TestBuildAdminUpdatesUsesEditableProfileFields 验证编辑管理员时只构建真实存在的基础资料字段。
func TestBuildAdminUpdatesUsesEditableProfileFields(t *testing.T) {
	realName := " 新姓名 "
	email := " new@example.com "
	phone := " 13800000000 "
	avatar := " /avatars/new.png "
	description := " 新备注 "
	req := &types.UpdateAdminReq{
		RealName:    &realName,
		Email:       &email,
		Phone:       &phone,
		Avatar:      &avatar,
		Description: &description,
	}
	old := &model.Admin{
		RealName:    "原姓名",
		Email:       "old@example.com",
		Phone:       "13900000000",
		Avatar:      "/avatars/old.png",
		Description: "原备注",
	}
	updates := buildAdminUpdates(req, old)
	want := map[string]string{
		"real_name":   "新姓名",
		"email":       "new@example.com",
		"phone":       "13800000000",
		"avatar":      "/avatars/new.png",
		"description": "新备注",
	}
	for field, value := range want {
		if updates[field] != value {
			t.Fatalf("buildAdminUpdates() %s = %v, want %q", field, updates[field], value)
		}
	}
	if _, ok := updates["updated_at"]; !ok {
		t.Fatal("buildAdminUpdates() should set updated_at when profile changes")
	}
	if len(updates) != len(want)+1 {
		t.Fatalf("buildAdminUpdates() = %#v, want only editable profile fields and updated_at", updates)
	}
}
