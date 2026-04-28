package types

import "testing"

// TestSecurityDebugSignReqValidateTrimsSignMeta 验证安全调试台只使用标准签名元数据。
func TestSecurityDebugSignReqValidateTrimsSignMeta(t *testing.T) {
	req := &SecurityDebugSignReq{
		AppID:       "demo-app",
		RequestID:   " request-001 ",
		Timestamp:   " 1700000000 ",
		PayloadText: `{"username":"admin"}`,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.RequestID != "request-001" {
		t.Fatalf("RequestID = %q, want request-001", req.RequestID)
	}
	if req.Timestamp != "1700000000" {
		t.Fatalf("Timestamp = %q, want 1700000000", req.Timestamp)
	}
}
