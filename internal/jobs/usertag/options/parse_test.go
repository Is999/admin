package options

import (
	"strings"
	"testing"

	"admin_cron/internal/jobs/usertag/types"
)

// TestParseOptionsRejectsFilterHash 验证多维筛选 hash 不能作为标签计算来源。
func TestParseOptionsRejectsFilterHash(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{
		Mode:    types.ModeTargeted,
		Targets: []string{"filter_hash=abc", "tag=30"},
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected filterHash error")
	}
	if !strings.Contains(err.Error(), "多维筛选 hash") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseOptionsTargetedRequiresUIDs 验证 targeted 模式必须显式传入 UID 和标签类型。
func TestParseOptionsTargetedRequiresUIDs(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{
		Mode:     types.ModeTargeted,
		TagTypes: []int{30},
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected targeted uid error")
	}
	if !strings.Contains(err.Error(), "uids") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseOptionsRecalculateRequiresTagTypes 验证指定标签重算必须提供标签类型。
func TestParseOptionsRecalculateRequiresTagTypes(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{Mode: types.ModeRecalculate}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected tag_types error")
	}
	if !strings.Contains(err.Error(), "tag_types") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseOptionsFullRejectsPartialTargets 验证 full 不能从通用任务入口绕过局部目标限制。
func TestParseOptionsFullRejectsPartialTargets(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{
		Mode:    types.ModeFull,
		Targets: []string{"tag=30"},
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected full tag target error")
	}
	if !strings.Contains(err.Error(), "full 模式") {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ParseOptions(types.WorkflowPayload{
		Mode:    types.ModeFull,
		Targets: []string{"uid=10001"},
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected full uid target error")
	}
}

// TestParseOptionsNormalizesSets 验证 UID 和标签类型会去重、过滤非法值并排序。
func TestParseOptionsNormalizesSets(t *testing.T) {
	opts, err := ParseOptions(types.WorkflowPayload{
		Mode:     types.ModeTargeted,
		TagTypes: []int{40, 0, 30, 40},
		UIDs:     []int64{9, -1, 3, 9},
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if got := opts.TagTypes; len(got) != 2 || got[0] != 30 || got[1] != 40 {
		t.Fatalf("unexpected tag types: %#v", got)
	}
	if got := opts.UIDs; len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("unexpected uids: %#v", got)
	}
}

// TestParseOptionsRejectsInvalidMode 验证非法模式不会静默回退到 full。
func TestParseOptionsRejectsInvalidMode(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{Mode: "ful"}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseOptionsRejectsOversizedBatch 验证运行参数存在硬上限，避免误配置放大资源消耗。
func TestParseOptionsRejectsOversizedBatch(t *testing.T) {
	_, err := ParseOptions(types.WorkflowPayload{
		Mode:      types.ModeFull,
		BatchSize: MaxBatchSize + 1,
	}, Defaults{ShardTotal: 10, BatchSize: 100, WorkerCount: 2})
	if err == nil {
		t.Fatal("expected batch size limit error")
	}
	if !strings.Contains(err.Error(), "batch_size") {
		t.Fatalf("unexpected error: %v", err)
	}
}
