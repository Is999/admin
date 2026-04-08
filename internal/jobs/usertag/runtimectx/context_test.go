package runtimectx

import (
	"strings"
	"testing"

	"admin_cron/internal/jobs/usertag/types"
)

// TestRuntimeContextTraceFields 验证日志链路字段格式稳定。
func TestRuntimeContextTraceFields(t *testing.T) {
	ctx := New(nil, nil, types.RuntimeOptions{WorkflowID: "wf1", Mode: types.ModeDelta, ShardIndex: 2, ShardTotal: 10}, types.NodeBusinessHook).WithBatch(7)
	text := ctx.TraceFields()
	for _, want := range []string{"workflow_id=wf1", "mode=delta", "node=business_hook", "shard=2/10", "batch=7"} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace fields missing %s: %s", want, text)
		}
	}
}

// TestRuntimeContextWrap 验证跨层错误包装携带 usertag 链路信息。
func TestRuntimeContextWrap(t *testing.T) {
	ctx := New(nil, nil, types.RuntimeOptions{WorkflowID: "wf1", Mode: types.ModeFull, ShardIndex: 0, ShardTotal: 10}, types.NodePrepare)
	err := ctx.Wrap(assertErr("boom"), "准备失败")
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !strings.Contains(err.Error(), "workflow_id=wf1") {
		t.Fatalf("wrapped error missing workflow id: %v", err)
	}
}

// assertErr 是测试用固定错误。
type assertErr string

// Error 返回测试错误文本。
func (e assertErr) Error() string {
	return string(e)
}
