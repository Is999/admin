package types

import (
	"encoding/json"
	"testing"
)

const (
	// testUserJSONSnowflakeID 表示超过 JS 安全整数范围的前台用户雪花 ID。
	testUserJSONSnowflakeID int64 = 778919762005200896
	// testUserJSONSnowflakeIDString 表示前端应接收的前台用户雪花 ID 字符串。
	testUserJSONSnowflakeIDString = "778919762005200896"
	// testUserJSONAuthVersion 表示超过 JS 安全整数范围的认证版本测试值。
	testUserJSONAuthVersion uint64 = 9007199254740993
	// testUserJSONAuthVersionString 表示前端应接收的认证版本字符串。
	testUserJSONAuthVersionString = "9007199254740993"
)

// assertJSONStringField 校验响应 JSON 字段以字符串形式输出。
func assertJSONStringField(t *testing.T, value any, field string, want string) {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	got, ok := payload[field].(string)
	if !ok {
		t.Fatalf("field %s should be string, got %T in %s", field, payload[field], body)
	}
	if got != want {
		t.Fatalf("field %s = %q, want %q", field, got, want)
	}
}

// TestUserItemMarshalSnowflakeIDAsString 验证前台用户列表 ID 以字符串输出，避免前端精度丢失。
func TestUserItemMarshalSnowflakeIDAsString(t *testing.T) {
	assertJSONStringField(t, UserItem{ID: testUserJSONSnowflakeID}, "id", testUserJSONSnowflakeIDString)
}

// TestUserRuntimeSyncRespMarshalUserIDAsString 验证运行期同步响应的用户 ID 以字符串输出。
func TestUserRuntimeSyncRespMarshalUserIDAsString(t *testing.T) {
	assertJSONStringField(t, UserRuntimeSyncResp{UserID: testUserJSONSnowflakeID}, "userId", testUserJSONSnowflakeIDString)
}

// TestUserRuntimeSyncRespMarshalAuthVersionAsString 验证认证版本以字符串输出，避免前端精度丢失。
func TestUserRuntimeSyncRespMarshalAuthVersionAsString(t *testing.T) {
	assertJSONStringField(t, UserRuntimeSyncResp{AuthVersion: testUserJSONAuthVersion}, "authVersion", testUserJSONAuthVersionString)
}

// TestUserListMetaMarshalNextCursorIDAsString 验证列表游标以字符串输出，避免前端翻页丢失精度。
func TestUserListMetaMarshalNextCursorIDAsString(t *testing.T) {
	assertJSONStringField(t, UserListMeta{NextCursorID: testUserJSONSnowflakeID}, "nextCursorId", testUserJSONSnowflakeIDString)
}
