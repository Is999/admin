package user

import (
	"strings"
	"testing"

	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
)

// TestBuildUserProfileUpdatesKeepsExplicitEmptyValue 验证空字符串是显式清空资料，不应被忽略。
func TestBuildUserProfileUpdatesKeepsExplicitEmptyValue(t *testing.T) {
	nickname := "  新昵称  "
	email := "  "
	req := &types.UpdateUserReq{
		Nickname: &nickname,
		Email:    &email,
	}
	updates := buildUserProfileUpdates(req)
	if updates["nickname"] != "新昵称" {
		t.Fatalf("nickname update = %v, want trimmed nickname", updates["nickname"])
	}
	if value, ok := updates["email"]; !ok || value != "" {
		t.Fatalf("email update = %v ok=%v, want explicit empty value", value, ok)
	}
	if _, ok := updates["phone"]; ok {
		t.Fatalf("phone should not be updated when request field is nil")
	}
}

// TestUserDatabaseUsesMainDB 验证前台用户管理固定使用默认主库。
func TestUserDatabaseUsesMainDB(t *testing.T) {
	if userDatabase != svc.DatabaseMain {
		t.Fatalf("userDatabase = %q, want %q", userDatabase, svc.DatabaseMain)
	}
}

// TestUserModelUsesUserTable 验证后台管理前台用户时读取统一 user 表。
func TestUserModelUsesUserTable(t *testing.T) {
	if model.TableNameUser != "user" {
		t.Fatalf("TableNameUser = %q, want user", model.TableNameUser)
	}
	if tableName := (&model.User{}).TableName(); tableName != "user" {
		t.Fatalf("User.TableName() = %q, want user", tableName)
	}
}

// TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics 验证写库后的同步失败只作为可重试告警返回。
func TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics(t *testing.T) {
	resp := apiRuntimeSyncWarning(7, types.UserRuntimeSyncResp{Enabled: true}, "资料已更新", assertError("timeout"))
	if resp.Success {
		t.Fatal("sync warning should mark success false")
	}
	if !resp.Enabled || resp.UserID != 7 {
		t.Fatalf("sync warning response = %+v, want enabled user 7", resp)
	}
	if !strings.Contains(resp.Message, "资料已更新") || !strings.Contains(resp.Message, "timeout") {
		t.Fatalf("sync warning message = %q, want fallback and cause", resp.Message)
	}
}

// assertError 是测试用固定错误文本。
type assertError string

// Error 返回固定错误文本。
func (e assertError) Error() string {
	return string(e)
}
