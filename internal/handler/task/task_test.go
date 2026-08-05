package task

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	tasklimits "admin/internal/task/limits"
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

// TestParseEnqueueTaskReq_RejectsOversizedBody 确保解析层在分配无界请求体前拒绝超限内容。
func TestParseEnqueueTaskReq_RejectsOversizedBody(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(strings.Repeat("x", enqueueTaskBodyMaxBytes+1)))

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err == nil {
		t.Fatal("parseEnqueueTaskReq() error = nil, want oversized body error")
	}
}

// TestParseEnqueueTaskReq_RejectsOversizedPayload 确保任务负载不能超过统一的一 MiB 硬上限。
func TestParseEnqueueTaskReq_RejectsOversizedPayload(t *testing.T) {
	body := `{"taskType":"order_month_repair","payload":{"data":"` + strings.Repeat("x", tasklimits.MaxPayloadBytes) + `"}}`
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(body))

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err == nil {
		t.Fatal("parseEnqueueTaskReq() error = nil, want oversized payload error")
	}
}

// TestParseEnqueueTaskReq_RejectsTrailingJSON 确保单个请求不能拼接多份 JSON 文档绕过审计语义。
func TestParseEnqueueTaskReq_RejectsTrailingJSON(t *testing.T) {
	body := `{"taskType":"order_month_repair","payload":{}} {"taskType":"other","payload":{}}`
	httpReq := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(body))

	var req types.EnqueueTaskReq
	if err := parseEnqueueTaskReq(httpReq, &req); err == nil {
		t.Fatal("parseEnqueueTaskReq() error = nil, want trailing JSON error")
	}
}

// TestParseTaskJSONReq_RejectsOversizedWorkflowBody 确保工作流入口不会无界读取目标列表。
func TestParseTaskJSONReq_RejectsOversizedWorkflowBody(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/api/tasks/workflows", strings.NewReader(strings.Repeat("x", triggerWorkflowBodyMaxBytes+1)))

	var req types.TriggerTaskWorkflowReq
	if err := parseTaskJSONReq(httpReq, &req, triggerWorkflowBodyMaxBytes); err == nil {
		t.Fatal("parseTaskJSONReq() error = nil, want oversized workflow body error")
	}
}

// TestParseTaskJSONReq_AllowsEscapedTargetsWithinDecodedLimit 确保合法目标不会因 JSON 转义膨胀被解析层提前拒绝。
func TestParseTaskJSONReq_AllowsEscapedTargetsWithinDecodedLimit(t *testing.T) {
	target := strings.Repeat("\x01", tasklimits.MaxWorkflowTargetBytes)
	targets := make([]string, tasklimits.MaxWorkflowTargetsBytes/tasklimits.MaxWorkflowTargetBytes)
	for index := range targets {
		targets[index] = target
	}
	reqBody, err := json.Marshal(types.TriggerTaskWorkflowReq{
		Name:    "cache-refresh",
		Targets: targets,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	httpReq := httptest.NewRequest("POST", "/api/tasks/workflows", strings.NewReader(string(reqBody)))

	var req types.TriggerTaskWorkflowReq
	if err = parseTaskJSONReq(httpReq, &req, triggerWorkflowBodyMaxBytes); err != nil {
		t.Fatalf("parseTaskJSONReq() error = %v", err)
	}
}
