package repository

import (
	"reflect"
	"strings"
	"testing"

	"admin/common/idgen"
	"admin/internal/jobs/usertag/route"
	"admin/internal/jobs/usertag/types"
	"admin/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestWorkflowShardUIDsFiltersCurrentShard 验证骨架仓储仍按工作流分片过滤 UID。
func TestWorkflowShardUIDsFiltersCurrentShard(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10)))
	input := []int64{1, 2, 3, 4, 3, 0}
	uids := repo.WorkflowShardUIDs(types.RuntimeOptions{ShardIndex: 1, ShardTotal: 2}, input)
	want := make([]int64, 0, len(input))
	seen := make(map[int64]struct{})
	for _, uid := range input {
		if uid <= 0 || idgen.ShardNo(uid)*2/idgen.ShardMod != 1 {
			continue
		}
		if _, exists := seen[uid]; exists {
			continue
		}
		seen[uid] = struct{}{}
		want = append(want, uid)
	}
	if !reflect.DeepEqual(uids, want) {
		t.Fatalf("unexpected shard uids: %#v", uids)
	}
}

// TestEventOutboxRowToChangeKeepsTagSource 验证 hook 事件保留标签来源。
func TestEventOutboxRowToChangeKeepsTagSource(t *testing.T) {
	change := eventOutboxRowToChange(model.UserTagEventOutbox{TagSource: 1})
	if change.Source != 1 {
		t.Fatalf("Source=%d want=1", change.Source)
	}
}

// TestApplyEventOutboxScopeSkipsShardForSingleWorker 验证单任务派发会覆盖所有 outbox 分片。
func TestApplyEventOutboxScopeSkipsShardForSingleWorker(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10)))
	query, err := repo.applyEventOutboxScope(newUserTagDryRunDB(t).Model(&model.UserTagEventOutbox{}), types.RuntimeOptions{ShardTotal: 1})
	if err != nil {
		t.Fatalf("applyEventOutboxScope() error = %v", err)
	}
	sqlText := query.Find(&[]model.UserTagEventOutbox{}).Statement.SQL.String()
	if strings.Contains(sqlText, "shard_no") || strings.Contains(sqlText, "MOD(") {
		t.Fatalf("single worker should not add shard filter: %s", sqlText)
	}
}

// TestApplyEventOutboxScopeUsesRuntimeShardIndex 验证分片数一致时命中 shard_no 索引。
func TestApplyEventOutboxScopeUsesRuntimeShardIndex(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(1024)))
	query, err := repo.applyEventOutboxScope(newUserTagDryRunDB(t).Model(&model.UserTagEventOutbox{}), types.RuntimeOptions{ShardIndex: 3, ShardTotal: 1024})
	if err != nil {
		t.Fatalf("applyEventOutboxScope() error = %v", err)
	}
	stmt := query.Find(&[]model.UserTagEventOutbox{}).Statement
	if !strings.Contains(stmt.SQL.String(), "shard_no = ?") {
		t.Fatalf("expected shard_no filter, sql=%s", stmt.SQL.String())
	}
	if len(stmt.Vars) != 1 || stmt.Vars[0] != 3 {
		t.Fatalf("unexpected vars: %#v", stmt.Vars)
	}
}

// TestApplyEventOutboxScopeUsesBucketRange 验证分片数不整除时仍使用连续固定桶范围。
func TestApplyEventOutboxScopeUsesBucketRange(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10)))
	query, err := repo.applyEventOutboxScope(newUserTagDryRunDB(t).Model(&model.UserTagEventOutbox{}), types.RuntimeOptions{ShardIndex: 3, ShardTotal: 10})
	if err != nil {
		t.Fatalf("applyEventOutboxScope() error = %v", err)
	}
	stmt := query.Find(&[]model.UserTagEventOutbox{}).Statement
	if !strings.Contains(stmt.SQL.String(), "shard_no BETWEEN") {
		t.Fatalf("expected shard_no BETWEEN filter, sql=%s", stmt.SQL.String())
	}
	if len(stmt.Vars) != 2 || stmt.Vars[0] != 308 || stmt.Vars[1] != 409 {
		t.Fatalf("unexpected vars: %#v", stmt.Vars)
	}
}

// newUserTagDryRunDB 创建用户标签仓储 SQL 断言使用的 DryRun DB。
func newUserTagDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
