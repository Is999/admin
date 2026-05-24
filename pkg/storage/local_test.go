package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"admin/helper"
	"admin/internal/config"
)

type testVirusScanner struct {
	called *bool // 是否已调用
}

// ScanFile 记录测试扫描调用。
func (s testVirusScanner) ScanFile(context.Context, string, string) error {
	if s.called != nil {
		*s.called = true
	}
	return nil
}

// TestRuntimeRegistrySpecsValid 确保 storage 运行时注册入口规格完整且名称唯一。
func TestRuntimeRegistrySpecsValid(t *testing.T) {
	specs := RuntimeRegistrySpecs()
	if len(specs) == 0 {
		t.Fatal("RuntimeRegistrySpecs() 不能为空")
	}
	nameSeen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || spec.File == "" || spec.Method == "" || spec.Description == "" {
			t.Fatalf("运行时注册入口规格字段不完整: %+v", spec)
		}
		if _, ok := nameSeen[spec.Name]; ok {
			t.Fatalf("运行时注册入口名称重复: %s", spec.Name)
		}
		nameSeen[spec.Name] = struct{}{}
	}
}

// TestLocalStorageSaveContentAndOpen 确保本地存储可保存内容并读取对象元信息。
func TestLocalStorageSaveContentAndOpen(t *testing.T) {
	rootDir := t.TempDir()
	store := NewLocalStorage(config.FileStorageConfig{
		Local: config.FileStorageLocalConfig{
			RootDir: rootDir,
			Domain:  "https://static.example.com/files",
		},
	})

	object, err := store.SaveContent(context.Background(), SaveContentReq{
		BizType:          "avatar",
		Body:             strings.NewReader("hello"),
		Size:             5,
		OriginalFileName: "demo.txt",
		StoredFileName:   BuildStoredFileName("file-id", "demo.txt"),
		Visibility:       VisibilityPublic,
	})
	if err != nil {
		t.Fatalf("保存本地内容失败: %v", err)
	}
	if object.StorageType != TypeLocal || object.ObjectKey == "" {
		t.Fatalf("对象信息不完整: %+v", object)
	}
	wantHash := sha256.Sum256([]byte("hello"))
	if object.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("对象摘要不正确: got=%s", object.SHA256)
	}
	if object.PublicURL == "" {
		t.Fatalf("期望 public 对象返回公开 URL")
	}

	result, err := store.Open(context.Background(), OpenObjectReq{ObjectKey: object.ObjectKey})
	if err != nil {
		t.Fatalf("打开本地对象失败: %v", err)
	}
	defer result.Reader.Close()
	if result.ContentLength != 5 || result.FileName != "file-id.txt" {
		t.Fatalf("对象读取信息不正确: %+v", result)
	}
}

// TestLocalStorageObjectKeyKeepsLocalPath 确保本地存储不会被 S3 的 key 前缀配置影响。
func TestLocalStorageObjectKeyKeepsLocalPath(t *testing.T) {
	rootDir := t.TempDir()
	store := NewLocalStorage(config.FileStorageConfig{
		Local: config.FileStorageLocalConfig{
			RootDir: rootDir,
		},
	})

	object, err := store.SaveContent(context.Background(), SaveContentReq{
		BizType:          "admin-avatar",
		Body:             strings.NewReader("hello"),
		Size:             5,
		OriginalFileName: "demo.txt",
		StoredFileName:   BuildStoredFileName("file-id", "demo.txt"),
		Visibility:       VisibilityPrivate,
	})
	if err != nil {
		t.Fatalf("保存带前缀的本地内容失败: %v", err)
	}
	if !strings.HasPrefix(object.ObjectKey, "admin-avatar/") {
		t.Fatalf("本地对象 key 不应带 S3 前缀: %s", object.ObjectKey)
	}
}

