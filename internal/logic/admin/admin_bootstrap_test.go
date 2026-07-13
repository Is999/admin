package admin

import (
	"context"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/svc"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestFinalizeBootstrapAdminRuntimeFailsWithoutRedis 确保账号重置后无法失效会话时不会伪报全部成功。
func TestFinalizeBootstrapAdminRuntimeFailsWithoutRedis(t *testing.T) {
	logicObj := NewAdminBootstrapLogic(context.Background(), svc.NewServiceContext(
		config.Config{AppID: "site-a"},
		svc.Dependencies{},
	))

	err := logicObj.finalizeBootstrapAdminRuntime(7)
	if err == nil || !strings.Contains(err.Error(), "Redis 未初始化") {
		t.Fatalf("finalizeBootstrapAdminRuntime() error = %v, want Redis failure", err)
	}
}

// TestBootstrapTargetIsSuperAdminTxUsesIndexedWriteQuery 验证自举校验直接生成当前写事务的索引点查。
func TestBootstrapTargetIsSuperAdminTxUsesIndexedWriteQuery(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	var sqlText string
	if err = db.Callback().Query().After("gorm:query").Register("test:capture_bootstrap_super_role_sql", func(tx *gorm.DB) {
		sqlText = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register query callback error = %v", err)
	}
	if _, err = bootstrapTargetIsSuperAdminTx(db, 7); err != nil {
		t.Fatalf("bootstrapTargetIsSuperAdminTx() error = %v", err)
	}
	for _, fragment := range []string{
		"FROM admin_role_rel AS rel",
		"JOIN admin_role AS role ON role.id = rel.role_id",
		"rel.user_id = ?",
		"rel.role_id = ?",
		"role.status = 1",
		"role.is_delete = 0",
		"LIMIT ?",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("bootstrap query = %q, want fragment %q", sqlText, fragment)
		}
	}
}
