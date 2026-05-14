package logic

import (
	"github.com/Is999/go-utils/errors"
	drivermysql "github.com/go-sql-driver/mysql"
)

// mysqlDuplicateEntryErrorNumber 表示 MySQL 唯一键冲突错误码。
const mysqlDuplicateEntryErrorNumber uint16 = 1062

// isMySQLDuplicateEntryError 判断错误链中是否包含 MySQL 唯一键冲突。
func isMySQLDuplicateEntryError(err error) bool {
	var mysqlErr *drivermysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntryErrorNumber
}

// IsMySQLDuplicateEntryError 判断错误链中是否包含 MySQL 唯一键冲突。
func IsMySQLDuplicateEntryError(err error) bool {
	return isMySQLDuplicateEntryError(err)
}
