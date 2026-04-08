package stage

import (
	"testing"

	"admin_cron/internal/jobs/usertag/runtimectx"
	"admin_cron/internal/jobs/usertag/types"
)

// TestBusinessHookStageIsNoop 验证默认业务扩展节点只是占位骨架。
func TestBusinessHookStageIsNoop(t *testing.T) {
	result, err := NewBusinessHookStage().Run(runtimectx.New(nil, nil, types.RuntimeOptions{Mode: types.ModeDelta}, types.NodeBusinessHook), nil)
	if err != nil {
		t.Fatalf("business hook should be noop: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("business hook should be marked skipped: %#v", result)
	}
}
