package model

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"admin/common/idgen"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

// TestUserPhysicalTableName 验证固定逻辑桶稳定路由到用户物理表。
func TestUserPhysicalTableName(t *testing.T) {
	tests := []struct {
		name            string // name 表示测试场景名称。
		shardNo         int    // shardNo 表示逻辑分片号。
		routeShardCount int    // routeShardCount 表示用户物理分片数。
		want            string // want 表示期望结果。
	}{
		{name: "default", shardNo: 0, routeShardCount: 0, want: "user"},
		{name: "single", shardNo: 1023, routeShardCount: 1, want: "user"},
		{name: "two first", shardNo: 0, routeShardCount: 2, want: "user"},
		{name: "two boundary", shardNo: 512, routeShardCount: 2, want: "user_b0512"},
		{name: "four middle", shardNo: 700, routeShardCount: 4, want: "user_b0512"},
		{name: "sixteen middle", shardNo: 345, routeShardCount: 16, want: "user_b0320"},
		{name: "full last", shardNo: 1023, routeShardCount: 1024, want: "user_b1023"},
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

// TestUserIdentityTableName 验证身份类型稳定路由到独立物理表。
func TestUserIdentityTableName(t *testing.T) {
	tests := []struct {
		identityType string // identityType 表示身份类型。
		want         string // want 表示期望物理表。
	}{
		{identityType: UserIdentityTypeUsername, want: TableNameUserIdentityUsername},
		{identityType: UserIdentityTypeEmail, want: TableNameUserIdentityEmail},
		{identityType: UserIdentityTypePhone, want: TableNameUserIdentityPhone},
		{identityType: UserIdentityTypeOAuth, want: TableNameUserIdentityOAuth},
	}
	for _, tt := range tests {
		t.Run(tt.identityType, func(t *testing.T) {
			got, err := UserIdentityTableName(tt.identityType)
			if err != nil {
				t.Fatalf("UserIdentityTableName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("UserIdentityTableName() = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := UserIdentityTableName("unknown"); err == nil {
		t.Fatal("期望非法身份类型返回错误")
	}
}

// TestUserPhysicalTableNameRejectsInvalidRoute 验证物理分片数只接受平滑拆分档位。
func TestUserPhysicalTableNameRejectsInvalidRoute(t *testing.T) {
	if _, err := UserPhysicalTableName(1, 3); err == nil {
		t.Fatal("期望非法物理表数量返回错误")
	}
	if _, err := UserPhysicalTableName(1, -1); err == nil {
		t.Fatal("期望负数物理表数量返回错误")
	}
	if _, err := UserPhysicalTableName(1024, 2); err == nil {
		t.Fatal("期望非法 shard_no 返回错误")
	}
}

// TestUserIdentityTableNameRejectsMismatchedShardNo 验证身份索引不会接受错误分片号。
func TestUserIdentityTableNameRejectsMismatchedShardNo(t *testing.T) {
	userID := int64(1)
	for idgen.ShardNo(userID) < 512 {
		userID++
	}
	identity := &UserIdentity{
		IdentityType:  UserIdentityTypeUsername,
		Provider:      UserIdentityProviderLocal,
		IdentityValue: "demo_user",
		UserID:        userID,
		UserShardNo:   idgen.ShardNo(userID),
	}
	const currentRouteShardCount = 2
	want, err := UserPhysicalTableName(identity.UserShardNo, currentRouteShardCount)
	if err != nil {
		t.Fatalf("UserPhysicalTableName() error = %v", err)
	}
	got, err := identity.UserTableName(currentRouteShardCount)
	if err != nil {
		t.Fatalf("UserTableName() error = %v", err)
	}
	if got != want {
		t.Fatalf("UserTableName() = %q, want %q", got, want)
	}

	identity.UserShardNo = (identity.UserShardNo + 1) % idgen.ShardMod
	if _, err := identity.UserTableName(currentRouteShardCount); err == nil {
		t.Fatal("期望身份索引 user_shard_no 与 user_id 不一致时返回错误")
	}
}

// TestFindUsersByIdentityRowsUsesRoutedTable 验证批量读取按身份目录访问用户物理表。
func TestFindUsersByIdentityRowsUsesRoutedTable(t *testing.T) {
	buffer := &bytes.Buffer{}
	db := newUserDryRunDB(t).Session(&gorm.Session{
		Logger: logger.New(log.New(buffer, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	firstID := int64(1)
	for idgen.ShardNo(firstID) >= 512 {
		firstID++
	}
	secondID := firstID + 1
	for idgen.ShardNo(secondID) < 512 {
		secondID++
	}
	identities := []UserIdentity{
		{IdentityType: UserIdentityTypeUsername, IdentityValue: "first", UserID: firstID, UserShardNo: idgen.ShardNo(firstID)},
		{IdentityType: UserIdentityTypeUsername, IdentityValue: "second", UserID: secondID, UserShardNo: idgen.ShardNo(secondID)},
	}
	if _, err := FindUsersByIdentityRows(db, identities, 2); err == nil {
		t.Fatal("DryRun 不返回业务行，期望缺失记录错误")
	}
	sqlText := buffer.String()
	for _, want := range []string{"FROM `user`", "FROM `user_b0512`", "shard_no IN", "id IN"} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("批量用户 SQL 缺少 %q: %s", want, sqlText)
		}
	}
}

// TestFindUsersByIdentityRowsRejectsMismatchedUserShard 验证批量读取不会接受主表中的错误固定桶。
func TestFindUsersByIdentityRowsRejectsMismatchedUserShard(t *testing.T) {
	db := newUserDryRunDB(t)
	firstID := int64(123456789)
	secondID := int64(987654321)
	if idgen.ShardNo(firstID) == idgen.ShardNo(secondID) {
		t.Fatal("测试用户必须落在不同固定桶")
	}
	identities := []UserIdentity{
		{IdentityType: UserIdentityTypeUsername, IdentityValue: "first", UserID: firstID, UserShardNo: idgen.ShardNo(firstID)},
		{IdentityType: UserIdentityTypeUsername, IdentityValue: "second", UserID: secondID, UserShardNo: idgen.ShardNo(secondID)},
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:inject_mismatched_user_shard", func(tx *gorm.DB) {
		if rows, ok := tx.Statement.Dest.(*[]User); ok {
			*rows = []User{
				{ID: firstID, ShardNo: idgen.ShardNo(secondID)},
				{ID: secondID, ShardNo: idgen.ShardNo(secondID)},
			}
		}
	}); err != nil {
		t.Fatalf("注册测试查询回调失败: %v", err)
	}
	if _, err := FindUsersByIdentityRows(db, identities, 1); err == nil {
		t.Fatal("主表 shard_no 与身份目录不一致时应返回错误")
	}
}

// TestSafeUserUpdatesRejectsImmutableFields 验证通用更新不会修改用户分片和唯一账号字段。
func TestSafeUserUpdatesRejectsImmutableFields(t *testing.T) {
	got := safeUserUpdates(map[string]any{
		"id":            int64(1),
		"shard_no":      12,
		"username":      "changed",
		"password_hash": "unsafe",
		"auth_version":  uint64(99),
		"status":        UserStatusDisabled,
		"email":         "raw@example.com",
		"email_hash":    " hash ",
	})
	for _, key := range []string{"id", "shard_no", "username", "password_hash", "auth_version", "status", "email"} {
		if _, ok := got[key]; ok {
			t.Fatalf("safeUserUpdates() should reject %s: %+v", key, got)
		}
	}
	if got["email_hash"] != "hash" {
		t.Fatalf("safeUserUpdates() should keep secure email fields: %+v", got)
	}
}

// TestUserDBSessionPreservesResolverMode 验证动态表会话复制不会把显式主库或副本路由清空。
func TestUserDBSessionPreservesResolverMode(t *testing.T) {
	db := newUserDryRunDB(t)
	for name, operation := range map[string]dbresolver.Operation{
		"read":  dbresolver.Read,
		"write": dbresolver.Write,
	} {
		t.Run(name, func(t *testing.T) {
			key := "gorm:db_resolver:" + name
			session := userDBSession(db.Clauses(operation))
			if _, ok := session.Statement.Settings.Load(key); !ok {
				t.Fatalf("userDBSession() should preserve %s resolver mode", name)
			}
		})
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
