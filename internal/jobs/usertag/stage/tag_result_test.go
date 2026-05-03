package stage

import (
	"testing"

	"admin/internal/jobs/usertag/runtimectx"
	"admin/internal/jobs/usertag/types"
)

// TestSkeletonStageIsNoop 验证默认通用阶段只是可调度骨架。
func TestSkeletonStageIsNoop(t *testing.T) {
	result, err := NewCollectScopeStage().Run(runtimectx.New(nil, nil, types.RuntimeOptions{Mode: types.ModeDelta}, types.NodeCollectScope), nil)
	if err != nil {
		t.Fatalf("skeleton stage should be noop: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("skeleton stage should be marked skipped: %#v", result)
	}
}
