package file

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/pkg/storage"
	"admin/pkg/transfer"
)

// TestValidateStoredObjectSHA256 确保存储层返回的流式摘要会参与最终对象完整性校验。
func TestValidateStoredObjectSHA256(t *testing.T) {
	object := &storage.StoredObject{SHA256: "abc123"}
	if err := validateStoredObjectSHA256(object, "ABC123"); err != nil {
		t.Fatalf("大小写不同的相同摘要应通过校验: %v", err)
	}
	if err := validateStoredObjectSHA256(object, "bad-hash"); err == nil {
		t.Fatal("期望摘要不一致时返回错误")
	}
	if err := validateStoredObjectSHA256(&storage.StoredObject{}, "bad-hash"); err != nil {
		t.Fatalf("空对象摘要应交由原有链路校验，不应在此失败: %v", err)
	}
}

// TestPersistedServerUploadSessionDetection 验证对应场景符合预期。
func TestPersistedServerUploadSessionDetection(t *testing.T) {
	session := &transfer.UploadSession{
		Status:      "completed",
		ObjectKey:   "private/demo/file.txt",
		StoragePath: "private/demo/file.txt",
	}
	if !isPersistedServerUploadSession(session) {
		t.Fatal("期望已持久化服务端上传会话被识别")
	}
	session.StoragePath = filepath.Join(t.TempDir(), "file.txt")
	if isPersistedServerUploadSession(session) {
		t.Fatal("本地完成文件尚未持久化时不应被识别为对象存储态")
	}
}

// TestValidateRequiredUploadSHA256 验证对应场景符合预期。
func TestValidateRequiredUploadSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	if err := validateRequiredUploadSHA256(strings.ToUpper(hex.EncodeToString(sum[:]))); err != nil {
		t.Fatalf("合法 SHA-256 摘要应通过校验: %v", err)
	}
	if err := validateRequiredUploadSHA256(""); err == nil {
		t.Fatal("空 SHA-256 摘要应失败")
	}
	if err := validateRequiredUploadSHA256("bad-hash"); err == nil {
		t.Fatal("非法 SHA-256 摘要应失败")
	}
}

// TestBuildUploadAccessURLUsesObjectKey 验证公开头像地址不依赖会过期的上传会话。
func TestBuildUploadAccessURLUsesObjectKey(t *testing.T) {
	session := &transfer.UploadSession{
		BizType:   FileTransferBizAdminAvatar,
		ObjectKey: "admin-avatar/202607/16/avatar.png",
		Status:    "completed",
	}
	if got := buildUploadAccessURL(session); got != "/api/file-transfer/access?objectKey=admin-avatar%2F202607%2F16%2Favatar.png" {
		t.Fatalf("公开头像地址不符合预期: %s", got)
	}
	session.Status = "uploading"
	if got := buildUploadAccessURL(session); got != "" {
		t.Fatalf("未完成上传不应生成公开地址: %s", got)
	}
}

// TestResolveFileObjectKeyParsesPublicAccessURL 验证保存头像时可从公开访问 URL 还原对象 key。
func TestResolveFileObjectKeyParsesPublicAccessURL(t *testing.T) {
	store := storage.NewLocalStorage(config.FileStorageConfig{Local: config.FileStorageLocalConfig{RootDir: t.TempDir()}})
	objectKey, err := resolveFileObjectKey(store, "/api/file-transfer/access?objectKey=admin-avatar%2F202607%2F16%2Favatar.png")
	if err != nil {
		t.Fatalf("解析公开头像地址失败: %v", err)
	}
	if objectKey != "admin-avatar/202607/16/avatar.png" {
		t.Fatalf("对象 key = %q, want admin-avatar/202607/16/avatar.png", objectKey)
	}
	if _, err := resolveFileObjectKey(store, "/api/file-transfer/access"); err == nil {
		t.Fatal("缺少对象 key 的公开地址应被拒绝")
	}
}

// TestManagedUploadObjectKey 验证上传对象只接受指定业务的固定目录结构。
func TestManagedUploadObjectKey(t *testing.T) {
	validKeys := map[string]string{
		"admin-avatar/202607/16/avatar.png":              "admin-avatar",
		"tenant/files/admin-avatar/202607/16/avatar.png": "tenant/files/admin-avatar",
	}
	for objectKey, expectedPrefix := range validKeys {
		if !isManagedUploadObjectKey(objectKey, expectedPrefix) {
			t.Fatalf("合法头像对象 key 被拒绝: %s", objectKey)
		}
	}
	invalidKeys := []string{
		"sys-config-excel-import/202607/16/import.xlsx",
		"admin-avatar/not-date/16/avatar.png",
		"admin-avatar/202607/avatar.png",
		"tenant/admin-avatar/202607/16/private/secret.txt",
		"other-app/admin-avatar/202607/16/avatar.png",
	}
	for _, objectKey := range invalidKeys {
		if isManagedUploadObjectKey(objectKey, "admin-avatar") {
			t.Fatalf("非头像对象 key 被错误放行: %s", objectKey)
		}
	}
}
