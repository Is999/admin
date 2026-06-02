package types

import "testing"

// TestAdminInfoCacheUsesLowerCamelLoginAddr 验证登录态缓存只使用标准小驼峰字段名。
func TestAdminInfoCacheUsesLowerCamelLoginAddr(t *testing.T) {
	info := &AdminInfo{
		LastLoginIPAddr: "中国香港",
	}

	values := info.ToMap()
	if _, ok := values["LastLoginIPAddr"]; ok {
		t.Fatal("登录态缓存不应写入 Go 结构体字段名 LastLoginIPAddr")
	}
	if got := values["lastLoginIpaddr"]; got != "中国香港" {
		t.Fatalf("lastLoginIpaddr = %v, want 中国香港", got)
	}

	var restored AdminInfo
	if err := restored.FromMap(map[string]string{"lastLoginIpaddr": "中国香港"}); err != nil {
		t.Fatalf("FromMap() error = %v", err)
	}
	if restored.LastLoginIPAddr != "中国香港" {
		t.Fatalf("LastLoginIPAddr = %q, want 中国香港", restored.LastLoginIPAddr)
	}
}
