package model

import (
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestApplyAdminLogOrderDefaultsToLatestFirst 验证日志列表未指定排序时，默认按创建时间和 ID 倒序返回。
func TestApplyAdminLogOrderDefaultsToLatestFirst(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	dbq, err := applyAdminLogOrder(db.Model(&AdminLog{}), "", "")
	if err != nil {
		t.Fatalf("applyAdminLogOrder() error = %v", err)
	}

	stmt := dbq.Limit(20).Find(&[]AdminLog{}).Statement
	sqlText := stmt.SQL.String()
	if !strings.Contains(sqlText, "ORDER BY created_at DESC,id DESC") {
		t.Fatalf("applyAdminLogOrder() sql = %q, want ORDER BY created_at DESC,id DESC", sqlText)
	}
}

// TestApplyAdminLogOrderKeepsExplicitSort 验证日志列表显式传入排序字段时，仍优先使用调用方指定排序。
func TestApplyAdminLogOrderKeepsExplicitSort(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	dbq, err := applyAdminLogOrder(db.Model(&AdminLog{}), "id", "asc")
	if err != nil {
		t.Fatalf("applyAdminLogOrder() error = %v", err)
	}

	stmt := dbq.Limit(20).Find(&[]AdminLog{}).Statement
	sqlText := stmt.SQL.String()
	if !strings.Contains(sqlText, "ORDER BY `id` asc") {
		t.Fatalf("applyAdminLogOrder() sql = %q, want ORDER BY `id` asc", sqlText)
	}
	if strings.Contains(sqlText, "created_at DESC") {
		t.Fatalf("applyAdminLogOrder() sql = %q, unexpected default order fragment", sqlText)
	}
}
