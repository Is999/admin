package storage

import (
	"strings"
	"testing"
)

// TestBuildObjectKeyWithPrefix 确保 S3 key 前缀会正确拼接到业务目录之前。
func TestBuildObjectKeyWithPrefix(t *testing.T) {
	objectKey := buildObjectKey("image", "admin-avatar", "demo.png")
	if !strings.HasPrefix(objectKey, "image/admin-avatar/") {
		t.Fatalf("对象 key 前缀不正确: %s", objectKey)
	}
}

// TestBuildObjectKeyWithoutPrefix 确保未配置前缀时仍保持原有业务目录结构。
func TestBuildObjectKeyWithoutPrefix(t *testing.T) {
	objectKey := buildObjectKey("", "admin-avatar", "demo.png")
	if !strings.HasPrefix(objectKey, "admin-avatar/") {
		t.Fatalf("对象 key 不正确: %s", objectKey)
	}
}

// TestObjectKeyPrefixMatchesBuildObjectKey 验证公开访问校验使用的前缀与存储写入规则一致。
func TestObjectKeyPrefixMatchesBuildObjectKey(t *testing.T) {
	objectPrefix := ObjectKeyPrefix("Tenant//Files", "admin-avatar")
	if objectPrefix != "tenant/files/admin-avatar" {
		t.Fatalf("ObjectKeyPrefix() = %q", objectPrefix)
	}
	if objectKey := buildObjectKey("Tenant//Files", "admin-avatar", "demo.png"); !strings.HasPrefix(objectKey, objectPrefix+"/") {
		t.Fatalf("buildObjectKey() = %q, want prefix %q", objectKey, objectPrefix)
	}
}
