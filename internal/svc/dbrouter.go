package svc

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// DbName 表示可路由的数据库名称。
type DbName string

const (
	// DatabaseMain 表示默认主库。
	DatabaseMain DbName = "main"
)

// DB 根据数据库名称返回默认连接。
// 空名称回退到主库；命名扩展库只在配置 site_mysql.<name> 后生效。
func (s *ServiceContext) DB(database DbName) *gorm.DB {
	if s == nil {
		return nil
	}
	return s.SiteDBs.Lookup(database)
}

// ReadDB 根据数据库名称返回只读连接。
func (s *ServiceContext) ReadDB(database DbName) *gorm.DB {
	if s == nil {
		return nil
	}
	return readDB(s.SiteDBs.Lookup(database))
}

// WriteDB 根据数据库名称返回写连接。
func (s *ServiceContext) WriteDB(database DbName) *gorm.DB {
	if s == nil {
		return nil
	}
	return writeDB(s.SiteDBs.Lookup(database))
}

// NormalizeDbName 规范化数据库名称，空值统一回退主库。
func NormalizeDbName(database DbName) DbName {
	name := strings.TrimSpace(string(database))
	if name == "" || strings.EqualFold(name, string(DatabaseMain)) {
		return DatabaseMain
	}
	return DbName(name)
}

// readDB 为 GORM 连接附加只读路由；测试占位连接没有 Statement 时直接原样返回。
func readDB(db *gorm.DB) *gorm.DB {
	if db == nil || db.Statement == nil {
		return db
	}
	return db.Clauses(dbresolver.Read)
}

// writeDB 为 GORM 连接附加主库路由；测试占位连接没有 Statement 时直接原样返回。
func writeDB(db *gorm.DB) *gorm.DB {
	if db == nil || db.Statement == nil {
		return db
	}
	return db.Clauses(dbresolver.Write)
}
