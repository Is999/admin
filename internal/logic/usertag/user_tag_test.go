package usertag

import (
	"strings"
	"testing"

	"admin/internal/config"
	usertagtask "admin/internal/jobs/usertag/task"
	"admin/internal/types"
)

// TestBuildUserTagWorkflowReqTargetedUIDs 验证指定 UID 补算场景。
func TestBuildUserTagWorkflowReqTargetedUIDs(t *testing.T) {
	req := &types.TriggerUserTagWorkflowReq{
		Mode:        "targeted",
		TagTypes:    []int{30, 20},
		UIDs:        []int64{10001, 10002},
		BatchSize:   300,
		WorkerCount: 4,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("校验 targeted 请求失败: %v", err)
	}

	got := buildUserTagWorkflowReq(req, config.UserTagConfig{DefaultShardTotal: 8})

	if got.Name != usertagtask.WorkflowNameUserTagTargeted {
		t.Fatalf("期望使用 targeted 工作流，实际为 %s", got.Name)
	}
	if !strings.HasPrefix(got.UniqueKey, "user_tag:targeted:") {
		t.Fatalf("期望 targeted 工作流使用参数指纹唯一键，实际为 %q", got.UniqueKey)
	}
	if got.ShardTotal != req.WorkerCount {
		t.Fatalf("期望由 worker_count 驱动分片总数，实际为 %d", got.ShardTotal)
	}
	for _, want := range []string{
		"mode=targeted",
		"tag=30",
		"tag=20",
		"uid=10001",
		"uid=10002",
		"batch_size=300",
		"worker_count=4",
	} {
		if !targetExists(got.Targets, want) {
			t.Fatalf("期望目标列表 %#v 中包含 %q", got.Targets, want)
		}
	}
}

// TestBuildUserTagWorkflowReqRecalculate 验证对应场景。
func TestBuildUserTagWorkflowReqRecalculate(t *testing.T) {
	retry := 3
	req := &types.TriggerUserTagWorkflowReq{
		Mode:      usertagtask.ModeRecalculate,
		TagTypes:  []int{30, 5},
		BatchSize: 500,
		Retry:     &retry,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("校验 recalculate 请求失败: %v", err)
	}

	got := buildUserTagWorkflowReq(req, config.UserTagConfig{DefaultShardTotal: 8})
	if got.Name != usertagtask.WorkflowNameUserTagRecalculate {
		t.Fatalf("期望使用 recalculate 工作流，实际为 %s", got.Name)
	}
	if !strings.HasPrefix(got.UniqueKey, "user_tag:recalculate:") {
		t.Fatalf("期望 recalculate 唯一键符合预期，实际为 %q", got.UniqueKey)
	}
	if got.Retry == nil || *got.Retry != retry {
		t.Fatalf("期望透传 retry=%d，实际为 %#v", retry, got.Retry)
	}
	for _, want := range []string{
		"mode=recalculate",
		"tag=30",
		"tag=5",
		"batch_size=500",
	} {
		if !targetExists(got.Targets, want) {
			t.Fatalf("期望目标列表 %#v 中包含 %q", got.Targets, want)
		}
	}
}

// TestBuildUserTagWorkflowReqUniqueKeyUsesCanonicalParams 验证默认唯一键只对完全相同的计算参数去重。
func TestBuildUserTagWorkflowReqUniqueKeyUsesCanonicalParams(t *testing.T) {
	first := &types.TriggerUserTagWorkflowReq{
		Mode:      usertagtask.ModeTargeted,
		TagTypes:  []int{30},
		UIDs:      []int64{10001, 10002},
		BatchSize: 500,
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("校验 first 请求失败: %v", err)
	}
	second := &types.TriggerUserTagWorkflowReq{
		Mode:      usertagtask.ModeTargeted,
		TagTypes:  []int{30},
		UIDs:      []int64{10001, 10003},
		BatchSize: 500,
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("校验 second 请求失败: %v", err)
	}
	sameAsFirst := &types.TriggerUserTagWorkflowReq{
		Mode:      usertagtask.ModeTargeted,
		TagTypes:  []int{30},
		UIDs:      []int64{10002, 10001},
		BatchSize: 500,
	}
	if err := sameAsFirst.Validate(); err != nil {
		t.Fatalf("校验 sameAsFirst 请求失败: %v", err)
	}

	firstKey := buildUserTagWorkflowReq(first, config.UserTagConfig{DefaultShardTotal: 8}).UniqueKey
	secondKey := buildUserTagWorkflowReq(second, config.UserTagConfig{DefaultShardTotal: 8}).UniqueKey
	sameKey := buildUserTagWorkflowReq(sameAsFirst, config.UserTagConfig{DefaultShardTotal: 8}).UniqueKey
	if firstKey == secondKey {
		t.Fatalf("不同 UID 参数不应共用唯一键: %s", firstKey)
	}
	if firstKey != sameKey {
		t.Fatalf("规范化后完全相同的参数应共用唯一键: first=%s same=%s", firstKey, sameKey)
	}
}

// TestBuildUserTagWorkflowReqKeepsExplicitUniqueKey 验证外部自定义唯一键优先级最高。
func TestBuildUserTagWorkflowReqKeepsExplicitUniqueKey(t *testing.T) {
	req := &types.TriggerUserTagWorkflowReq{
		Mode:      usertagtask.ModeTargeted,
		TagTypes:  []int{30},
		UIDs:      []int64{10001},
		UniqueKey: "custom-key",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("校验 targeted 请求失败: %v", err)
	}
	got := buildUserTagWorkflowReq(req, config.UserTagConfig{DefaultShardTotal: 8})
	if got.UniqueKey != "custom-key" {
		t.Fatalf("期望保留外部唯一键，实际为 %q", got.UniqueKey)
	}
}

// targetExists 判断目标列表中是否包含指定目标。
func targetExists(targets []string, want string) bool {
	for _, target := range targets {
		if strings.TrimSpace(target) == want {
			return true
		}
	}
	return false
}
