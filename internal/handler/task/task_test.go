package task

import (
	"net/http/httptest"
	"strings"
	"testing"

	"admin/internal/types"
)

// TestParseEnqueueTaskReq_ObjectPayload 确保手动投递接口允许 payload 直接传 JSON 对象。
func TestParseEnqueueTaskReq_ObjectPayload(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"taskType":"order_month_repair","payload":{},"queue":"maintenance","retry":1}`))
	httpReq.Header.Set("Content-Type", "application/json")

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err != nil {
		t.Fatalf("parseEnqueueTaskReq() error = %v", err)
	}
	if string(req.Payload) != "{}" {
		t.Fatalf("Payload = %s, want {}", req.Payload)
	}
	if req.TaskType != "order_month_repair" {
		t.Fatalf("TaskType = %s, want order_month_repair", req.TaskType)
	}
}

// TestParseEnqueueTaskReq_NestedPayload 确保嵌套 payload 不会在参数解析阶段被判定为类型不匹配。
func TestParseEnqueueTaskReq_NestedPayload(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"taskType":"order_month_repair","payload":{"targets":["order_month_repair#archive"]},"queue":"maintenance","retry":1,"processInSeconds":60,"sign":"demo"}`))
	httpReq.Header.Set("Content-Type", "application/json")

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err != nil {
		t.Fatalf("parseEnqueueTaskReq() error = %v", err)
	}
	if got := string(req.Payload); got != `{"targets":["order_month_repair#archive"]}` {
		t.Fatalf("Payload = %s, want nested targets object", got)
	}
	if req.ProcessInSeconds == nil || *req.ProcessInSeconds != 60 {
		t.Fatalf("ProcessInSeconds = %#v, want 60", req.ProcessInSeconds)
	}
}

// TestParseEnqueueTaskReq_MissingPayload 确保缺少 payload 时仍然返回明确的参数错误。
func TestParseEnqueueTaskReq_MissingPayload(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"taskType":"order_month_repair"}`))
	httpReq.Header.Set("Content-Type", "application/json")

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err == nil {
		t.Fatal("parseEnqueueTaskReq() error = nil, want payload validation error")
	}
}