// TestLocalStorageRejectsUnsafeObjectKey 确保本地存储拒绝路径穿越对象标识。
func TestLocalStorageRejectsUnsafeObjectKey(t *testing.T) {
	store := NewLocalStorage(config.FileStorageConfig{
		Local: config.FileStorageLocalConfig{RootDir: t.TempDir()},
	})
	for _, raw := range []string{
		"../secret.txt",
		"safe/../secret.txt",
		"safe//secret.txt",
		`safe\secret.txt`,
	} {
		if _, err := store.ResolveObjectKey(raw); err == nil {
			t.Fatalf("期望不安全对象标识 %q 被拒绝", raw)
		}
	}
}

// TestDetectContentTypeRejectsInvalidFallback 确保非法 MIME 回退值不会进入对象元数据。
func TestDetectContentTypeRejectsInvalidFallback(t *testing.T) {
	if got := DetectContentType("demo.txt", "text/plain\r\nX-Bad: 1"); got != "text/plain; charset=utf-8" && got != "text/plain" {
		t.Fatalf("DetectContentType() = %q, want text/plain fallback from extension", got)
	}
	if got := DetectContentType("demo.bin", "application/json"); got != "application/json" {
		t.Fatalf("DetectContentType() = %q, want application/json", got)
	}
}

// TestCopyToFileUsesAtomicTempFile 确保存储写入完成后不会残留临时文件。
func TestCopyToFileUsesAtomicTempFile(t *testing.T) {
	dir := t.TempDir()
	dstPath := filepath.Join(dir, "nested", "demo.txt")
	result, err := copyToFile(dstPath, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	wantHash := sha256.Sum256([]byte("hello"))
	if result.Size != 5 || result.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("写入结果元数据不正确: %+v", result)
	}
	body, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("文件内容不正确: %s", string(body))
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(dstPath), storageTempFilePattern))
	if err != nil {
		t.Fatalf("扫描临时文件失败: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("不应残留临时文件: %+v", matches)
	}
}

// TestIsCrossDeviceRenameError 验证 EXDEV 能被识别并触发跨卷复制兜底。
func TestIsCrossDeviceRenameError(t *testing.T) {
	err := &os.LinkError{Op: "rename", Old: "/mnt/a/tmp", New: "/mnt/b/file", Err: syscall.EXDEV}
	if !helper.IsCrossDeviceRenameError(err) {
		t.Fatal("期望 EXDEV rename 错误被识别为跨设备错误")
	}
}

// TestRegisterObjectStorage 确保对象存储实现可以通过注册表扩展。
func TestRegisterObjectStorage(t *testing.T) {
	storageType := fmt.Sprintf("local_test_registry_%d", time.Now().UnixNano())
	if err := RegisterObjectStorage(storageType, func(cfg config.FileStorageConfig) (ObjectStorage, error) {
		return NewLocalStorage(cfg), nil
	}); err != nil {
		t.Fatalf("注册对象存储失败: %v", err)
	}
	store, err := NewObjectStorage(config.FileStorageConfig{
		Type:  storageType,
		Local: config.FileStorageLocalConfig{RootDir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("创建注册对象存储失败: %v", err)
	}
	if store.Type() != TypeLocal {
		t.Fatalf("期望注册对象存储返回本地实现，实际为 %s", store.Type())
	}
}

// TestRegisterVirusScanner 确保病毒扫描器可以通过注册表扩展。
func TestRegisterVirusScanner(t *testing.T) {
	called := false
	name := fmt.Sprintf("test_scanner_registry_%d", time.Now().UnixNano())
	if err := RegisterVirusScanner(name, func() VirusScanner {
		return testVirusScanner{called: &called}
	}); err != nil {
		t.Fatalf("注册病毒扫描器失败: %v", err)
	}
	scanner, err := NewNamedVirusScanner(name)
	if err != nil {
		t.Fatalf("创建病毒扫描器失败: %v", err)
	}
	if err := scanner.ScanFile(context.Background(), "/tmp/demo.txt", "demo"); err != nil {
		t.Fatalf("执行病毒扫描失败: %v", err)
	}
	if !called {
		t.Fatal("期望病毒扫描器已被调用")
	}
}
