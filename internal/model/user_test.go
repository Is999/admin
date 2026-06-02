package model

import (
	"strings"
	"testing"

	"admin/common/idgen"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestUserPhysicalTableName 验证 1/10/100/1000 物理表数量的路由规则稳定。
func TestUserPhysicalTableName(t *testing.T) {
	tests := []struct {
		name            string // name 表示测试场景名称。
		shardNo         int    // shardNo 表示逻辑分片号。
		routeShardCount int    // routeShardCount 表示物理路由分片数。
		want            string // want 表示期望结果。
	}{
		{name: "single", shardNo: 999, routeShardCount: 1, want: "user"},
		{name: "ten first", shardNo: 0, routeShardCount: 10, want: "user_000"},
		{name: "ten boundary", shardNo: 100, routeShardCount: 10, want: "user_100"},
		{name: "ten middle", shardNo: 345, routeShardCount: 10, want: "user_300"},
		{name: "ten last", shardNo: 999, routeShardCount: 10, want: "user_900"},
		{name: "hundred first boundary", shardNo: 10, routeShardCount: 100, want: "user_010"},
		{name: "hundred", shardNo: 345, routeShardCount: 100, want: "user_340"},
		{name: "thousand boundary", shardNo: 999, routeShardCount: 1000, want: "user_999"},
		{name: "thousand", shardNo: 345, routeShardCount: 1000, want: "user_345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UserPhysicalTableName(tt.shardNo, tt.routeShardCount)
			if err != nil {
				t.Fatalf("UserPhysicalTableName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("UserPhysicalTableName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUserPhysicalTableNameRejectsInvalidRoute 验证路由数量只能按 1/10/100/1000 平滑拆分。
func TestUserPhysicalTableNameRejectsInvalidRoute(t *testing.T) {
	if _, err := UserPhysicalTableName(1, 64); err == nil {
		t.Fatal("期望非法物理表数量返回错误")
	}
	if _, err := UserPhysicalTableName(1000, 10); err == nil {
		t.Fatal("期望非法 shard_no 返回错误")
	}
}

// TestUserAccountTableNameRejectsMismatchedShardNo 验证账号索引不会接受错误分片号。
func TestUserAccountTableNameRejectsMismatchedShardNo(t *testing.T) {
	userID := int64(123456789)
	account := &UserAccount{
		UserID:          userID,
		ShardNo:         idgen.ShardNo(userID),
		RouteShardCount: 1000,
	}
	want, err := UserPhysicalTableName(account.ShardNo, account.RouteShardCount)
	if err != nil {
		t.Fatalf("UserPhysicalTableName() error = %v", err)
	}
	got, err := account.UserTableName()
	if err != nil {
		t.Fatalf("UserTableName() error = %v", err)
	}
	if got != want {
		t.Fatalf("UserTableName() = %q, want %q", got, want)
	}

	account.ShardNo = (account.ShardNo + 1) % userRouteShardMod
	if _, err := account.UserTableName(); err == nil {
		t.Fatal("期望账号索引 shard_no 与 user_id 不一致时返回错误")
	}
}

// TestSafeUserUpdatesRejectsImmutableFields 验证通用更新不会修改用户分片和唯一账号字段。
func TestSafeUserUpdatesRejectsImmutableFields(t *testing.T) {
	got := safeUserUpdates(map[string]any{
		"id":            int64(1),
		"shard_no":      12,
		"username":      "changed",
		"password_hash": "unsafe",
		"email":         "ok@example.com",
	}, false)
	for _, key := range []string{"id", "shard_no", "username", "password_hash"} {
		if _, ok := got[key]; ok {
			t.Fatalf("safeUserUpdates() should reject %s: %+v", key, got)
		}
	}
	if got["email"] != "ok@example.com" {
		t.Fatalf("safeUserUpdates() should keep email: %+v", got)
	}
}

// TestSplitUserAccountQueryUsesIndexedProbe 验证分表状态探测只按 route_shard_count 索引取一行。
func TestSplitUserAccountQueryUsesIndexedProbe(t *testing.T) {
	db := newUserDryRunDB(t)
	stmt := splitUserAccountQuery(db).Find(&[]int{}).Statement
	sqlText := stmt.SQL.String()
	if !strings.Contains(sqlText, "FROM `user_account`") {
		t.Fatalf("splitUserAccountQuery() sql = %q, want user_account", sqlText)
	}
	if !strings.Contains(sqlText, "route_shard_count > ?") {
		t.Fatalf("splitUserAccountQuery() sql = %q, want route_shard_count predicate", sqlText)
	}
	if !strings.Contains(sqlText, "LIMIT ?") {
		t.Fatalf("splitUserAccountQuery() sql = %q, want single-row probe", sqlText)
	}
	if strings.Contains(strings.ToLower(sqlText), "count(") {
		t.Fatalf("splitUserAccountQuery() sql = %q, should not count large table", sqlText)
	}
}

// newUserDryRunDB 构造测试依赖。
func newUserDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
