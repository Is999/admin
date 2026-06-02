package logic

import (
	"testing"

	"github.com/Is999/go-utils/errors"
	drivermysql "github.com/go-sql-driver/mysql"
)

// TestIsMySQLDuplicateEntryErrorDetectsWrappedDuplicate 验证对应场景符合预期。
func TestIsMySQLDuplicateEntryErrorDetectsWrappedDuplicate(t *testing.T) {
	duplicateErr := &drivermysql.MySQLError{Number: mysqlDuplicateEntryErrorNumber, Message: "Duplicate entry"}
	if !isMySQLDuplicateEntryError(errors.Wrap(duplicateErr, "create admin")) {
		t.Fatal("期望识别被包装的 MySQL duplicate entry 错误")
	}
	otherErr := &drivermysql.MySQLError{Number: 1213, Message: "Deadlock found"}
	if isMySQLDuplicateEntryError(errors.Wrap(otherErr, "create admin")) {
		t.Fatal("不应把非 duplicate entry 的 MySQL 错误识别为重复数据")
	}
}
