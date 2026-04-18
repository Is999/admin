//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import "testing"

// TestRecalculateUserTagReqValidate 验证对应场景。
func TestRecalculateUserTagReqValidate(t *testing.T) {
	ttl := 60
	retry := 2
	timeout := 300
	req := &RecalculateUserTagReq{
		TagTypes:              []int{30, 5, 30, -1},
		TagTypesCamel:         []int{2},
		ShardTotalSnake:       10,
		BatchSizeSnake:        2000,
		WorkerCountSnake:      4,
		DryRunSnake:           true,
		UniqueTTLSecondsSnake: &ttl,
		Retry:                 &retry,
		TimeoutSecondsSnake:   &timeout,
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
		t.Fatalf("snake_case 参数未正确归一化: shard=%d batch=%d worker=%d", req.ShardTotal, req.BatchSize, req.WorkerCount)
	}
	if !req.DryRun || req.UniqueTTLSeconds == nil || *req.UniqueTTLSeconds != ttl || req.Retry == nil || *req.Retry != retry || req.TimeoutSeconds == nil || *req.TimeoutSeconds != timeout {
		t.Fatalf("snake_case 开关字段未正确归一化")
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

// TestTriggerUserTagWorkflowReqSyncSnapshotOnlyRequiresFull 验证对应场景。
func TestTriggerUserTagWorkflowReqSyncSnapshotOnlyRequiresFull(t *testing.T) {
	req := &TriggerUserTagWorkflowReq{Mode: "delta", SyncSnapshotOnly: true}
	if err := req.Validate(); err == nil {
		t.Fatalf("期望返回 syncSnapshotOnly 仅支持 full 模式错误")
	}
}

// TestTriggerUserTagWorkflowReqSyncSnapshotOnlyConflictDryRun 验证对应场景。
func TestTriggerUserTagWorkflowReqSyncSnapshotOnlyConflictDryRun(t *testing.T) {
	req := &TriggerUserTagWorkflowReq{Mode: "full", SyncSnapshotOnly: true, DryRun: true}
	if err := req.Validate(); err == nil {
		t.Fatalf("期望返回 syncSnapshotOnly 与 dryRun 冲突错误")
	}
}

// TestTriggerUserTagWorkflowReqFullRejectsPartialTargets 验证 full 不能被误用成局部 UID 或指定标签补算。
func TestTriggerUserTagWorkflowReqFullRejectsPartialTargets(t *testing.T) {
	req := &TriggerUserTagWorkflowReq{Mode: "full", TagTypes: []int{30}}
	if err := req.Validate(); err == nil {
		t.Fatal("期望 full 指定 tagTypes 被拒绝")
	}
	req = &TriggerUserTagWorkflowReq{Mode: "full", UIDs: []int64{10001}}
	if err := req.Validate(); err == nil {
		t.Fatal("期望 full 指定 uids 被拒绝")
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
