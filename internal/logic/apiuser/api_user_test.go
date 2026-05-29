package apiuser

import (
	"strings"
	"testing"

	"admin/internal/svc"
	"admin/internal/types"
)

// TestBuildAPIUserProfileUpdatesKeepsExplicitEmptyValue 验证空字符串是显式清空资料，不应被忽略。
func TestBuildAPIUserProfileUpdatesKeepsExplicitEmptyValue(t *testing.T) {
	nickname := "  新昵称  "
	email := "  "
	req := &types.UpdateAPIUserReq{
		Nickname: &nickname,
		Email:    &email,
	}
	updates := buildAPIUserProfileUpdates(req)
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

// TestAPIUserDatabaseDefaultsToAPINamedDB 验证前台用户库默认落到命名库 api，而不是后台主库。
func TestAPIUserDatabaseDefaultsToAPINamedDB(t *testing.T) {
	if got := apiUserDatabase(""); got != svc.DbName("api") {
		t.Fatalf("apiUserDatabase(empty) = %q, want api", got)
	}
	if got := apiUserDatabase(" main "); got != svc.DatabaseMain {
		t.Fatalf("apiUserDatabase(main) = %q, want main", got)
	}
	if got := apiUserDatabase(" api_read "); got != svc.DbName("api_read") {
		t.Fatalf("apiUserDatabase(api_read) = %q, want api_read", got)
	}
}

// TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics 验证写库后的同步失败只作为可重试告警返回。
func TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics(t *testing.T) {
	resp := apiRuntimeSyncWarning(7, types.APIUserRuntimeSyncResp{Enabled: true}, "资料已更新", assertError("timeout"))
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
