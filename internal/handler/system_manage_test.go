package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"admin_cron/internal/types"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

// TestParseSysConfigJSONRequestAcceptsScalarValue 确保系统配置 JSON 请求支持 number/bool 等标量值。
func TestParseSysConfigJSONRequestAcceptsScalarValue(t *testing.T) {
	body := `{"type":6,"pid":6,"uuid":"adminMFACheckEnable","title":"Admin校验MFA设备验证码","page":"","remark":"demo","example":1,"value":1,"version":0}`
	req := httptest.NewRequest(http.MethodPatch, "/api/dicts/7", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = pathvar.WithVars(req, map[string]string{"id": "7"})

	var parsed types.SaveSysConfigReq
	if err := parseSysConfigJSONRequest(req, &parsed); err != nil {
		t.Fatalf("expected parse success, got %v", err)
	}
	if parsed.ID != 7 {
		t.Fatalf("id = %d, want 7", parsed.ID)
	}

	valueRaw, err := parsed.ValueRawMessage()
	if err != nil {
		t.Fatalf("expected value normalize success, got %v", err)
	}
	exampleRaw, err := parsed.ExampleRawMessage()
	if err != nil {
		t.Fatalf("expected example normalize success, got %v", err)
	}
	if string(valueRaw) != "1" {
		t.Fatalf("value raw = %s, want 1", string(valueRaw))
	}
	if string(exampleRaw) != "1" {
		t.Fatalf("example raw = %s, want 1", string(exampleRaw))
	}
}
