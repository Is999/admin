package workflow

import (
	"context"
	"testing"

	"admin/internal/jobs/usertag/types"
)

// TestNewServiceRegistersSkeletonStages 验证骨架工作流只注册通用阶段。
func TestNewServiceRegistersSkeletonStages(t *testing.T) {
	service := NewService(context.Background(), nil)
	names := service.StageNames()
	want := map[string]bool{
		types.NodePrepare:      false,
		types.NodeBusinessHook: false,
		types.NodeFinalize:     false,
		types.NodeSyncKafka:    false,
	}
	for _, name := range names {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, ok := range want {
		if !ok {
			t.Fatalf("stage not registered: %s names=%#v", name, names)
		}
	}
}

// TestRunStageRejectsUnknownNode 验证未知阶段不会静默成功。
func TestRunStageRejectsUnknownNode(t *testing.T) {
	service := NewService(context.Background(), nil)
	_, err := service.RunStage(types.WorkflowPayload{
		WorkflowID: "wf1",
		Mode:       types.ModeFull,
		Node:       "missing_stage",
		ShardTotal: 10,
	})
	if err == nil {
		t.Fatal("expected unknown stage error")
	}
}
