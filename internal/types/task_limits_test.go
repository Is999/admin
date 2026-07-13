package types

import (
	"encoding/json"
	"testing"

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
