package maint

import (
	"strings"
	"testing"
)

// TestValidateClickHouseCollectionName 验证 named collection 只接受普通标识符。
func TestValidateClickHouseCollectionName(t *testing.T) {
	if err := ValidateClickHouseCollectionName("mysql_203_log", "app_id"); err != nil {
		t.Fatalf("valid collection rejected: %v", err)
	}
	if err := ValidateClickHouseCollectionName("mysql-1-log", "app_id"); err == nil {
		t.Fatal("expected invalid collection error")
	}
	if err := ValidateClickHouseCollectionName("", "app_id"); err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected empty hint in error, got %v", err)
	}
}

// TestIndexHasPrefix 验证索引左前缀判断兼容大小写和空白字符。
func TestIndexHasPrefix(t *testing.T) {
	tests := []struct {
		name       string   // name 表示测试场景名称。
		columns    []string // columns 表示列定义集合。
		leadColumn string   // leadColumn 表示期望首列。
		want       bool     // want 表示期望结果。
	}{
		{name: "match", columns: []string{"created_time", "id"}, leadColumn: "created_time", want: true},
		{name: "case_insensitive", columns: []string{"DATE", "id"}, leadColumn: "date", want: true},
		{name: "wrong_order", columns: []string{"id", "date"}, leadColumn: "date", want: false},
		{name: "empty", columns: nil, leadColumn: "date", want: false},
	}
	for _, tt := range tests {
		if got := IndexHasPrefix(tt.columns, tt.leadColumn); got != tt.want {
			t.Fatalf("%s IndexHasPrefix = %t, want %t", tt.name, got, tt.want)
		}
	}
}

// TestSQLHelpers 验证公共 SQL 字面量和标识符工具保持历史行为。
func TestSQLHelpers(t *testing.T) {
	if got := QuoteClickHouseIdent("archive_source"); got != "`archive_source`" {
		t.Fatalf("QuoteClickHouseIdent = %s", got)
	}
	if got := QuoteMySQLIdent("archive_event"); got != "`archive_event`" {
		t.Fatalf("QuoteMySQLIdent = %s", got)
	}
	if got := QuoteClickHouseCollection("mysql_203_log"); got != "mysql_203_log" {
		t.Fatalf("QuoteClickHouseCollection = %s", got)
	}
	if got := ClickHouseStringLiteral("a'b"); got != "'a''b'" {
		t.Fatalf("ClickHouseStringLiteral = %s", got)
	}
	if got := UInt64ListSQL([]uint64{11, 12, 13}); got != "11,12,13" {
		t.Fatalf("UInt64ListSQL = %s", got)
	}
}
