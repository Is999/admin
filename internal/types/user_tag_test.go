//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"encoding/json"
	"testing"

	tasklimits "admin/internal/task/limits"
)

// TestRecalculateUserTagReqValidate 验证对应场景。
func TestRecalculateUserTagReqValidate(t *testing.T) {
	ttl := 60
	retry := 2
	timeout := 300
	req := &RecalculateUserTagReq{
		TagTypes:         []int{30, 5, 30, -1, 2},
		ShardTotal:       10,
		BatchSize:        2000,
		WorkerCount:      4,
		DryRun:           true,
		UniqueTTLSeconds: &ttl,
		Retry:            &retry,
		TimeoutSeconds:   &timeout,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("校验请求失败: %v", err)
	}
	if got := len(req.TagTypes); got != 3 {
		t.Fatalf("期望规范化后共有 3 个标签类型，实际为 %d: %v", got, req.TagTypes)
	}
	if req.TagTypes[0] != 2 || req.TagTypes[1] != 5 || req.TagTypes[2] != 30 {
		t.Fatalf("规范化后的标签类型不符合预期: %v", req.TagTypes)
	}
	if req.ShardTotal != 10 || req.BatchSize != 2000 || req.WorkerCount != 4 {
		t.Fatalf("重算参数被意外改写: shard=%d batch=%d worker=%d", req.ShardTotal, req.BatchSize, req.WorkerCount)
	}
	if !req.DryRun || req.UniqueTTLSeconds == nil || *req.UniqueTTLSeconds != ttl || req.Retry == nil || *req.Retry != retry || req.TimeoutSeconds == nil || *req.TimeoutSeconds != timeout {
		t.Fatalf("重算开关字段被意外改写")
	}
}

// TestUserTagReqValidateRejectsResourceOverridesAboveHardLimits 校验两个用户标签入口都继承任务重试和超时硬上限。
func TestUserTagReqValidateRejectsResourceOverridesAboveHardLimits(t *testing.T) {
	retry := tasklimits.MaxRetry + 1
	timeout := tasklimits.MaxTimeoutSeconds + 1
	tests := []interface{ Validate() error }{
		&TriggerUserTagWorkflowReq{Mode: "full", DryRun: true, Retry: &retry},
		&TriggerUserTagWorkflowReq{Mode: "full", DryRun: true, TimeoutSeconds: &timeout},
		&RecalculateUserTagReq{TagTypes: []int{30}, DryRun: true, Retry: &retry},
		&RecalculateUserTagReq{TagTypes: []int{30}, DryRun: true, TimeoutSeconds: &timeout},
	}
	for index, req := range tests {
		if err := req.Validate(); err == nil {
			t.Fatalf("场景 %d 期望超过任务资源硬上限时返回错误", index)
		}
	}
}

// TestRecalculateUserTagReqValidateRequiresTagTypes 验证对应场景。
func TestRecalculateUserTagReqValidateRequiresTagTypes(t *testing.T) {
	req := &RecalculateUserTagReq{}
	if err := req.Validate(); err == nil {
		t.Fatalf("期望返回 tag_types 必填错误")
	}
}

// TestUserTagReqValidateRejectsNegativeRetry 验证用户标签触发和重算入口都会拒绝非法重试次数。
func TestUserTagReqValidateRejectsNegativeRetry(t *testing.T) {
	retry := -1
	triggerReq := &TriggerUserTagWorkflowReq{Mode: "full", Retry: &retry}
	if err := triggerReq.Validate(); err == nil {
		t.Fatalf("期望触发工作流拒绝负数 retry")
	}
	recalculateReq := &RecalculateUserTagReq{TagTypes: []int{30}, Retry: &retry}
	if err := recalculateReq.Validate(); err == nil {
		t.Fatalf("期望标签重算拒绝负数 retry")
	}
}

