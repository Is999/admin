package database

import (
	"database/sql"
	"strings"
	"testing"

	"admin/common/embedasset"
)

// TestMigrationLockSQLAssetsStripHeaders 确保迁移锁 SQL 资产执行前剥离说明头。
func TestMigrationLockSQLAssetsStripHeaders(t *testing.T) {
	for name, query := range map[string]string{
		"acquire": migrationLockAcquireSQL,
		"release": migrationLockReleaseSQL,
	} {
		if !strings.Contains(query, "代码资产") {
			t.Fatalf("%s 迁移锁 SQL 缺少资产说明", name)
		}
		executable := embedasset.StripLeadingLineComments(query, "--")
		if strings.Contains(executable, "代码资产") || !strings.HasPrefix(executable, "SELECT ") {
			t.Fatalf("%s 迁移锁 SQL 剥离说明后不可执行: %q", name, executable)
		}
	}
}

// TestMigrationLockRejectsSingleConnectionPoolUnit 确保单连接配置在访问数据库前快速失败。
func TestMigrationLockRejectsSingleConnectionPoolUnit(t *testing.T) {
	db := &sql.DB{}
	db.SetMaxOpenConns(1)
	called := false
	err := WithMigrationLock(t.Context(), db, "admin:test-single-connection", 0, func() error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "至少需要 2 条连接") {
		t.Fatalf("WithMigrationLock() error = %v, want pool capacity error", err)
	}
	if called {
		t.Fatal("单连接池不应执行迁移函数")
	}
}
