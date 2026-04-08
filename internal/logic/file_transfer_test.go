package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"admin_cron/pkg/storage"
	"admin_cron/pkg/transfer"
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
