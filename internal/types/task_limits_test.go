package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tasklimits "admin/internal/task/limits"
)

// TestTaskRequestsRejectResourceOverridesAboveHardLimits 校验工作流和通用任务入口不会接受无界资源参数。
func TestTaskRequestsRejectResourceOverridesAboveHardLimits(t *testing.T) {
	retry := tasklimits.MaxRetry + 1
	shardTotal := tasklimits.MaxShardTotal + 1
	timeout := tasklimits.MaxTimeoutSeconds + 1
	tests := []struct {
		name string                        // 用例名称
		req  interface{ Validate() error } // 待校验请求
	}{
		{
			name: "workflow retry",
			req:  &TriggerTaskWorkflowReq{Name: "demo.workflow", Retry: &retry},
		},
		{
			name: "workflow timeout",
			req:  &TriggerTaskWorkflowReq{Name: "demo.workflow", TimeoutSeconds: &timeout},
		},
		{
			name: "workflow shard total",
			req:  &TriggerTaskWorkflowReq{Name: "demo.workflow", ShardTotal: shardTotal},
		},
		{
			name: "task retry",
			req:  &EnqueueTaskReq{TaskType: "demo:task", Payload: json.RawMessage(`{}`), Retry: &retry},
		},
		{
			name: "task timeout",
			req:  &EnqueueTaskReq{TaskType: "demo:task", Payload: json.RawMessage(`{}`), TimeoutSeconds: &timeout},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.req.Validate(); err == nil {
				t.Fatal("期望超过任务资源硬上限的请求被拒绝")
			}
		})
	}
}

// TestTriggerTaskWorkflowReqRejectsNegativeShardTotal 校验手动触发入口不会把非法负分片静默改成默认值。
func TestTriggerTaskWorkflowReqRejectsNegativeShardTotal(t *testing.T) {
	req := &TriggerTaskWorkflowReq{Name: "demo.workflow", ShardTotal: -1}
	if err := req.Validate(); err == nil {
		t.Fatal("期望负分片数被拒绝")
	}
}

// TestTriggerTaskWorkflowReqRejectsConflictingSchedule 校验工作流入口拒绝相互冲突的调度方式。
func TestTriggerTaskWorkflowReqRejectsConflictingSchedule(t *testing.T) {
	delay := 10
	req := &TriggerTaskWorkflowReq{
		Name:             "demo.workflow",
		ProcessAt:        time.Now().Add(time.Hour).Format(time.RFC3339),
		ProcessInSeconds: &delay,
	}
	if err := req.Validate(); err == nil {
		t.Fatal("期望同时设置 processAt 和 processInSeconds 被拒绝")
	}
}

// TestTaskRequestsRejectScheduleBeyondRedisWindow 校验一次性任务不能提前超过三十天占用 scheduled 状态。
func TestTaskRequestsRejectScheduleBeyondRedisWindow(t *testing.T) {
	delay := tasklimits.MaxScheduleDelaySeconds + 1
	for _, req := range []interface{ Validate() error }{
		&TriggerTaskWorkflowReq{Name: "demo.workflow", ProcessInSeconds: &delay},
		&EnqueueTaskReq{TaskType: "demo:task", Payload: json.RawMessage(`{}`), ProcessInSeconds: &delay},
		&TriggerTaskWorkflowReq{Name: "demo.workflow", ProcessAt: time.Now().Add(31 * 24 * time.Hour).Format(time.RFC3339)},
		&EnqueueTaskReq{TaskType: "demo:task", Payload: json.RawMessage(`{}`), ProcessAt: time.Now().Add(31 * 24 * time.Hour).Format(time.RFC3339)},
	} {
		if err := req.Validate(); err == nil {
			t.Fatal("期望超过三十天的任务调度被拒绝")
		}
	}
}

// TestEnqueueTaskReqRejectsOversizedPayload 校验所有通用任务调用方都遵守一 MiB 负载边界。
func TestEnqueueTaskReqRejectsOversizedPayload(t *testing.T) {
	req := &EnqueueTaskReq{
		TaskType: "demo:task",
		Payload:  json.RawMessage(`{"data":"` + strings.Repeat("x", tasklimits.MaxPayloadBytes) + `"}`),
	}
	if err := req.Validate(); err == nil {
		t.Fatal("期望超过一 MiB 的任务负载被拒绝")
	}
}

// TestTriggerTaskWorkflowReqRejectsOversizedTargets 校验工作流目标数量、单项和总量都有明确边界。
func TestTriggerTaskWorkflowReqRejectsOversizedTargets(t *testing.T) {
	tests := []struct {
		name    string   // 用例名称
		targets []string // 待校验目标
	}{
		{name: "count", targets: make([]string, tasklimits.MaxWorkflowTargets+1)},
		{name: "single", targets: []string{strings.Repeat("x", tasklimits.MaxWorkflowTargetBytes+1)}},
		{name: "total", targets: make([]string, tasklimits.MaxWorkflowTargetsBytes/tasklimits.MaxWorkflowTargetBytes+1)},
	}
	for index := range tests[0].targets {
		tests[0].targets[index] = "target-" + strings.Repeat("x", index%8)
	}
	for index := range tests[2].targets {
		tests[2].targets[index] = strings.Repeat("x", tasklimits.MaxWorkflowTargetBytes)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &TriggerTaskWorkflowReq{Name: "demo.workflow", Targets: tt.targets}
			if err := req.Validate(); err == nil {
				t.Fatal("期望超限工作流目标被拒绝")
			}
		})
	}
}
