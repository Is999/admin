package file

import (
	"fmt"
	"testing"
	"time"

	"admin/pkg/storage"
)

// TestRegisterFileUploadPolicy 确保文件上传策略支持通过注册表扩展。
func TestRegisterFileUploadPolicy(t *testing.T) {
	bizType := fmt.Sprintf("custom-upload-%d", time.Now().UnixNano())
	policy := FileUploadPolicy{
		MaxSize:           1024,                      // 测试文件大小限制
		AllowedExts:       []string{".txt"},          // 测试扩展名白名单
		Visibility:        storage.VisibilityPrivate, // 测试默认私有访问
		AllowDirectUpload: true,                      // 测试允许直传
	}
	if err := RegisterFileUploadPolicy(bizType, policy); err != nil {
		t.Fatalf("注册文件上传策略失败: %v", err)
	}
	loaded, err := fileUploadPolicyOf(bizType)
	if err != nil {
		t.Fatalf("读取文件上传策略失败: %v", err)
	}
	if loaded.MaxSize != policy.MaxSize || !loaded.AllowDirectUpload {
		t.Fatalf("文件上传策略不一致: %+v", loaded)
	}
	if err := RegisterFileUploadPolicy(bizType, policy); err == nil {
		t.Fatal("重复注册上传业务类型应失败")
	}
}

// TestRegisterFileUploadPolicyRejectsUnsafeExt 确保注册表拒绝危险扩展名。
func TestRegisterFileUploadPolicyRejectsUnsafeExt(t *testing.T) {
	bizType := fmt.Sprintf("unsafe-upload-%d", time.Now().UnixNano())
	err := RegisterFileUploadPolicy(bizType, FileUploadPolicy{
		MaxSize:     1024,
		AllowedExts: []string{"../.txt"},
		Visibility:  storage.VisibilityPrivate,
	})
	if err == nil {
		t.Fatal("危险扩展名不应允许注册")
	}
}
