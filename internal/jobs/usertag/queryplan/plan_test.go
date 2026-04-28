package queryplan

import (
	"strings"
	"testing"
	"time"

	"admin/internal/jobs/usertag/route"
	"admin/internal/jobs/usertag/types"
)

// TestPlanRejectsUnsafeFullScan 验证查询计划默认拒绝没有 UID 和时间窗口的全表扫描。
func TestPlanRejectsUnsafeFullScan(t *testing.T) {
	err := Plan{Name: "snapshot", Source: types.SourceMySQL, Table: "user_tag_0", Fields: []string{"uid"}}.Validate()
	if err == nil {
		t.Fatal("expected unsafe full scan error")
	}
	if !strings.Contains(err.Error(), "全表扫描") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPlanRejectsSelectStar 验证查询计划禁止 SELECT *。
func TestPlanRejectsSelectStar(t *testing.T) {
	err := Plan{Name: "runtime", Source: types.SourceMySQL, Table: "user_tag_0", Fields: []string{"*"}, UIDs: UIDSet{UIDs: []int64{1}}}.Validate()
	if err == nil {
		t.Fatal("expected select star error")
	}
	if !strings.Contains(err.Error(), "SELECT *") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPlanAllowsUIDBatch 验证批次 UID 查询属于安全计划。
func TestPlanAllowsUIDBatch(t *testing.T) {
	err := Plan{Name: "runtime", Source: types.SourceMySQL, Table: "user_tag_0", Fields: []string{"uid"}, UIDs: UIDSet{UIDs: []int64{1, 2}}}.Validate()
	if err != nil {
		t.Fatalf("expected uid batch plan valid: %v", err)
	}
}

// TestPlanAllowsWindowQuery 验证带时间窗口的聚合查询属于安全计划。
func TestPlanAllowsWindowQuery(t *testing.T) {
	err := Plan{
		Name:   "window_source",
		Source: types.SourceClickHouse,
		Table:  "generic_events",
		Fields: []string{"uid"},
		Window: TimeWindow{Start: time.Now().Add(-24 * time.Hour), End: time.Now()},
	}.Validate()
	if err != nil {
		t.Fatalf("expected window plan valid: %v", err)
	}
}

// TestPlanAllowsExplicitShardScan 验证 full/recalculate 可显式允许分片游标扫描。
func TestPlanAllowsExplicitShardScan(t *testing.T) {
	err := Plan{
		Name:               "runtime_full",
		Source:             types.SourceMySQL,
		Table:              "user_tag_0",
		Fields:             []string{"uid"},
		Shard:              route.Shard{Index: 0, Total: 10},
		AllowFullShardScan: true,
		BatchSize:          2000,
	}.Validate()
	if err != nil {
		t.Fatalf("expected shard scan valid: %v", err)
	}
}
