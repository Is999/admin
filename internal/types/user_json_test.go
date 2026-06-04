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
)

// assertJSONStringField 校验响应 JSON 字段以字符串形式输出。
func assertJSONStringField(t *testing.T, value any, field string) {
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
	if got != testUserJSONSnowflakeIDString {
		t.Fatalf("field %s = %q, want %q", field, got, testUserJSONSnowflakeIDString)
	}
}

func TestUserItemMarshalSnowflakeIDAsString(t *testing.T) {
	assertJSONStringField(t, UserItem{ID: testUserJSONSnowflakeID}, "id")
}

func TestUserRuntimeSyncRespMarshalUserIDAsString(t *testing.T) {
	assertJSONStringField(t, UserRuntimeSyncResp{UserID: testUserJSONSnowflakeID}, "userId")
}
