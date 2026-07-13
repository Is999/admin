package file

import (
	"fmt"
	"testing"
	"time"

	"admin/pkg/storage"
)

// TestRuntimeRegistrySpecsValid 确保文件业务运行时注册入口规格完整且名称唯一。
func TestRuntimeRegistrySpecsValid(t *testing.T) {
	specs := RuntimeRegistrySpecs()
	if len(specs) == 0 {
		t.Fatal("RuntimeRegistrySpecs() 不能为空")
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || spec.File == "" || spec.Method == "" || spec.Description == "" {
			t.Fatalf("运行时注册入口规格字段不完整: %+v", spec)
		}
		if _, ok := seen[spec.Name]; ok {
			t.Fatalf("运行时注册入口名称重复: %s", spec.Name)
		}
		seen[spec.Name] = struct{}{}
	}
}

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

// TestSecretKeyMaterialUploadPolicyRemoved 验证无生产消费方的秘钥材料上传类型不再对外开放。
func TestSecretKeyMaterialUploadPolicyRemoved(t *testing.T) {
	if _, err := fileUploadPolicyOf("secret-key-material"); err == nil {
		t.Fatal("无消费方的秘钥材料上传类型应被拒绝")
	}
}