// TestUserTagReqValidateRejectsProductionRun 验证骨架接口不会创建注定失败或覆盖空结果的正式任务。
func TestUserTagReqValidateRejectsProductionRun(t *testing.T) {
	triggerReq := &TriggerUserTagWorkflowReq{Mode: "delta"}
	if err := triggerReq.Validate(); err == nil {
		t.Fatal("期望触发工作流拒绝非 dryRun 请求")
	}
	recalculateReq := &RecalculateUserTagReq{TagTypes: []int{30}}
	if err := recalculateReq.Validate(); err == nil {
		t.Fatal("期望标签重算拒绝非 dry_run 请求")
	}
}

// TestTriggerUserTagWorkflowReqFullRejectsPartialTargets 验证 full 不能被误用成局部 UID 或指定标签补算。
func TestTriggerUserTagWorkflowReqFullRejectsPartialTargets(t *testing.T) {
	req := &TriggerUserTagWorkflowReq{Mode: "full", TagTypes: []int{30}}
	if err := req.Validate(); err == nil {
		t.Fatal("期望 full 指定 tagTypes 被拒绝")
	}
	req = &TriggerUserTagWorkflowReq{Mode: "full", UIDs: []string{"10001"}}
	if err := req.Validate(); err == nil {
		t.Fatal("期望 full 指定 uids 被拒绝")
	}
}

// TestTriggerUserTagWorkflowReqKeepsSnowflakeUIDPrecision 验证 UID 字符串不会经过 JavaScript number 契约丢失精度。
func TestTriggerUserTagWorkflowReqKeepsSnowflakeUIDPrecision(t *testing.T) {
	req := &TriggerUserTagWorkflowReq{
		Mode:     "targeted",
		TagTypes: []int{30},
		UIDs:     []string{"9223372036854775807", "778919762005200896", "778919762005200896"},
		DryRun:   true,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	want := []string{"778919762005200896", "9223372036854775807"}
	if len(req.UIDs) != len(want) || req.UIDs[0] != want[0] || req.UIDs[1] != want[1] {
		t.Fatalf("UIDs = %#v, want %#v", req.UIDs, want)
	}
	invalid := &TriggerUserTagWorkflowReq{Mode: "targeted", TagTypes: []int{30}, UIDs: []string{"9007199254740993.0"}, DryRun: true}
	if err := invalid.Validate(); err == nil {
		t.Fatal("期望拒绝非十进制 int64 UID 字符串")
	}
}

// TestTriggerUserTagWorkflowReqJSONRequiresStringUIDs 验证 HTTP 契约拒绝会在浏览器侧丢精度的数字 UID。
func TestTriggerUserTagWorkflowReqJSONRequiresStringUIDs(t *testing.T) {
	var req TriggerUserTagWorkflowReq
	if err := json.Unmarshal([]byte(`{"mode":"targeted","tagTypes":[30],"uids":["778919762005200896"],"dryRun":true}`), &req); err != nil {
		t.Fatalf("Unmarshal(string UID) error = %v", err)
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate(string UID) error = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"mode":"targeted","tagTypes":[30],"uids":[778919762005200896],"dryRun":true}`), &req); err == nil {
		t.Fatal("期望 JSON 数字 UID 被 string[] 契约拒绝")
	}
}

// TestReleaseUserTagWorkflowLeaseReqValidate 验证人工释放锁请求会归一化 mode 并强制要求释放原因。
func TestReleaseUserTagWorkflowLeaseReqValidate(t *testing.T) {
	req := &ReleaseUserTagWorkflowLeaseReq{
		WorkflowID: " wf-full ",
		Reason:     " 已确认原 workflow 失败且无切表/Kafka 同步任务运行 ",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("校验释放锁请求失败: %v", err)
	}
	if req.WorkflowID != "wf-full" || req.Mode != "full" {
		t.Fatalf("释放锁请求未正确归一化: %#v", req)
	}

	req = &ReleaseUserTagWorkflowLeaseReq{WorkflowID: "wf-full", Mode: "unknown", Reason: "确认释放"}
	if err := req.Validate(); err == nil {
		t.Fatal("期望非法 mode 被拒绝")
	}
	req = &ReleaseUserTagWorkflowLeaseReq{WorkflowID: "wf-full", Mode: "full"}
	if err := req.Validate(); err == nil {
		t.Fatal("期望缺少释放原因被拒绝")
	}
}
