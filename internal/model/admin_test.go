package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdminLastLoginIPFieldSupportsIPv6 确保模型与建库基线都能保存规范化 IPv6 文本。
func TestAdminLastLoginIPFieldSupportsIPv6(t *testing.T) {
	field, ok := reflect.TypeOf(Admin{}).FieldByName("LastLoginIP")
	if !ok {
		t.Fatal("Admin 缺少 LastLoginIP 字段")
	}
	if tag := field.Tag.Get("gorm"); !strings.Contains(tag, "type:varchar(45)") {
		t.Fatalf("Admin.LastLoginIP GORM 类型=%q, want varchar(45)", tag)
	}
}
