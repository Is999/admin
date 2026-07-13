package stage

import (
	"context"
	"testing"

	"admin/internal/jobs/usertag/runtimectx"
	"admin/internal/jobs/usertag/types"
)

// TestSkeletonStageIsNoop 验证 dry_run 下默认通用阶段只是可调度骨架。
func TestSkeletonStageIsNoop(t *testing.T) {
	result, err := NewCollectScopeStage().Run(runtimectx.New(context.Background(), nil, types.RuntimeOptions{Mode: types.ModeDelta, DryRun: true}, types.NodeCollectScope), nil)
	if err != nil {
		t.Fatalf("skeleton stage should be noop: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("skeleton stage should be marked skipped: %#v", result)
	}
}

// TestSkeletonStagesRejectProductionRun 验证未实现业务计算时不会清表、切表或伪装成功。
func TestSkeletonStagesRejectProductionRun(t *testing.T) {
	ctx := runtimectx.New(context.Background(), nil, types.RuntimeOptions{Mode: types.ModeFull}, types.NodePrepare)
	if _, err := NewPrepareStage(nil).Run(ctx, nil); err == nil {
		t.Fatal("prepare stage should reject non-dry-run before accessing repository")
	}
	if _, err := NewCollectScopeStage().Run(ctx.WithNode(types.NodeCollectScope), nil); err == nil {
		t.Fatal("skeleton stage should reject non-dry-run")
	}
	if _, err := NewFinalizeStage(nil).Run(ctx.WithNode(types.NodeFinalize), nil); err == nil {
		t.Fatal("finalize stage should reject non-dry-run before accessing repository")
	}
	if _, err := NewDispatchHooksStage(nil).Run(ctx.WithNode(types.NodeDispatchHooks), nil); err == nil {
		t.Fatal("dispatch hooks stage should reject non-dry-run before accessing repository")
	}
}
