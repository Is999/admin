package middleware

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestRequestJSONMapRejectsTrailingContent 校验安全请求体不能夹带第二个 JSON 值。
func TestRequestJSONMapRejectsTrailingContent(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", bytes.NewBufferString(`{"username":"first"} {"username":"second"}`))

	if _, err := requestJSONMap(req); err == nil {
		t.Fatal("期望尾随 JSON 内容被拒绝")
	}
}

// TestRequestParamsAcceptsSingleJSONObject 校验合法单对象仍可参与签名参数提取。
func TestRequestParamsAcceptsSingleJSONObject(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", bytes.NewBufferString("{\"count\":1}\n"))

	params, err := requestParams(req)
	if err != nil {
		t.Fatalf("解析合法 JSON 失败: %v", err)
	}
	if params["count"] != json.Number("1") {
		t.Fatalf("期望保留 JSON 数字精度，实际 %#v", params["count"])
	}
}
