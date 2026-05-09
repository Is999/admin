package repository

import (
	"strings"
	"testing"

	"admin/internal/jobs/usertag/route"
	"admin/internal/jobs/usertag/types"
	"admin/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestWorkflowShardUIDsFiltersCurrentShard 验证骨架仓储仍按工作流分片过滤 UID。
func TestWorkflowShardUIDsFiltersCurrentShard(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10, 10)))
	uids := repo.WorkflowShardUIDs(types.RuntimeOptions{ShardIndex: 1, ShardTotal: 2}, []int64{1, 2, 3, 4, 3, 0})
	if len(uids) != 2 || uids[0] != 1 || uids[1] != 3 {
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
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10, 10)))
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
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10, 10)))
	query, err := repo.applyEventOutboxScope(newUserTagDryRunDB(t).Model(&model.UserTagEventOutbox{}), types.RuntimeOptions{ShardIndex: 3, ShardTotal: 10})
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

// TestApplyEventOutboxScopeFallsBackToUIDModulo 验证工作流分片数不等于运行期索引分片数时按 UID 取模。
func TestApplyEventOutboxScopeFallsBackToUIDModulo(t *testing.T) {
	repo := NewTagRepository(NewRuntimeDeps(nil, route.NewShardPlan(10, 16)))
	query, err := repo.applyEventOutboxScope(newUserTagDryRunDB(t).Model(&model.UserTagEventOutbox{}), types.RuntimeOptions{ShardIndex: 3, ShardTotal: 10})
	if err != nil {
		t.Fatalf("applyEventOutboxScope() error = %v", err)
	}
	stmt := query.Find(&[]model.UserTagEventOutbox{}).Statement
	if !strings.Contains(stmt.SQL.String(), "MOD(uid, ?) = ?") {
		t.Fatalf("expected uid modulo filter, sql=%s", stmt.SQL.String())
	}
	if len(stmt.Vars) != 2 || stmt.Vars[0] != 10 || stmt.Vars[1] != 3 {
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
