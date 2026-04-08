package types

import "testing"

// TestSecurityDebugSignReqValidateAcceptsTraceID 验证安全调试台兼容真实请求头中的 traceId 字段。
func TestSecurityDebugSignReqValidateAcceptsTraceID(t *testing.T) {
	req := &SecurityDebugSignReq{
		AppID:       "demo-app",
		TraceID:     "trace-001",
		PayloadText: `{"username":"admin"}`,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.RequestID != "trace-001" || req.TraceID != "trace-001" {
		t.Fatalf("期望 traceId 同步为 requestId，实际 requestId=%q traceId=%q", req.RequestID, req.TraceID)
	}
}

// TestSecurityDebugSignReqValidateKeepsRequestID 验证同时传入 requestId 与 traceId 时优先使用显式 requestId。
func TestSecurityDebugSignReqValidateKeepsRequestID(t *testing.T) {
	req := &SecurityDebugSignReq{
		AppID:       "demo-app",
		RequestID:   "request-001",
		TraceID:     "trace-001",
		PayloadText: `{"username":"admin"}`,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.RequestID != "request-001" || req.TraceID != "request-001" {
		t.Fatalf("期望显式 requestId 优先生效，实际 requestId=%q traceId=%q", req.RequestID, req.TraceID)
	}
}
