package user

import (
	"strings"
	"testing"

	"admin/internal/config"
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

// TestValidateUserAccountListReq 验证分表阶段后台列表不会退化为扫描用户分表。
func TestValidateUserAccountListReq(t *testing.T) {
	req := &types.UserListReq{
		Username:    "demo",
		GetOrderReq: types.GetOrderReq{OrderBy: "username"},
	}
	if err := validateUserAccountListReq(req); err != nil {
		t.Fatalf("validateUserAccountListReq() error = %v", err)
	}

	req.Email = "demo@example.com"
	if err := validateUserAccountListReq(req); err == nil {
		t.Fatal("expected email filter to be rejected in account-index list")
	}

	req.Email = ""
	req.OrderBy = "lastLoginAt"
	if err := validateUserAccountListReq(req); err == nil {
		t.Fatal("expected unsupported order field to be rejected in account-index list")
	}
}

// TestUserAccountOrderField 验证分表阶段排序字段只映射账号索引可承载列。
func TestUserAccountOrderField(t *testing.T) {
	if got := userAccountOrderField("id"); got != "user_id" {
		t.Fatalf("userAccountOrderField(id) = %q, want user_id", got)
	}
	if got := userAccountOrderField("shardNo"); got != "shard_no" {
		t.Fatalf("userAccountOrderField(shardNo) = %q, want shard_no", got)
	}
}

// TestUseUserAccountListHonorsSplitWriteConfig 验证写入路由切分后列表直接走账号索引。
func TestUseUserAccountListHonorsSplitWriteConfig(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		User: config.UserConfig{RouteShardCount: 10},
	}, svc.Dependencies{})
	logicObj := NewLogic(nil, svcCtx)
	got, err := logicObj.useUserAccountList(nil)
	if err != nil {
		t.Fatalf("useUserAccountList() error = %v", err)
	}
	if !got {
		t.Fatal("useUserAccountList() = false, want true for split write config")
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
